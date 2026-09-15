# Responses 流中断排查记录（2026-09-15）

请求 ID：`20260915094743112596115G6ntbn3p`。本地时间 17:47:43–17:47:47（UTC 09:47:43–09:47:47），模型 `grok-4.6`，渠道 76，上游 `10.34.12.81:8317`。

## 已证实的结论

本次请求持续 4.594 秒，系统配置的 15 秒 Ping 尚未到期。配置项为 `general_setting.ping_interval_enabled`、`general_setting.ping_interval_seconds`，定义在 `setting/operation_setting/general_setting.go`。请求等待响应头期间的 Ping 在 `relay/channel/api_request.go` 的 `startPingKeepAlive` 中创建 ticker；开始读流后的 Ping 在 `relay/helper/stream_scanner.go` 中创建 ticker。两处均不立即发送 Ping，也不会用 Ping 间隔作为请求超时。

日志对应的代码链：

1. `relay/helper/stream_scanner.go` 等待 `c.Request.Context().Done()`，记录 `StreamEndReasonClientGone`。这是 new-api 的入站请求 context，和连接 CLIProxyAPI 的出站请求是两条不同的连接。
2. scanner 清理函数关闭 `resp.Body`，解除上游读取阻塞。原代码仍将这个主动关闭产生的 `use of closed network connection` 记录为 scanner 错误。日志打印顺序不代表关闭上游连接先于入站取消。
3. `relay/channel/openai/relay_responses.go` 原先将所有缺少 `response.completed` 的结束归为 502，包括 `client_disconnected`。
4. `controller/relay.go` 继续记录渠道错误、错误日志并尝试写 SSE 终止错误，最终因入站 context 已取消而再次失败。

GIN 的 HTTP 200 表示 SSE 响应头已经提交，不表示已经收到 `response.completed`，也无法在流中途改成 HTTP 502。

`received=15` 是读到的 `data:` 记录数，不是正文 token 数，也不证明已成功向客户端转发 15 条。`output_text_bytes=0` 只证明没有累计到 `response.output_text.delta`；已收到的内容可能包括握手、推理、工具事件。旧日志不包含这 15 条事件类型，不能据此确认客户端因何退出。

## 上游源码核查

核查目录：`../router-for-me/CLIProxyAPI`。

- `sdk/api/handlers/stream_forwarder.go` 的 `ForwardStream` 在 `c.Request.Context().Done()` 后调用 `cancel`。new-api 关闭到 CLIProxyAPI 的连接时，上游取消任务是正常传播。
- `sdk/api/handlers/openai/openai_responses_handlers.go` 的 `handleStreamingResponse` / `forwardResponsesStream` 会转发事件，并对没有终止事件的上游 EOF 生成错误。
- `internal/runtime/executor/xai_executor_stream.go` 的 `ExecuteStream` 使用 `http.NewRequestWithContext`，通过 `NewProxyAwareHTTPClient(..., 0)` 发起请求；该执行路径没有 4～5 秒计时终止逻辑。生产环境是否选择此执行器，仍需 CLIProxyAPI 的路由配置或请求日志确认，模型名本身不能证明执行器类型。
- `config.example.yaml` 的 `streaming.keepalive-seconds` 默认关闭，它是上游向 new-api 发送的心跳配置，和 new-api 向客户端发送的 Ping 独立。

没有发现证据表明本次应通过修改 CLIProxyAPI 的配置或代码解决。不能排除客户端因某个上游事件内容不兼容而取消；确认这种情况需要事件记录和客户端报错。

## 本次代码修复

- 客户端取消或下游事件写入失败使用独立的 `client_disconnected` 错误；内部状态码 499，上游状态码为空（0）。不向已经断开的客户端发送 499。
- 保留部分用量结算和原有退款流程；取消不再进入渠道失败处理、状态码映射、渠道重试及终止错误补写。
- scanner 主动关闭响应体产生的错误不再当成上游网络故障；真实上游 EOF、畸形数据和 `response.failed` 仍按原有失败路径处理。
- 结束日志增加最后事件类型、成功转发事件数、距最近转发的时间、入站 context 错误、直连对端和 User-Agent，不记录提示词或事件正文。

测试包括真实 TCP 连接：启用 15 秒 Ping，上游发送 15 条非正文事件后保持连接，客户端主动关闭响应体。另有完整 Relay 测试检查无正文及已有正文两种取消情况，验证只调用一次上游、不补写失败事件、不生成上游错误日志、结算不被后续退款撤销。已有真实上游失败测试保留。

验证结果：以下命令均通过。并发检测过程中修正了原 scanner 测试并行修改全局配置的问题；原依赖毫秒级休眠比较速度的测试改为用通道同步验证读取与处理解耦，避免 Windows 定时器精度导致偶发失败。

```text
go test ./relay/helper ./relay/channel/openai ./relay ./controller ./types
go test -race ./relay/helper ./relay/channel/openai ./controller -run 'Test(StreamScanner|ResponsesStream|ResponsesRelay)'
```

## 生产环境完成根因定位所需信息

仅凭现有服务端日志，无法确定客户端自身、客户端之前的网络，或 new-api 前置反向代理中哪一方触发取消。本地源码修复解决的是错误归因，不能恢复已被外部关闭的连接。

按同一个请求关联以下信息：

1. 客户端名称、版本及 17:47:47 的原始异常，确认是否为超时、主动停止、SSE/JSON 解析失败或工具协议错误。
2. new-api 前的 Nginx/CDN/负载均衡拓扑与该时刻日志。若有 Nginx，检查实际生效的 `proxy_read_timeout`、`send_timeout`、`proxy_buffering`，以及 access/error log 中的客户端提前关闭或 upstream timeout；不能只检查配置模板。不要仅因本次持续约 5 秒就推定超时配置是 5 秒。
3. CLIProxyAPI 同时刻的模型路由、最后事件及取消/失败日志。其请求 ID 未必与 new-api 相同，可用时间、来源 `10.34.12.80` 和模型交叉关联。
4. 使用同一请求在获授权的生产网络内对比客户端直连 new-api、经过前置代理、直连 CLIProxyAPI 的结果。保持模型、请求体及客户端一致，避免把不同协议或不同负载的结果当成证据。

配置建议：本次无证据要求改动 15 秒 Ping。new-api 的 `RELAY_TIMEOUT` 默认 0（出站 HTTP 总超时），`STREAMING_TIMEOUT` 默认 300 秒（扫描空闲超时），它们在 `common/init.go` 中读取，与 Ping 是三个不同概念。真实扫描超时应产生 `timeout`，不是本次的 `client_gone`。

另一个日志细节：`9143 × 1 × 0.095 = 868.585` 与日志中的 3203 不相等。`relay/helper/price.go` 的活动计费策略分支通过 `billing_policy.CalculateBilling` 计算输入和预计输出成本，后面的日志仍打印简单的 `scaled_formula` 文本；所以该日志文字不足以反推 3203 的计算过程。要核对金额需查看该模型实际启用的计费策略。本次已进入信任额度路径且预扣为 0，没有证据表明额度计算导致连接取消。
