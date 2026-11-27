# Anthropic 中转与 upstream 对比（2026-11-27）

## 比较基准与结论

- 远端：`upstream = https://github.com/QuantumNous/new-api.git`。本次执行 `git fetch upstream`，随后 `git ls-remote upstream refs/heads/main` 再次确认 main 为 `81c1fd20efade39a5d5692c8018dfef4cfb30e1d`，提交时间 2026-11-26 13:51:26 +08:00。
- 本地 HEAD：`cc943de90b`。本报告比较本地工作树与该 upstream/main；不代表生产二进制提交已核验。既有运维手册修改保留。
- 建议按功能移植正确性修复，不整体覆盖 Claude 文件或立即迁移 relaykit。上游默认路径也不会把原生/透传请求的无效 thinking 自动改成 adaptive，因此同步最新代码不能直接解决渠道 198 的 400。

## 主要异同

| 范围 | 上游 | 本地 | 建议 |
| --- | --- | --- | --- |
| thinking 能力 | `relaykit/relayconvert/reasoning/claude.go` 包含 Sonnet 5.5 adaptive / between_tools 规则 | `relay/claudethinking/claude.go` 已移植对应规则 | 不重复移植整套规则 |
| 原生 thinking | shared/claude/reasoning.go 的无跨协议、无后缀分支保留原生控制 | apply.go 同样保留；ApplyNative 在透传时直接返回 | 当前 400 需要独立兼容策略与参数证据 |
| 多行 anthropic-beta | 合并 `Header.Values` 的全部值 | adaptor.go 使用 `Header.Get`，只取首值 | 高优先级移植；单个头中的逗号列表不受此缺陷影响 |
| thinking.block_binding | 保留会话控制字段 | DTO、ApplyNative 转换与专项测试已覆盖 | 已有对应实现；不能将整笔上游提交视为完全缺失 |
| 缓存与零 usage | 判断输入缺失时计入 cache read/write；有输出正文而输出 usage 为零才估算 | FinalizeStreamUsage 在输出零或中断时，仍以 PromptTokens==0 补输入估算 | 高优先级移植判断及边界测试；可能把全缓存输入补成普通输入，实际费用影响需独立验证 |
| 工具参数大整数 | `4d962e464d` 保留 Claude 响应工具参数数字 | service/convert.go 的 ResponseOpenAI2Claude 使用 map + common.Unmarshal，数字会走 float64；请求转换也有类似解析 | 移植数字保真，使用已有 common.UnmarshalUseNumber/原始 JSON；遵守项目 JSON 包规则 |
| 工具 schema 顺序 | `d0b840e75f`；ClaudeRequest.UnmarshalJSON 将 tools 保留为 RawMessage | 本地 Tools 为 any，ClaudeRequest 无对应定制解码 | 非透传路径需补齐；原始请求透传不受这次 DTO 重序列化影响 |
| DTO 扩展 | 有 safeguards、chat_template_kwargs | 本地 ClaudeRequest 缺失这两项 | 按需同步，覆盖非透传重序列化保留测试 |
| 跨协议转换 | Gemini/Responses 到 Claude 已接入 service.ConvertRequest，含 Responses SSE 转换 | Claude adaptor 对这两种入口仍返回 not implemented；Chat 转换已有 | 若需要这些入口，再单独实施；不属于这次原生 Messages 400 的原因 |
| SSE | 使用共享流处理/转换路径 | 有独立 ClaudeStreamPassThroughHandler，原始字节转发、usage 旁路解析、首包及中断处理 | 保留本地定制，移植时覆盖透传和非透传两条路径 |
| 插件与架构 | relaykit DTO/types、统一转换、requestplugin 钩子 | 原 dto/types/service + 本地 claudethinking | 属于系统性迁移，不宜仅复制 adaptor/handler |

## 同步顺序

1. 多行 beta 头、缓存/零 usage 判断、工具参数数字与 schema 顺序，分别移植并增加边界测试。
2. 按业务需要补 DTO 字段；保留本地 SSE 透传、上游 usage 独立记录、计费安全及请求诊断。
3. Gemini/Responses 到 Claude、插件框架与 relaykit 迁移单独规划。
4. 当前 thinking 400 单独处理：先比较失败/成功请求的 type、budget、effort。生产相邻请求输出上限为 64 与 64000，不能假设是同参数重试。上游的默认原生保留语义不能解决这类参数兼容问题。

## 证据与验证范围

- 相关上游提交：`767a82b876`（beta 头、block_binding）、`1b63c28dcc`（零 usage/缓存）、`4d962e464d`（工具数字）、`d0b840e75f`（schema 顺序）。以上是行为移植依据，不表示适合直接 cherry-pick 到旧架构。
- `go test ./relay/claudethinking ./relay/channel/claude` 通过。这是已有测试基线，不证明本报告指出的未覆盖边界已修复。
- 本次仅更新远端跟踪引用、审阅代码、运行自动化测试并新增报告。没有合并上游、修改业务代码、启动前后端应用服务或部署生产。

## 后续实施：四项选择性同步（本地，2026-11-27）

用户随后授权同步 beta 头、缓存 usage、工具数字保真、Schema 顺序，已在本地适配：

- beta 头合并全部同名行，保留每行已有的逗号列表。
- 流结束判断输入缺失时包含缓存读写；保留全缓存请求的普通输入零值、空输出拒绝的输出零值，并对确实缺失和中断的 usage 保持估算。本地最终计费的 `hasBillableTokenUsage` 已支持缓存单独计费，因此不重复移植上游计费实现，增加回归测试确认。
- 工具参数在 OpenAI/Claude 请求、响应及 ParseContent 中使用 common JSON 包的数字保真解码，覆盖超过 2^53 的整数和嵌套数组。Claude adaptor 原有直接 JSON 调用同时改用 common 包。
- Claude tools、input_schema 与 OpenAI function parameters 解码后保留原始 JSON。保留 map 构造兼容，适配 AddTool/GetTools、Gemini schema 检查以及 OpenAI token 估算文本，避免将 RawMessage 当字节数组估算。
- 测试覆盖多行 beta、两条流路径的缓存/缺失 usage、双向工具数字、schema 转换与过滤后的顺序、缺省/null 字段及显式 false/0。另修正 Gemini 旧测试调用 WriteResponsesStreamFailure 时遗漏现有 info 参数的编译错误。
- 全仓 `go test ./...` 除 service 的 `TestUpstreamUsageSettlementAndRefund`、`TestTaskUpstreamUsageMissingRefundsTokenPricingOnly` 外通过。两者报 `invalid DDL, unbalanced brackets`；通过 Go overlay 使用修改前 HEAD 的业务文件复测，同样失败，确认属于既有 SQLite 测试问题。本次不扩展修复数据库迁移。
- 本次未提交、推送、部署或启动前后端应用服务；thinking 模式兼容不在这四项同步范围内。

## 复审遗漏与补齐（本地，2026-11-27）

复审使用临时 Go overlay 测试确认 AWS 最终解码、参数覆盖 copy/move/append、小米原始工具规范化仍会把 `9007199254740993` 改为 `9007199254740992`，并将 Schema 的 z/a 顺序改成 a/z。用户授权后补齐：

- AWS 的 AwsClaudeRequest 再次解码时保留原始 tools JSON，并用 common.UnmarshalUseNumber 保留消息数字；透传分支直接删除 model/stream 字段，避免整包 map 重序列化。AWS beta 列表同时合并全部同名头。
- 参数覆盖 copy/move 使用原始 JSON token，append/prepend 保留原数组项的 RawMessage。计算型选择器（例如 items.#）没有原始 token，仍按计算值写入，避免破坏已有功能。
- 小米仅为原始工具对象补充缺失/空白的 type，不重建 input_schema；支持原始工具列表、AddTool 后混合列表及重复规范化。
- 新增正式回归测试覆盖以上最终路径：Schema 顺序、大整数、嵌套数组、科学计数法、false/null、标量类型、计算型选择器和未知工具字段。
- AWS、relay/common、小米包测试通过；关联 Claude、Vertex、DTO 与 relay 测试也通过。一次组合命令在测试通过后清理临时 relay.test.exe 时遇到 Windows Access is denied，随后单独复测 relay。未启动应用服务或部署生产。

## 请求字段补齐（本地，2026-11-27）

- ClaudeRequest 新增 safeguards 与 chat_template_kwargs，与上述上游基准一致，均使用 json.RawMessage 和 omitempty。
- 非透传的原生 Claude 请求转换保留字段内容；缺省时不发送，显式 null、空对象、嵌套 false/0、大整数、科学计数法及对象字段顺序均保留。
- 新增回归测试覆盖请求解码、DeepCopy、Claude adaptor 转换、禁用字段过滤及无关参数覆盖的完整序列。
- `go test ./dto ./relay/channel/claude ./relay/claudethinking ./relay/channel/xiaomi ./relay/channel/aws ./relay/channel/vertex ./relay` 全部通过。本次未提交或部署，也未启动应用服务。

## Gemini / Responses → Claude（本地，2026-11-27）

基于同一上游提交 `81c1fd20efade39a5d5692c8018dfef4cfb30e1d` 的请求转换、Gemini 工具历史关联和 Responses 流转换行为，适配本地架构，补齐 Anthropic adaptor 的两个入口。没有引入 relaykit，也没有扩展 AWS / Vertex adaptor 的入口支持。

- 请求：支持系统指令、文本、图片/PDF/文本文件、函数定义、工具调用/结果、采样参数、输出上限及 thinking。Gemini 无 ID 的调用按名字和调用顺序配对结果；Responses namespace/custom 工具沿用本地可逆映射，响应恢复原工具身份。工具结果的媒体保留为内容块，普通 JSON 数组/对象保留为结果文本。
- 保真：GeminiPart 解码使用 common.UnmarshalUseNumber，避免请求尚未转换时已丢失大整数精度；Schema 保留原始 JSON，Gemini Schema 的类型和 nullable 按关键字修正，保留 properties 顺序。请求可选零值/false 保留；显式 max output tokens 为零按上游 Claude 转换语义使用默认值，默认值和派生值仍受现有 MaxTokensLimit 限制。
- 响应：普通 JSON 与 SSE 都返回客户端入口协议。Responses 保留 thinking summary、文本、函数参数增量、稳定 item ID / output_index / sequence_number，以及完整终态；Gemini 文本即时输出，工具 JSON 片段聚合完成后再输出 functionCall。缺少 message_stop、未闭合块、非法工具 JSON 或上游错误不会伪造成功终态。
- 计费：复用 Claude 原始 usage 记录与 Anthropic 计费语义，客户端 usage 的输入包含普通输入及缓存读写；客户端缩放不修改结算 usage。流已开始后禁重试，保留部分 usage，Responses 发送一次 response.failed，Gemini 由统一错误出口使用 SSE 格式返回错误。
- 入口：全局/渠道透传开关不再绕过这两个跨协议转换；Claude 字节透传只适用于原生 Claude 客户端。未开始输出的转换错误返回 400。
- 验证：新增最终请求入口测试（内存 HTTP transport，无应用服务）、上游原始 request golden 对比、工具历史/namespace/custom 往返、Schema/大整数、usage/缓存/缩放、截断/异常/断连、首字 flush 和请求不变性测试。`go test ./relay/... ./dto ./service/openaicompat ./controller` 通过；service 的 Gemini 转换与计费相关专项测试通过。

支持范围不是全部供应商扩展的等价实现：Responses 的 previous_response_id / conversation / prompt 托管状态和已启用 multi_agent、Gemini cachedContent / batch 等不能在 Claude 端解析，直接返回请求错误。托管工具、供应商专属输出约束、历史 thinking 签名及其他无法映射的内容按本地 allow/safe/strict 策略记录转换损失或拒绝；没有移植上游完整 hosted-tool bridge、引用转换和插件体系。仅有 allow 策略时，诊断允许的字段/内容会被省略。

未提交、推送、部署或启动前后端应用服务；本次自动化验证不代表已通过生产真实模型端到端验证。
