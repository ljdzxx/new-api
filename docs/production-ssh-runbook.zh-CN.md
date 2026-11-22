# 生产环境 SSH 与排查手册

归档日期：2026-11-15（Asia/Shanghai）。依据：2026-11-14–15 本项目排查会话、用户执行的诊断安装记录、当前本机 ray.sh 与 deploy/README.md。本次仅归档，未重新连接生产刷新状态。

## 下次收到“去生产排查一下”时

1. 先读本手册，直接使用已授权的 ray 只读 SSH 入口；不重复询问这里已有的主机、用户名、密钥路径或诊断权限。
2. 先执行 diag status、diag nginx；需要时间关联时执行 diag host，确认四个实例状态和当前路由。
3. 用户指定 master/slave 时遵从指定；没有指定时，按当前 Nginx 路由定位承接请求的实例。上次排查目标为 new-api master / 13000，不把历史指向当作当前状态。
4. 对目标执行 inspect、status probe、最近一小时 logs，必要时与另一节点对照。历史故障优先使用已授权的 `docker logs --since ... --until ... CONTAINER`，不受 diag 的最近 2,000 行限制。健康状态不等于模型推理成功。
5. 用问题相关 request ID、时间窗、channel_id 筛选证据。需要 API 复测时使用当前可用的授权凭据，先发小规模请求，立即关联目标实例日志。
6. 报告实际项目、节点、端口、渠道和请求结果；区分 SSH 连通、HTTP 成功、响应 completed 与子代理任务成功。

默认沿用已授权的只读诊断，不部署、不重启、不改渠道/Nginx。新增权限应说明准确缺项。**当前偏好是 SSE；用户已明确停止 WebSocket 测试，除非以后明确恢复，否则不发 WS 探针。**

## SSH 入口

| 项目 | 已知配置 |
| --- | --- |
| 生产主机 / SSH 端口 | 167.160.90.42:22 |
| SSH 用户 | ray |
| 本机私钥路径 | ~/.ssh/ray_readonly_key |
| 本次 Windows 私钥位置 | C:\Users\ray\.ssh\ray_readonly_key |
| 本地项目目录 | C:\vs_project\new-api |
| 本地终端 | PowerShell |
| 连接方式 | 强制命令门禁，支持授权诊断、docker logs 及只读 SFTP，不是交互式 Linux shell |

PowerShell 最小检查（整行执行）：

~~~powershell
ssh -T -i "$HOME/.ssh/ray_readonly_key" -p 22 -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=yes -o ConnectTimeout=15 ray@167.160.90.42 "diag status"
~~~

保留主机密钥校验。换机器缺少可信 known_hosts 记录，或指纹改变时，先核对可信指纹，不使用 StrictHostKeyChecking=no 绕过。不读取、打印或复制私钥内容。

同一轮可在本地 PowerShell 使用以下辅助函数：

~~~powershell
$prodSshArgs = @(
  '-T', '-i', "$HOME/.ssh/ray_readonly_key", '-p', '22',
  '-o', 'BatchMode=yes', '-o', 'IdentitiesOnly=yes',
  '-o', 'StrictHostKeyChecking=yes', '-o', 'ConnectTimeout=15'
)
function Invoke-ProductionDiag {
  param([Parameter(Mandatory=$true)][string]$DiagnosticCommand)
  & ssh @prodSshArgs 'ray@167.160.90.42' $DiagnosticCommand
  if ($LASTEXITCODE -ne 0) {
    throw "Production diagnostic failed: $DiagnosticCommand (exit $LASTEXITCODE)"
  }
}

Invoke-ProductionDiag 'diag status'
Invoke-ProductionDiag 'diag nginx'
Invoke-ProductionDiag 'diag inspect new-api master'
Invoke-ProductionDiag 'diag probe new-api master status'
Invoke-ProductionDiag 'diag logs new-api master 1'
~~~

每次只传一个白名单命令，不传 cd/cat/grep、管道、分号或 sudo 命令串；Docker 目前仅放行 `docker logs ...`。筛选在本机进行。门禁不提供任意 shell、SSH 端口转发、Docker 组或广泛 sudo。2026-11-17 已复核旧门禁还保留显式 `/usr/lib/openssh/sftp-server` 的只读 SFTP 分支，使用方式与读取边界见下文；不能将其等同于任意 SSH 文件读取或写入权限。

## 两项目、四实例与部署路径

| 项目 / 角色 | 容器名 | 镜像标签（历史配置） | 宿主机端口 → 容器端口 | 部署目录 |
| --- | --- | --- | --- | --- |
| new-api master | new-api-master | new-api-local:master | 13000 → 13000 | /opt/new-api |
| new-api slave | new-api-slave | new-api-local:slave | 13001 → 13001 | /opt/new-api |
| sub2api master | sub2api-ray-master-master-1 | sub2api-ray:master | 8080 → 8080 | /opt/sub2api/deploy-ray |
| sub2api slave | sub2api-ray-slave-slave-1 | sub2api-ray:slave | 8081 → 8080 | /opt/sub2api/deploy-ray |

这些是已确认部署映射，不保证未来状态不变。inspect 的 port_bindings 可能保留停止容器的配置，ports 才反映当时发布情况；不要仅凭配置端口断言服务运行。

### new-api

- 源码/部署根目录：/opt/new-api；部署脚本：/opt/new-api/deploy/deploy.sh。用法见 [Docker 部署文档](../deploy/README.md)，start/stop/update 不属于 ray 的诊断权限，不因“去排查”而执行。
- 默认共用 /opt/new-api/.env，只读挂载到容器 /app/.env。这里只记录路径，不读取或归档内容。
- master：/opt/new-api/deploy/data/master → /data；slave：/opt/new-api/deploy/data/slave → /data；各自 logs 子目录存日志。
- 部署脚本固定 PORT=13000/13001、NODE_TYPE=master/slave、TZ=Asia/Shanghai，构建 noweb 后端镜像。
- 更新一个节点不自动更新另一个。分别检查 image_id、image_created、started、health、restarts；同名镜像标签不能证明正在运行最新代码。
- 生产镜像曾没有 revision 标签，状态接口版本为空。不能用本地 HEAD 或镜像标签推断生产精确 commit。
- 部署文档要求主从共用外部数据库、Redis 和一致会话密钥；两个目录中的独立 SQLite 不自动同步。未读取生产 DSN，也未确认当前数据库引擎或连接值。

### sub2api

- 项目根目录 /opt/sub2api；Compose 工作目录 /opt/sub2api/deploy-ray；配置 /opt/sub2api/deploy-ray/compose.app.yml。
- Compose project：sub2api-ray-master、sub2api-ray-slave；service：master、slave。
- 数据卷 sub2api-ray-master_master_data、sub2api-ray-slave_slave_data，均挂载至对应容器 /app/data。
- 历史卷源路径：/var/lib/docker/volumes/sub2api-ray-master_master_data/_data、/var/lib/docker/volumes/sub2api-ray-slave_slave_data/_data。
- 2026-11-14 安装诊断权限时，sub2api master 为 running/healthy，slave 曾为 exited/unhealthy。仅为历史快照，下次必须刷新。

## 公网入口与路由

- 用户测试入口 https://api.jucodex.com，历史路径经过 Cloudflare Worker 再进入源站 Nginx。
- 本项目源站虚拟主机 alb.jucodex.com；历史配置文件 /etc/nginx/conf.d/alb.jucodex.com.conf；upstream 名 newapi_backend。
- 会话早期指向 127.0.0.1:13001（slave），后来改为 127.0.0.1:13000（master）。2026-11-15 00:25–00:28 的请求在 master 日志匹配。下次以 diag nginx 和请求日志重新确认。
- master.jucodex.com 是另一个 sub2api 入口，不是 new-api master；不可仅凭域名判断项目或角色。
- 同一 KEY 曾在渠道 312、345 间切换，最后一次 SSE 测试为 312。下次从 responses relay selected channel 日志确认，不当作永久路由。
- 尚未证明渠道 345 必然对应上述 sub2api 实例。未找到关联日志，不能单独证明该上游没有收到请求。
- 如需绕过 Worker 的 HTTPS 对照，保留 Host、TLS SNI 和证书校验。历史源站对照为 167.160.90.42:443 + alb.jucodex.com，不使用关闭证书验证的裸 IP 请求替代。

## 可用的只读诊断命令

PROJECT 仅接受 new-api / sub2api，ROLE 仅接受 master / slave。

| 命令 | 用途 / 限制 |
| --- | --- |
| diag help | 查看门禁支持的操作 |
| diag status | 汇总四容器状态、端口、挂载及部分标签 |
| diag host | UTC 时间、uptime、有限进程摘要 |
| diag nginx | 筛选后的 nginx -T 路由、代理及日志配置，不是任意配置文件读取 |
| diag inspect PROJECT ROLE | 指定容器与镜像状态、ID、时间、端口、挂载和允许的版本标签；不输出完整环境变量 |
| diag top PROJECT ROLE | 指定容器有限进程摘要 |
| diag logs PROJECT ROLE [HOURS] | HOURS=1–24，默认 1；最多最近 2,000 行，另有约 2 MB 输出上限和脱敏 |
| docker logs --timestamps --since START --until END CONTAINER | 2026-11-17 用户已开通并实测；直接按时间窗读取容器 stdout/stderr，无 diag 的 2,000 行限制；原始输出必须本机筛选脱敏 |
| diag probe PROJECT ROLE status | 主机回环 GET：new-api 为 /api/status，sub2api 为 /health |

~~~text
diag inspect new-api master
diag inspect new-api slave
diag inspect sub2api master
diag inspect sub2api slave
diag logs new-api master 1
diag logs new-api slave 1
diag logs sub2api master 1
diag logs sub2api slave 1
diag probe new-api master status
diag probe new-api slave status
~~~

门禁还支持 diag probe PROJECT ROLE websocket：token 仅从 stdin 读，只发 GET 握手，不发 response.create。**这里只归档其存在，不纳入当前默认流程；用户已停止 WS 测试。** 101 仅证明接入握手，不证明上游推理或子代理功能。

logs 最后一个数字是小时，不是行数；24 小时仍受最近 2,000 行限制。高流量时及时采集本次 ID，未命中需考虑截断、实例、时间及路由变化。只落盘和展示相关请求，并再次脱敏。

用户于 2026-11-17 明确授权直接执行 `docker logs`，并已在生产 root 终端完成配置：现有 SSH 门禁分派 `docker logs ...` 到 `sudo -n /usr/bin/docker logs ...`，sudoers 文件为 `/etc/sudoers.d/ray-docker-logs`，门禁备份为 `/usr/local/bin/sub2api-diag-entry.before-docker-logs-20261117-073316`。15:34（Asia/Shanghai）已实测能读取 new-api master 的历史错误日志，不需再次索要该权限。不因此执行其他 Docker 命令，也不延伸为部署、重启、补单或写数据库权限。

~~~powershell
Invoke-ProductionDiag 'docker logs --timestamps --since 2026-11-17T14:56:40+08:00 --until 2026-11-17T15:00:20+08:00 new-api-master'
~~~

调用日志命令时在本机捕获 stdout 和 stderr，先按订单号、请求 ID 或错误类型筛选，再对 `sign`、`key`、令牌和其他敏感字段脱敏后展示；不直接打印或落盘整个时间窗的生产原始日志。先取短时间窗，避免无边界读取或 `--follow` 长期占用连接。

Docker 外层日志时间曾为 UTC（Z），内层应用为 Asia/Shanghai。跨午夜时 request ID 日期与本地日期可能不同，用完整 ID 与带时区时间关联。

### new-api 历史日志的只读 SFTP 入口（2026-11-17 复核）

- Windows OpenSSH 使用 `sftp -s /usr/lib/openssh/sftp-server -b -`，沿用相同密钥、主机校验和连接参数。该分支在服务端以 `-R` 只读运行，文件权限仍有效；不上传、不修改权限。
- 已验证可以读取 `/opt/new-api/deploy/data/master/logs/oneapi-20261117.log`、`/var/log/nginx/alb.jucodex.com/access.log` 及 `/opt/new-api/` 下必要源码。日志达数百 MB，应按时间戳定位字节区间，在本机内存中筛选目标请求；不能整份下载或保存带签名的原始回调 URL。
- `controller/topup.go` 的易支付校验、入账错误当前使用标准库 `log.Printf`，写入容器 stderr；`logger.SetupLogger` 仅将 Gin writer 和项目 logger 写入每日文件。因此每日文件有回调访问记录，不等于有对应错误正文。
- 直接读取 new-api master 的 Docker JSON 日志文件曾被权限拒绝，不能绕过；2026-11-17 后续已由用户开通 `docker logs` 白名单，可通过时间窗读取同一容器的 stdout/stderr，无需再读原始 JSON 文件或让用户代查。

## 诊断门禁与权限维护

2026-11-14 用户以 root 执行 ray.sh 后，四服务诊断安装成功：

| 路径 | 作用 |
| --- | --- |
| C:\Users\ray\Downloads\tmp-ssh\ray.sh | 本机安装脚本原件，使用前确认仍存在 |
| /usr/local/bin/sub2api-diag-entry | 当时既有 SSH 强制命令门禁；名称含 sub2api，但已支持 new-api 诊断 |
| /usr/local/libexec/ray-services-diag/diag.py | 固定参数只读 helper |
| /etc/sudoers.d/ray-services-diag | 仅允许指定 helper 的 sudo 规则 |
| /usr/local/bin/sub2api-diag-entry.before-services-diag | 原门禁备份，保留旧白名单命令 |
| /var/backups/ray-services-diag.3Pf6aCIk | 用户当次安装输出的备份目录，仅作历史记录 |
| C:\Users\ray\Downloads\tmp-ssh\new-api-responses-ws.location.conf | 历史 Nginx 修复片段，不代表当前配置，不自动应用 |

管理员当时先执行 sudo bash ray.sh --check 只读预检，再执行 sudo bash ray.sh 安装。输出确认保留认证、转发限制和旧命令，不需要 SSH reload。ray 的受限连接不能执行此安装脚本。下次默认复用已有权限，不重新安装，不索要 root、不加 Docker 组。

若需要白名单外读取，先完成现有权限能做的定位，再说明准确缺项与原因；如确需扩展，由用户审核后以管理员身份执行范围有限的脚本。不假设 .env、数据库、容器 exec、重启或渠道修改已获授权。

## SSE / 子代理复测经验与证据

- 最近测试模型 gpt-6.1-sol。实际 KEY 不写入文件；新会话不能依赖旧运行时缓存，没有 KEY 也可先做 SSH 只读检查。
- SSE：POST /v1/responses、stream:true，小规模请求与短输出；拿到 x-oneapi-request-id 后立即对应到目标实例，核对渠道、终止事件、错误与结算。
- 渠道 345（2026-11-14）与 312（2026-11-15）均通过真实 Codex SSE 创建、结果回传和同一子代理追加任务。详情见 [审计报告](codex-subagent-audit.zh-CN.md)，历史通过不代替本次复测。
- 使用 codex-cli 0.167.0、隔离 CODEX_HOME/工作目录、read-only sandbox、一个子代理、provider supports_websockets=false；下次先确认本机版本。
- 回环记录转发器只允许 POST/SSE，阻止 GET/Upgrade并保留原 JSON；它是临时测试代码，不是生产组件，使用前审阅。
- --ephemeral 曾在创建继承历史的子代理时触发本地 no rollout found；隔离持久会话目录重试成功。保留失败证据，不把本地会话故障归给服务端。
- 渠道 312 返回过加密 spawn/followup message，需核对 encrypted_content、子请求及父代理实际收回的结果，不要求一定是明文。Codex 重放可能省略 response metadata，单独验证协议内容、call_id 和历史。
- production_probe 自定义工具脚本只能验证模拟编排；真实验收必须观察 Codex 原生协作工具执行、子请求和结果回传，不能只看最终成功标记。
- 应用结算日志不等同数据库余额对账，不把它称作完整账务审计。

历史本机结果目录（临时文件可能被清理，不作为永久依赖）：

~~~text
C:\Users\ray\AppData\Local\Temp\new-api-production-responses-probe\sse-channel345-20261114-235014
C:\Users\ray\AppData\Local\Temp\new-api-production-responses-probe\sse-channel312-20261115-002517
~~~

312 目录下 real-codex 保留首次临时会话失败；persistent-codex/real-codex 保留成功重试。关键时间与请求 ID 已归档在审计报告。

## 2026-11-17：渠道 324 的 HTTP Responses 400

- 故障证据来自用户提供的 2026-11-17 11:02:14（Asia/Shanghai）日志，请求 ID 为 `20261117030214289549247TJzx63vD`；路径为 `POST /v1/responses`，模型 `gpt-6.1-sol`，渠道 324，`channel_type=1`、`api_type=0`、`channel_pass_through=true`。返回 400：`function_call_output requires call_id on HTTP requests; continuation via previous_response_id is only supported on Responses WebSocket v2`。
- 本次实时只读检查：Nginx 的 `alb.jucodex.com` / `newapi_backend`（包括 `/v1/responses`）仍指向 `127.0.0.1:13000`；new-api master、slave 均 running/healthy、restarts=0；master 状态探针 HTTP 200。这是排查时状态，不单独证明故障时请求所在节点或模型请求成功。
- 本机 sub2api 仓库的 `backend/internal/handler/openai_gateway_handler.go` / `validateFunctionCallOutputRequest` 含有完全相同的错误文本。该实现进入这一错误分支的条件是：存在 `function_call_output`，校验发现至少一个工具输出缺失或空 `call_id`，同时没有非空 `previous_response_id` 或可识别的工具调用上下文。末尾提及 WebSocket 是该分支的通用提示，不证明请求曾使用 WebSocket，也不证明必须改用 WebSocket。未读取渠道 324 的上游地址或核实其部署版本，不能据此认定它对应本机某个 sub2api 实例。
- 当前 new-api 工作树的该透传分支使用原始请求体，禁用字段过滤及 item ID 清理不删除工具输出的 `call_id`；三个本地专项回归 `TestResponsesSanitizersPreserveOpaqueToolAndAgentData`、`TestRemoveDisabledFieldsPreservesRawNumbersAndOrder`、`TestPassthroughHeaderOverridesKeepRawBody` 通过。这不替代生产请求体取证或镜像版本核验。
- `calculated_quota=2807` 是估算；同一日志的 `pre_consume_path=minimum_balance`、`effective_preconsume_quota=0` 表示最低余额门槛策略下实际预扣为零，不是本次 400 的原因，也不是实际结算凭据。未做数据库余额对账。
- 取证限制：诊断门禁的最近 2,000 行上限使 master 的 `logs ... 24` 仅覆盖排查时最近几分钟，未命中原始请求 ID；相关 sub2api 只读错误/系统日志时间窗查询也未命中，不能用未命中排除其他上游。尚无故障请求体，不能区分真实工具结果漏关联 ID，与 Desktop 页面快照、自动化/委派启动上下文被包装成无调用 ID 的工具输出等兼容情形。
- 后续仍以 HTTP/SSE 为准：真实工具结果应携带原始 `call_id` 和可重放的对应调用历史；无真实调用的客户端注入上下文应由明确识别其协议的兼容层规范化，不能随意补造 `call_id`。进一步取证只需脱敏的 item `type/name/namespace/call_id`、调用对应关系及 `previous_response_id` 是否存在，不索要 API Key 或工具输出正文。本次未部署、重启、修改生产配置或发起模型/WS 探针。

## 2026-11-17：new-api 易支付异步通知返回 fail

- 用户提供收单号 `20261117145618502361`，三次通知显示 HTTP 200、正文 `fail`。实时 Nginx `alb.jucodex.com` / `newapi_backend` 指向 `127.0.0.1:13000`；new-api master 容器 `f317c212fda6` 健康运行，启动于当日 14:41:26（Asia/Shanghai），重启次数为 0，状态探针 HTTP 200。这里不把聚合状态输出中的其他项目当作本次排查目标。
- 只读读取 new-api master 每日日志，在 14:56:57、14:59:59、15:00:00 匹配到三次 `GET /api/user/epay/notify`，收单号均为上述号码，本站商户单号为 `USR1NOYifzaH1794898578`，支付方式 `wxpay`、通知金额 `5`、支付状态 `TRADE_SUCCESS`。访问日志仅证明请求到达；没有查询生产数据库或证明余额是否入账。签名不归档。
- 生产工作目录 `service/payment_epay_query.go`、`controller/epay_guard.go`、`controller/topup.go` 与本机提交 `11a3044aa4` 对应文件逐字节相同；没有容器构建提交标签，这不单独证明运行二进制与源码一致。
- 已复现协议问题：`QueryEpayOrder` 将渠道地址拼接为 `.../api/pay/query`，以 POST 和 MD5 签名查单；对现有渠道候选地址 `https://pay.88yyw.cn/xpay/epay/api/pay/query` 做不含密钥的只读探测，HTTP 200、响应为 `{"code":404,"message":"Not Found","data":{},"redirect":""}`。`/xpay/epay/api.php?act=order` 最初依据常见易支付协议及本机另一项目既有适配代码提出，并非来自该商户平台的官方文档；未填商户 ID 返回“请输入商户ID”，只填通知中的商户 ID 返回“请输入商户通讯密钥”。这些响应仅证明该 PHP 兼容候选入口会处理商户认证校验，不能证明 `act=order` 已被认证调用并返回正确订单。未读取或使用商户密钥，没有完成该笔订单的认证查单，不能将该候选写成已确认的正确查单接口，也不能断言成功响应结构及成功码。
- 15:34 复核：用户开通 `docker logs` 后，直接读取 `new-api-master` 的 14:56:40–15:00:20 +08:00 时间窗，共返回 3,989 行，在本机仅筛选出本单三条访问日志及三条错误日志。14:56:57、14:59:59、15:00:00 三条错误完全一致：`易支付回调校验失败 order=USR1NOYifzaH1794898578 err=epay order query failed: code=404 msg=`。至此已逐条确认，不再只是候选原因。
- 故障链路为：通知到达 new-api → 订单/渠道、本地签名、支付方式及通知金额检查通过 → 主动远程查单返回业务码 404 → 拒绝该回调并回复 HTTP 200、正文 `fail`。这三次均未进入 `CompleteEpayTopup` 入账步骤；不是三次通知没有送达，也不是这三次本地验签或充值限额拒绝。`msg` 为空是因为解析器只读 `msg`，而该网关错误字段名为 `message`。
- 排查结论：new-api 查单协议/路径适配错误，导致这三次真实付款通知均被主动查单校验拒绝。尚未查询订单数据库或认证查单，因此不把“三次回调未入账”等同于“该订单从未通过其他方式入账”，也不据此直接补发余额。修复协议并核对订单状态后，才能使用幂等处理安全重试。
- 本次仅状态、路由、目标历史访问日志、相关源码及无认证网关路由探测；未部署、重启、改渠道、改余额、补单或重放支付通知。修复应适配实际易支付查单协议和返回结构，保留签名、商户、订单、方式和实付金额核验，不能直接跳过远程验证或将 `fail` 改为 `success`。
- 后续用户提供该平台官方查单文档，明确 `GET /xpay/epay/api.php?act=order&pid=...&key=...`，支持 `trade_no` 或 `out_trade_no`，查询成功码为 `1`、支付成功状态为 `1`。据此在本地实现按渠道选择原 POST 签名协议或 PHP GET 密钥协议，配置说明见 [易支付多渠道配置与切换](epay-multi-channel.zh-CN.md#查单接口选择)。这属于用户提供的协议依据，不是生产认证查单验证；尚未部署修复或恢复本单。接口随订单渠道版本冻结，修改当前渠道不会自动更改本单历史版本。

## 2026-11-18：同一用户两笔易支付订单的通知核查

- 当日 `diag nginx` 指向 new-api master / 13000。按用户提供的两个本站单号筛选该实例 10:16:30–10:47:30（北京时间）的容器日志，再以同一单号和时间关联源站 Nginx 访问日志。
- 充值单与订阅单的单号生成时间相隔 16 秒，充值在先。订阅单 10:18:28 首次收到支付宝方式、279.30 元、`TRADE_SUCCESS` 通知，10:21:30–10:21:44 又收到五次相同流水的通知。充值单 10:19:32 收到支付宝方式、500.00 元、`TRADE_SUCCESS` 通知。两单易支付平台流水不同。
- 七次请求均为 HTTP 200。订阅单六条 Nginx 记录的响应长度都是 28 字节，充值单一条为 31 字节。本地 API 路由启用 gzip，以 Go 默认 gzip 验算，`fail` 对应 28 字节、`success` 对应 31 字节。长度与订阅通知被拒、充值通知被接受吻合。日志未包含响应正文、原始支付宝实收明细或本站余额流水。
- 商户补充其支付宝账单仅支付 279.31 元（有优惠），并确认 500 元充值成功入账，随后手工退款。订阅成功通知写的是 279.30 元，两种记录相差 0.01 元，原因待凭收单明细核实。
- 在 10:19:32–15:09:06 的源站访问记录中，14:42:28 有一笔后台充值退款请求，HTTP 200。日志未记录 POST 请求体或目标单号。现有只读诊断门禁没有账务查询命令，仍缺目标单的数据库终态、额度变动明细和收款方原始交易号。现有记录无法独立还原易支付平台把大单标为成功的操作。
- 本地 `MarkTopUpRefunded` 的余额订单分支要求订单此前为成功状态，记录退款并重算累计充值和等级。可用额度需另查余额变动记录，单独执行标记退款不会扣回该笔额度。


## 2026-11-18：微信充值通知金额与主动查单金额不同

- 北京时间 20:52:10，生产 new-api master（13000）收到充值单 `USR1NOTTQS5O1795006279` 的 `wxpay` 成功通知。目标订单日志记录 `epay query money mismatch: query=1.08 callback=1 order=1.000000`。
- 本地 `controller/epay_guard.go` 要求主动查单和通知算出的差额一致。该笔分别为 0.08 元和 0 元，处理函数在调用充值入账前返回 `fail`。诊断依据为生产容器日志和本地代码，账务终态未通过数据库核验。

## 2026-11-22：渠道 350 的 HTTP Responses 工具结果校验失败

- 北京时间 20:18:19，`new-api-master`（13000）记录请求 `20261122121818759308343YN9lEdeM`，`POST /v1/responses` 经渠道 350 转发 `gpt-6.2-sol`，渠道开启请求体透传，约 285 ms 后返回 HTTP 400，错误为 `function_call_output requires call_id on HTTP requests`。排查时 Nginx 的 `/v1/responses` 指向该实例，状态探针返回 200。
- 在 master 的 20:17:18–20:19:20 日志中，按请求 ID 关联同一窗口内选中渠道 350、该模型的记录和最终访问记录，找到 5 个 HTTP 200、4 个 HTTP 400。渠道 350 的相同校验错误涉及 4 个不同请求 ID。
- new-api 将这条错误记录在渠道 350 名下，报错指向工具结果的 `call_id`。定位具体输入项及字段在哪一层缺失，需要比对该请求入站与转发时的脱敏字段。
- 本次请求按最低余额策略计算额度 10163，钱包实际预扣为 0。日志仅反映预扣阶段。

- 扩展到 20:10–20:55 的 master 日志，按请求 ID 去重，共 8 笔相同报错，均属于同一用户、同一令牌、渠道 350。首末时间分别为 20:18:19 和 20:35:12。7 笔使用 `gpt-6.2-sol`，1 笔使用 `gpt-6.1-sol`。同窗口中选中渠道 350 和 `gpt-6.2-sol` 的请求有 127 笔最终 HTTP 200。8 笔均无 item ID 清理或 reasoning 恢复日志。示例请求同秒、同路径、同状态的源站访问记录只有一条，User-Agent 含 Codex 标识。
- 本地以 HTTP 模拟上游捕获渠道透传的实际请求体。并行调用的结果倒序返回时，原有 `call_id` 保持对应，包括触发非法 item `id` 清理的情况。客户端省略 `call_id` 或仅传空格时，原值也会到达上游。原生 SSE 的工具调用 added、done 和 completed 三个事件保留了上游提供的 `call_id`。生产既有日志缺少工具项结构，无法回溯这 8 笔的入站字段。

- 另查北京时间 18:42–18:53，渠道 350 对用户 ID 374 的 7 个独立请求返回相同 `function_call_output requires call_id` 错误，首末为 18:43:53 和 18:51:06，涉及 5 个模型。结合 20:10–20:55 窗口，两个用户 ID（374、140）共出现 15 笔同文 400，对当日影响范围只取下限。
- 11:23–11:27 窗口内，11:25:04 的请求 `20261122032503947935152Lv0wkQ7u`（用户 ID 4090、渠道 350、`gpt-6.2-astra`）返回 HTTP 400，错误原文指出该模型不支持值 `none`，列出的支持值为 `low`、`medium`、`high`、`xhigh` 和 `max`。该窗口命中 `function_call_output requires call_id` 的请求数为 0。

本文含内部地址和运维路径，未经要求不公开发布。实际 API Key、SSH 私钥、生产 .env 及未筛选完整日志不写入本文或 Git。

