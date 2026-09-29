# 分组监控

公开页面为顶部导航“可用性”（`/availability`），旧地址 `/monitor` 会跳转到此页面，不再提供独立的“分组监控”导航入口。配置入口为系统设置中的“监控设置”。

## 部署

所有实例必须连接同一个 Redis、同一个配置数据库。仅 master 调度检测；项目现有约定是 `NODE_TYPE=slave` 为从节点，其余值为 master。任务使用 Redis 租约防止重复执行。

SVG 测试使用 HTML 预览，不需要 Chrome、Playwright、截图服务或 Docker。Go 服务先将题目、模型原始回复、结果和 HTML 写入“SVG 本地输出目录”，再上传 Cloudflare R2。

默认目录为 `data/monitor-svg`，相对于 Go 进程的工作目录。在本项目根目录启动时为 `C:\vs_project\new-api\data\monitor-svg`。可在监控设置中填写绝对目录，例如 `D:\monitor-output`。目录属于实际执行任务的 master，失败请求的部分回复也会保存。本地文件不会随 R2 保留策略删除。

在“绘图设置 → 图片生成 R2 存储”配置账号、Bucket、Endpoint、Access Key ID、Secret。监控复用连接凭据，但拥有独立对象前缀和链接有效期；不要求同时开启图片生成结果转存。R2 凭据需要对象上传、读取、列举和删除权限。

## 配置

监控分组 JSON 示例：

```json
{"codex-pro":{"model":["gpt-5.5","gpt-5.6-sol"],"svg_model":"gpt-6-astra","logic_model":"gpt-6-astra","key":"sk-example","protocol":"responses","svg_test":true,"logic_test":false,"active":true,"order":0}}
```

密钥必须是绑定相应分组的本站令牌，在 root 管理员的监控设置中明文展示和编辑；公开监控接口不返回密钥。探测地址可以是站点根地址、`/v1` 或完整 `/v1/responses`、`/v1/messages`；最终路径根据分组协议自动选择。所有检测经过正常计费和请求统计。

- `protocol`：填写 `responses` 或 `messages`，分别使用 OpenAI Responses 或 Anthropic Messages 协议。不要填写 `responses|messages`。
- `svg_model` / `logic_model`：每组的绘图、逻辑专用模型，可不在 `model` 列表内。
- `svg_test` / `logic_test`：该组是否执行对应测试；启用时必须填写对应专用模型。
- `active`：控制分组展示和后台检测。设为 `false` 后，该组从公开页面及其趋势、作品接口隐藏，并停止发起可用性探针、SVG 和逻辑检测；已经发出的检测请求会完成本次执行。真实用户请求的指标仍会采集，历史数据继续遵循保留策略。改回 `true` 后恢复展示和检测，省略时默认为 `true`。调度器每 15 秒读取一次当前节点配置，多节点部署需等待 master 同步设置。
- `order`：分组展示顺序，整型，越小越靠前；未填写时默认为 `0`，相同值按分组名称排序。页面选择“默认排序”时使用此顺序。
- `model`：可用性探测及用户请求指标统计的模型列表，不决定绘图、逻辑测试模型。

旧配置自动兼容：缺省协议为 `responses`，缺省 `active` 为 `true`；旧全局 SVG 开关和模型迁入各组，旧逻辑开关迁入各组并使用该组第一个模型作为专用模型。显式的 `false` 优先于旧开关。保存后采用新格式；全局界面保留题目、频率、超时、并发及存储配置。

可用性逐个检测 `model` 中的模型。逻辑题仅调用 `logic_model`，答案去除首尾空白后按 `logic_match_mode` 匹配：`exact`（完全相等，默认）或 `contains`（包含指定字符串）；两种方式都区分大小写。SVG 仅调用 `svg_model`，每组执行一次。三类任务频率互相独立，各组测试均使用自身 `protocol`。

每次 SVG 测试在本地 `<输出目录>/<分组哈希>/<时间戳-记录ID>/` 保存 `prompt.txt`、`response.txt`、`result.json`；模型完成应答时额外保存 `artwork.html`。文件保存后上传到 R2 `<对象前缀>/<分组哈希>/<时间戳-记录ID>/` 的对应路径。未完成的请求也保留原始部分应答，便于排查；上传失败会留存本地文件及错误码。

模型测试状态和文件处理状态独立：绿色为完成应答且逻辑判定通过（SVG 只看完成应答）；黄色为请求失败或未完整结束；红色仅表示逻辑题答案不匹配。HTML 预览生成/上传失败不会把完成应答的 SVG 请求改成失败，会单独显示文件处理错误。HTML 预览移除模型返回的脚本、主动执行元素和事件属性；CSP 只允许应用内置缩放脚本的固定哈希，iframe 仅授予脚本权限，不授予同源权限。缩放脚本按整份 HTML 尺寸等比适配容器，外部请求仍被阻止，不使用旧 SVG 元素白名单评判绘图。

“模型检测”列上方显示逻辑题历史，下方显示 SVG 历史，每种按“每组每类测试历史最多记录数”和“测试历史保留天数”保存，默认 48 条、8 天，任一条件超限即从历史和详情中清理。数量范围为 1–1000，天数为 1–90。降低配置后在下次读取、写入或每分钟维护时清理；增加配置不会恢复已删除记录，也不会延长旧详情原有的 TTL。历史按请求开始时间排序，保留当次题目、模型、应答（包括部分回复）、结束时间、耗时、匹配方式、预期答案和结果；修改题目或测试模型不会改写旧记录。详情按需读取，不将完整模型回复塞进页面列表。记录依赖 Redis 持久化策略；SVG 本地归档持续保留。

点击逻辑题方块打开详情；点击 SVG 方块联动“预览”列，并可通过“查看所选测试详情”读取题目与回复。默认预览最新已上传 HTML。R2 每组按设置保留最近 N 次 SVG 记录（包括失败记录），上传后及 master 每分钟清理一次整套文件；旧版 HTML/PNG 对象一起兼容清理。旧作品仍可按 HTML 预览，旧版只保存 latest 的记录不能追溯为完整历史。预览已清理时明确显示无预览，不会跳到其他作品。

临时链接有效期为 1–168 小时；链接过期不代表对象删除。同一作品的 iframe 在定时刷新时保持原导航地址，避免每次重新签名后反复加载。配置“SVG 公开访问地址”后，新作品使用稳定的域名 URL；上传仍使用原 S3 Endpoint。旧作品通过本站兼容预览接口添加缩放处理，浏览器短期缓存，原 R2 文件不改写。详见 [R2 CDN 配置指南](monitor-r2-cdn.md)。

## 统计口径

- 所有节点采集，监控统计不写数据库；配置使用现有设置表。
- 请求结束时记一次样本，内部重试不重复计数；消费记录入口标记成功，不受数据库消费日志开关影响。
- 按实际使用分组、客户端请求模型匹配监控名单；无法归属的请求不计入。
- SSE 首个 `data:` 或 `event:` 到达客户端写出点即记 TTFT，空事件也计入；纯注释心跳不计入。
- 仅缓存读取 token 大于零且不超过总输入的 usage 参与缓存率；每笔比率平均，相当于按模型样本数加权。
- 原始请求样本保留 2 小时，窗口精确筛选最近 1 小时。每分钟 TTFT 聚合和探针曲线保留 25 小时，支持 1h/6h/24h。
- 测试历史及详情按配置保留（默认每类 48 条、8 天）；可用性最新结果和作品索引仍保留 8 天。所有 Redis Key 均有 TTL。持续写入的样本集合同时按时间修剪。
- 无 Redis 时不展示数据、不启动任务；Redis 写入失败或采集队列满会记录错误，不中断 API 转发。

## 验证

运行 `go test ./pkg/groupmonitor ./setting/group_monitor ./middleware ./service ./controller ./model`，以及 `cd web && bun run build`。真实 R2 联调需要已配置的连接及本站探测令牌。首次探测在启用后下一次调度检查执行；slave 设置同步遵循项目原有同步周期。

已有的 `TestLocalSQLiteModelPricingEndToEnd` 依赖本机 SQLite 中的特定模型定价配置。数据库不具备这些测试数据时，可用 `-skip '^TestLocalSQLiteModelPricingEndToEnd$'` 排除此项；监控专项测试可通过 `-run Monitor` 独立运行。

## 失败排查

后端日志搜索 `group monitor failure`（模型请求/答案判定）和 `group monitor artifact`（本地保存/HTML/R2）。通过 `job`、`group`、`model`、`kind`、`code` 定位。原始网络异常只进入后台日志，公开接口返回分类错误码。模型流超时/中断时，已收到的回复仍写入记录和 SVG 本地输出目录。上传失败没有自动重试，文件保留供排查。

旧 `screenshot_url`、`max_output_tokens` 配置被忽略，重新保存配置后去除。

监控不提供“测试最大输出 token”配置，也不在 Responses / Messages 探测请求中发送 `max_output_tokens` / `max_tokens`。旧配置中的此字段会被忽略，重新保存时移除。SVG、逻辑题与可用性检测均采用此行为。Messages 转发仍遵循本站 Claude 模型默认参数设置；模型提供方的输出能力限制和单次请求超时独立生效。

## 旧截图部署（历史参考）

以下为旧版本部署记录，当前监控功能不再调用此服务，不需要运行。镜像 `new-api-monitor-screenshot` 与目录 `deploy/monitor-screenshot` 仅保留历史兼容参考：

旧版本的截图容器部署命令：

```sh
docker build -t new-api-monitor-screenshot deploy/monitor-screenshot
docker run -d --name new-api-monitor-screenshot --restart unless-stopped --init --shm-size=256m -p 127.0.0.1:3011:3011 new-api-monitor-screenshot
```

旧版本曾使用 `http://127.0.0.1:3011/screenshot`，当前设置页已移除该字段。

旧 Node 服务目录 `deploy/monitor-screenshot` 与浏览器环境变量 `CHROMIUM_EXECUTABLE_PATH` 不再参与当前监控流程。


## 页面请求节奏

监控页面首次加载及每轮更新只调用一次 `GET /api/monitor?hours=1&models={}`，返回所有可见分组的统计、趋势、逻辑/SVG 历史摘要和预览链接。分组数量不会增加轮询接口数。切换时间范围或某组模型时仍调用同一接口，`models` 为分组名到所选模型名的 JSON；排序在本地完成。只有点击“测试详情”时才读取 `/api/monitor/record` 完整回复；点击 SVG 方块仅切换已有预览链接。浏览器加载 HTML 文件属于作品资源请求，不会再触发分组数据请求。旧 trend/history/artworks 接口仅保留兼容旧页面，新页面不调用。聚合接口采用 15 秒服务端缓存，不会通过 HTTP 调用自己的子接口；单组 R2 链接失败单独标记，不影响其他数据。

监控页面通过统一读取队列发起请求，同一标签页去重；支持 Web Locks 的浏览器（包括本地 Chrome）还会在同源标签页之间共享读取锁和 30 秒摘要缓存。所有监控数据读取依次执行，相邻网络请求至少间隔 1 秒。详情正文不写入浏览器持久缓存。

定时更新在上次读取完成后等待 30 秒，不叠加未完成轮询。页面隐藏时停止轮询并取消当前读取，组件卸载后取消排队请求，切换语言不重新拉取数据。遇到 429 共享暂停至少 180 秒，并遵循更长的 Retry-After；其他网络错误从 30 秒开始指数退避，最多 5 分钟。原系统限流阈值保持不变。请求失败时保留现有表格和预览，不通过卸载重建重复触发子请求。
