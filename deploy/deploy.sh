#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
ENV_FILE="${ENV_FILE:-$PROJECT_ROOT/.env}"

usage() {
  cat <<'EOF'
用法：bash deploy/deploy.sh [start|stop|update] [master|slave]
不传参数时显示交互菜单。

  start   首次构建并启动；已有容器则启动该容器
  stop    停止容器，保留容器、数据和日志
  update  git pull --ff-only，重新构建并替换所选节点容器

默认共用项目根目录 .env，可通过 ENV_FILE=/absolute/path/.env 指定。
master 固定使用 13000 端口，slave 固定使用 13001 端口。
修改 .env 后，停止再启动容器即可重新加载配置。
EOF
}

fail() { echo "错误：$*" >&2; exit 1; }

if [[ $# -eq 1 && ( "$1" == --help || "$1" == -h ) ]]; then
  usage
  exit 0
fi

if [[ $# -eq 0 ]]; then
  cat <<'EOF'
1) 启动 master (13000)
2) 停止 master
3) 更新 master
4) 启动 slave (13001)
5) 停止 slave
6) 更新 slave
0) 退出
EOF
  read -r -p '请选择：' choice
  case "$choice" in
    1) set -- start master ;;
    2) set -- stop master ;;
    3) set -- update master ;;
    4) set -- start slave ;;
    5) set -- stop slave ;;
    6) set -- update slave ;;
    0) exit 0 ;;
    *) fail '无效选项。' ;;
  esac
fi

[[ $# -eq 2 ]] || { usage >&2; exit 1; }
ACTION="$1"
ROLE="$2"
case "$ACTION" in start|stop|update) ;; *) fail '操作必须是 start、stop 或 update。' ;; esac
case "$ROLE" in
  master) PORT=13000 ;;
  slave) PORT=13001 ;;
  *) fail '节点必须是 master 或 slave。' ;;
esac

NAME="new-api-$ROLE"
IMAGE="new-api-local:$ROLE"
BACKUP="$NAME-previous"
DATA_DIR="$SCRIPT_DIR/data/$ROLE"
LABEL="new-api.deploy.role"

command -v docker >/dev/null 2>&1 || fail '请先安装 Docker。'
docker info >/dev/null 2>&1 || fail '无法连接 Docker，请检查服务和访问权限。'

exists() { docker container inspect "$1" >/dev/null 2>&1; }
running() { [[ "$(docker container inspect --format '{{.State.Running}}' "$1")" == true ]]; }
check_owner() {
  [[ "$(docker container inspect --format "{{index .Config.Labels \"$LABEL\"}}" "$NAME")" == "$ROLE" ]] \
    || fail "容器 $NAME 不是本脚本创建的，请先处理同名容器。"
}

if [[ "$ACTION" == stop ]]; then
  if exists "$NAME"; then
    check_owner
    docker stop "$NAME"
    echo "$NAME 已停止，数据和日志已保留。"
  else
    echo "$NAME 尚未创建。"
  fi
  exit 0
fi

[[ -f "$ENV_FILE" && -r "$ENV_FILE" ]] || fail "找不到可读配置文件：$ENV_FILE"
ENV_FILE="$(cd -- "$(dirname -- "$ENV_FILE")" && pwd)/$(basename -- "$ENV_FILE")"
mkdir -p "$DATA_DIR/logs"

build_image() {
  docker build --file "$SCRIPT_DIR/Dockerfile.noweb" --tag "$IMAGE" "$PROJECT_ROOT"
}

create_container() {
  docker run -d \
    --name "$NAME" \
    --label "$LABEL=$ROLE" \
    --restart unless-stopped \
    --publish "$PORT:$PORT" \
    --env "PORT=$PORT" \
    --env "NODE_TYPE=$ROLE" \
    --env TZ=Asia/Shanghai \
    --add-host host.docker.internal:host-gateway \
    --mount "type=bind,source=$ENV_FILE,target=/app/.env,readonly" \
    --mount "type=bind,source=$DATA_DIR,target=/data" \
    "$IMAGE" --config /app/.env --log-dir /data/logs
}

wait_ready() {
  local attempt health
  for ((attempt=0; attempt<60; attempt++)); do
    running "$NAME" || return 1
    health="$(docker container inspect --format '{{.State.Health.Status}}' "$NAME")" || return 1
    [[ "$health" != healthy ]] || return 0
    sleep 2
  done
  echo "$NAME 在 120 秒内未通过健康检查。" >&2
  return 1
}

if exists "$NAME"; then
  check_owner
fi

if [[ "$ACTION" == start ]]; then
  if exists "$NAME"; then
    # 重新创建容器才会更换配置文件挂载路径；普通启动沿用原路径。
    MOUNTED_ENV="$(docker container inspect --format '{{range .Mounts}}{{if eq .Destination "/app/.env"}}{{.Source}}{{end}}{{end}}' "$NAME")"
    [[ "$MOUNTED_ENV" == "$ENV_FILE" ]] || fail '配置路径与已有容器不一致，请使用 update 重建。'
    docker start "$NAME"
  else
    if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
      build_image
    fi
    create_container
  fi
  wait_ready || fail "启动未通过检查，请运行 docker logs $NAME 查看原因。"
  echo "$NAME 已启动，端口：$PORT，配置：$ENV_FILE"
  exit 0
fi

# 拉取和构建失败时，现有容器继续运行。
command -v git >/dev/null 2>&1 || fail '请先安装 Git。'
exists "$BACKUP" && fail "存在备份容器 $BACKUP，请先检查并处理上次更新。"
git -C "$PROJECT_ROOT" pull --ff-only
build_image

HAD_OLD=false
WAS_RUNNING=false
NEW_ATTEMPTED=false
COMMITTED=false

rollback() {
  local status=$?
  trap - EXIT INT TERM
  if [[ "$COMMITTED" != true ]]; then
    echo '更新未完成，正在恢复原容器状态。' >&2
    if [[ "$NEW_ATTEMPTED" == true ]] && exists "$NAME"; then
      docker rm -f "$NAME" || true
    fi
    if [[ "$HAD_OLD" == true ]]; then
      if exists "$BACKUP"; then
        docker rename "$BACKUP" "$NAME" || true
      fi
      if [[ "$WAS_RUNNING" == true ]]; then
        docker start "$NAME" || true
      fi
    fi
  fi
  exit "$status"
}
trap rollback EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if exists "$NAME"; then
  HAD_OLD=true
  if running "$NAME"; then WAS_RUNNING=true; fi
  docker stop "$NAME"
  docker rename "$NAME" "$BACKUP"
fi
NEW_ATTEMPTED=true
create_container
wait_ready || fail "新容器未通过健康检查，请检查配置及 Docker 日志。"
COMMITTED=true
if [[ "$HAD_OLD" == true ]]; then
  docker rm "$BACKUP"
fi
echo "$NAME 更新完成，端口：$PORT，配置：$ENV_FILE"
