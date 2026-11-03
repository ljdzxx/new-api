# Docker 部署

`deploy.sh` 使用项目源码构建 `noweb` 后端镜像，与根目录的 rebuild 脚本保持一致，不构建前端。目标主机需要 Bash、Git 和支持 `host-gateway` 的 Docker Engine（20.10 或更新版本）。

## 配置与启动

两个节点默认共用项目根目录 `.env`，通过只读挂载交给应用的 `--config` 加载，支持应用原有的 dotenv 语法。脚本不会将配置作为 Shell 执行，也不会修改配置文件。首次部署前准备好该文件。

| 节点 | 容器名称 | 宿主机 / 容器端口 | 强制 NODE_TYPE |
| --- | --- | --- | --- |
| master | new-api-master | 13000 / 13000 | master |
| slave | new-api-slave | 13001 / 13001 | slave |

以上端口、节点类型及 `TZ=Asia/Shanghai` 通过容器环境变量设置，优先于 `.env` 中的同名配置。镜像使用 `noweb` 编译模式。

在项目根目录执行（也可以从任意目录使用脚本的绝对路径）：

```sh
# 交互菜单
bash deploy/deploy.sh

bash deploy/deploy.sh start master
bash deploy/deploy.sh start slave
bash deploy/deploy.sh stop master
bash deploy/deploy.sh stop slave
bash deploy/deploy.sh update master
bash deploy/deploy.sh update slave

# 可选：指定其他 .env 文件
ENV_FILE=/absolute/path/.env bash deploy/deploy.sh start slave
```

初次启动时，若没有对应镜像则构建当前工作区源码。普通启动复用现有容器及其配置挂载；更换配置文件路径需要执行 `update`。修改同一 `.env` 文件内容后，执行 `stop` 再 `start` 重新加载。若编辑器通过替换文件保存配置，使用 `update` 重建容器以刷新文件挂载。

`update` 执行 `git pull --ff-only`、构建镜像，然后替换指定节点，不操作另一个节点。构建失败时保留原容器；新容器在 120 秒内未通过 `/api/status` 检查时，恢复原容器的名称及运行状态。恢复容器不能撤销新程序已执行的数据库迁移或写入。停止操作保留容器及持久化文件。

## 数据与连接

节点数据分别保存在 `deploy/data/master` 和 `deploy/data/slave`，挂载为容器 `/data`；日志在各自的 `logs` 子目录。默认相对路径 SQLite 文件也落在这里。原有本机运行的数据不会自动迁移；自定义文件路径应配置到 `/data` 内，才能使用该持久化挂载。

主从部署应在共享 `.env` 中配置同一个外部数据库、Redis 以及一致的会话密钥；两个独立目录中的 SQLite 文件不会自动同步。

容器内 `127.0.0.1` 指向容器自身。如果连接 `deploy-mysql.sh` 发布在宿主机 `4810` 端口的 MySQL，可在 `.env` 中使用以下占位配置，并替换用户名、密码和库名：

```dotenv
SQL_DSN=YOUR_DATABASE_USER:YOUR_DATABASE_PASSWORD@tcp(host.docker.internal:4810)/YOUR_DATABASE_NAME?parseTime=true
```

脚本添加了 `host.docker.internal` 到 Docker 宿主机网关的映射。宿主机数据库或 Redis 需要监听容器可访问的地址。诊断时可执行 `docker logs new-api-master` 或 `docker logs new-api-slave`。
