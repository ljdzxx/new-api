# Gemini 调试日志跟进（2026-10-15）

来源：用户提供的本地 `gemini-10150124.txt`。本记录只保存诊断摘要，不复制原始请求正文、令牌或其他凭据。

## 日志确认的事实

- 渠道 6，类型 24（Gemini），客户端请求 `/v1/responses`，模型 `gemini-4-argon`，reasoning effort 为 medium。
- 实际上游路径为 `/v1beta/models/gemini-4-argon-medium:streamGenerateContent?alt=sse`。
- 原日志第 149、269 行分别记录 `context deadline exceeded`；两个请求各持续约 300 秒。没有上游响应状态/响应体或 Gemini SSE 帧记录，失败发生在获得上游 HTTP 响应头之前，不能归因于 Responses 输出事件转换。
- 两个完整失败请求的 request ID 不同、`retry=0`；表现为客户端在失败后发起新请求，不是同一次 relay attempt 内反复生成。日志还包含第三次发起记录，其结束状态未出现在所给片段中。
- GIN 的 HTTP 200 与内部 500 同时出现：等待上游期间的 SSE 心跳可提前提交 HTTP 200，后续失败只能通过 SSE 终态表达，不能再改已发送的 HTTP 状态。

## 模型名与协议核查

只读查询本机渠道配置确认，`gemini-4-argon-medium` 来自已有模型映射规则，不是新转换器擅自拼接后缀。配置中同时有 low/high/xhigh 分支。本次未修改业务数据库、模型映射或渠道类型。

使用渠道已配置的凭据，只做了不含提示词的只读模型列表 GET，并禁用重定向：

| 接口 | 结果 |
|---|---|
| `/v1beta/models` | HTTP 200，11 个模型；未包含 `gemini-4-argon` 系列 |
| `/v1/models` | HTTP 200，14 个模型；包含 `gemini-4-argon-low`、`gemini-4-argon-medium`、`gemini-4-argon-high` |

这支持“Argon 模型暴露在供应商 OpenAI 接口，而当前渠道使用 Gemini 原生接口”的判断。但模型清单可能不完整，不能单凭清单缺失断言某个端点绝对不支持该模型，也不能据此承诺切换后一定可生成。需要供应商确认 Argon 对应的协议和具体生成路径；本次未发送真实生成请求，未重放日志中的用户内容，也未自动改动渠道配置。

## 已修复的代码问题

1. **首部超时分类丢失**：新增 `service.UpstreamResponseHeaderTimeout`，保留超时阶段和期限；请求层返回 HTTP 504、错误码 `upstream_response_header_timeout`。没有收到真实上游状态时 `UpstreamStatusCode` 保持 0，不再伪装成上游返回 500。
2. **HTTP/2 原因丢失**：HTTP/2 传输可能返回 `context canceled` 而不保留取消原因；错误退出前检查 `context.Cause`，恢复首部超时类型。下游取消仍按 `client_disconnected` 分类。
3. **重复转换警告**：同一次请求中同类诊断只向控制台输出一次，仍保留各字段的管理日志记录和原有 32 项上限。`strict:false` 的 upstream 兼容诊断语义不变，不把这些警告误认为阻断请求的异常。
4. **鉴权日志输出明文凭据**：token received、候选校验、成功/失败及双鉴权头日志改用长度与指纹；Messages 的请求头/query 调试副本也进行脱敏，实际鉴权请求不变。原始日志文件未改写，修复仅影响后续生成的日志。

## 回归

- service 覆盖 HTTP/1.1 和 HTTP/2 的首部超时类型、下游取消、响应体持续读取，保留原有流式超时边界。
- controller 以本地等待首部的 HTTP 服务复现：无心跳返回 JSON 504；已发送心跳时返回一次 response.failed，均不产生成功终态或消费日志，用户/令牌预扣费退还。
- controller 回归确认配置映射后的 `-medium` 名称保持原样，没有为掩盖超时自动剥离供应商别名。
- middleware 覆盖成功和失败校验日志，以及请求头/query 脱敏；原始 Authorization 和 query 值不被修改。
- conversion diagnostics 覆盖 30 个 strict 字段只输出一次控制台警告，同时保留全部字段诊断及后续搜索约束提示。

验证结果：middleware、service、relay/channel、relay/common、relay、controller、types 相关包测试通过；Gemini/Vertex 包复验通过；全仓 `go test ./... -run '^$'` 编译通过，gofmt / `git diff --check` 通过。全仓检查首次在 Windows 清理临时测试 exe 时遇到文件占用，重跑命令正常退出；并非业务代码编译失败。

这些修改修复错误分类、日志暴露和调试噪声；供应商为什么不返回该原生生成请求，仍需协议支持说明或单独的真实生成联调确认，不能把本地回归通过等同于上游生成已恢复。
