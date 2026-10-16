# Gemini 中转协议 upstream 同步记录（2026-10-14）

## 需求与范围

需求来自本次对话：检查 Git upstream 的最新代码，梳理 Gemini 中转协议相关更新，制定同步方案，并按批次 1–4 将选定更新移植到当前工作区。同步重点为请求/响应转换、流式工具调用、usage 归并、grounding 搜索计量及相关管理界面；本轮不执行整分支合并或 rebase。

## upstream 基准与差异

本记录编写前已运行 `git fetch upstream main`，获取成功。当前观察值：

| 项目 | 值 |
|---|---|
| 本地分支 | `main` |
| 本地 `HEAD` | `7301bad10b1ac85b39f0e0d62d830b756233b49d`（2026-10-13） |
| 本地 fetch 前的 `upstream/main` | `7de10f2c602b50122017556e8a09cb7c488c273c` |
| fetch 后的 `upstream/main` | `a0bf6a8a412764bc296a8e27cf8c4aaa3f3f0ed9`（2026-10-14） |
| merge base | `8ed2ea6ec11dd1da8da172ddd71f61bebef01b88` |
| `HEAD...upstream/main` 计数 | 本地独有 210 个提交；upstream 独有 1141 个提交 |

分支提交差异较大，因此本次采用按功能挑选和移植的方式，没有把 upstream 整体合并进来。fetch 之后 upstream 新增两条提交：

- `a0bf6a8a41`：Vertex API Key 模式支持配置 `project_id`，修复官方端点 URL 缺少项目路径的问题。该提交修改了本地也有改动的 `relay/channel/vertex/adaptor.go`，属于需后续人工整合的同文件变更。
- `4013729f6a`：敏感词过滤默认值调整，与本次 Gemini 同步范围无关。

本次检查到的 Gemini/relay 相关 upstream 提交包括：

| 提交 | upstream 变化 |
|---|---|
| `78bd5b1cbf`、`5810573162` | Gemini 流式 usage 细节、Responses 工具输出媒体转换；累计流式 usage 按快照归并，避免重复累加 |
| `0a03088e0e` | 修复协议转换信息丢失及转换流失败时的计费处理 |
| `acb347a197`、`d10f11ffff` | 搜索工具编码及 Gemini grounding 按查询数或 grounded prompt 计费 |
| `4b13d9388e`、`c10780e054` | 过滤非字符串 enum；在可用 Gemini 端点保留 function tools 与 structured output 的 JSON Schema 约束 |
| `036ba376da` | Gemini 2.5 thinking budget 与模型/effort 相匹配 |
| `a0bf6a8a41` | Vertex API Key 项目的 URL 路径修复；这是本次 fetch 后额外发现的 Vertex 同步项 |

## 批次 1–4 执行记录

### 批次 1–2：DTO、请求转换与 usage 基础

- 扩展 Gemini DTO，承载 Gemini 工具、thinking、grounding、usage 和 Vertex 调用所需字段。
- 保留可选标量的显式零值语义；区分没有 `usageMetadata` 与存在但数值为零的 usage 快照。
- 增加 Gemini usage 快照合并逻辑，保留模态 token 详情，避免对累计快照重复相加；缺少真实计量时保留估算计费数据。
- 补充 Vertex/Gemini 工具调用 ID 传递和 Responses usage 回填。

### 批次 3：协议转换、thinking、工具流与 grounding 引用

- 规范化 thinking level/budget，并按 API 版本及 Vertex 能力决定是否保留完整 JSON Schema；对旧端点记录降级诊断，清理 enum 中不支持的值。
- 往返转换 function call/result 时保留 ID。
- 增加跨流帧 `partialArgs` 重建；不完整调用在正常结束时作为错误处理，并限制 JSONPath 数组索引范围。
- 将 Gemini grounding 元数据转换为 OpenAI URL annotations，覆盖非流式与流式响应。

### 批次 4：Grounding 计量、策略与 UI

- 对流式帧与多个候选里的搜索词去重，并记录 query 和 grounded prompt 两种原始计数。
- 按模型选择一种计费单位：Gemini 3 按去重查询数；旧版按 grounded prompt；两者不叠加。查询数有上限。
- 接入默认价及活动 billing policy 的工具价格，日志记录计费单位/数量/单价，并增加管理界面标签和 en/fr/ja/ru/vi/zh-CN/zh-TW 文案。

主要实现与测试文件：`dto/gemini.go`、`dto/gemini_usage_merge.go`、`relay/channel/gemini/relay-gemini.go`、`relay/channel/gemini/schema_sync.go`、`relay/channel/gemini/thinking_sync.go`、`relay/channel/gemini/partial_tool_calls.go`、`relay/channel/gemini/grounding_annotations.go`、`relay/channel/gemini/grounding_usage.go`、`service/text_quota.go`、`service/gemini_search_billing_test.go` 及相关 UI/locale 文件。

## 测试与检查结果

| 检查 | 结果 | 说明 |
|---|---|---|
| `go test ./... -run '^$'` | 通过 | 全仓编译检查；没有运行测试用例 |
| Gemini、Vertex、helper、common 定向包测试 | 通过 | `relay/channel/gemini`、`relay/channel/vertex`、`relay/helper`、`relay/common` 均通过 |
| Gemini 专项 service 测试 | 通过 | usage 合并、native usage、Schema/call ID 转换、query/grounded-prompt 选价、策略单价覆盖和计数边界均通过 |
| `go test ./service -count=1 -skip 'TestLocalSQLiteModelPricingEndToEnd'` | 通过 | 排除依赖本机 SQLite 定价数据的端到端测试后，service 测试通过 |
| service 全量测试 | 未全通过 | `TestLocalSQLiteModelPricingEndToEnd/openai_native_usage_and_settlement` 在 `service/local_sqlite_pricing_e2e_test.go:168` 失败：本机策略计算结果为 `short`，测试期望 `长报文`。Gemini native usage 子用例通过；未修改本机数据库。 |
| `bun run build` | 通过 | 前端生产构建成功；构建器提示 caniuse-lite 数据较旧 |
| `bun run i18n:lint` | 未通过 | 未修改的 `web/src/components/table/channels/modals/ServiceTierBillingEditor.jsx` 第 90、99 行有两条硬编码文本：`gpt-6-astra / *`、`Ultrafast` |
| gofmt（本轮涉及的 36 个 Go 文件） | 通过 | `gofmt -d` 无输出 |
| `git diff --check` | 通过 | 无空白错误 |
| 真实 Gemini/Vertex 上游请求 | 未执行 | 当前结论基于本地单元/包测试和编译，不代表对真实上游端点完成联调 |

## 5 项遗留问题处理记录（2026-10-14 后续修复）

上方测试表保留批次 1–4 初次交付时的结果；以下记录后续修复和复验，不能将历史失败当作当前结论。

### 1. SQLite 定价测试：已修复

只读检查发现本机 `gpt-5.6-sol` 策略使用 `short`（fallback）和 `long`（`input_total_tokens >= 272000`），所以 200 token 命中 `short` 是正确结果。失败源于测试依赖可变的本机数据库并硬编码另一个策略的 tier ID，不是 Gemini 结算算错。

`service/local_sqlite_pricing_e2e_test.go` 改为从版本管理中的 `service/testdata/sqlite_pricing_options.json` 建立 `t.TempDir()` 下的临时 SQLite，再以只读连接加载配置；连接在测试后关闭，不再隐式访问 `one-api.db`。保留完整定价字段与三种协议 usage 往返断言，并补充 fixture 在 199/200/201 token 的分层边界验证。测试 fixture 的 200 阈值仅供测试，不是生产价格建议。

复验：`go test ./service -count=1` 全部通过，包含原失败用例，未跳过 SQLite 测试，也未修改本机数据库。

### 2. i18n lint：已修复

将两个 placeholder 改为 `t('例如：{{example}}', { example: ... })`，保留模型/档位示例字面值，通过翻译模板显示。补充所有 7 个 locale 文件的示例模板及 Vertex 项目 ID 文案。复验 `bun run i18n:lint`：No issues found。

### 3. Vertex project_id：已移植并测试

- `dto.ChannelOtherSettings.VertexProjectID` 保存可选项目 ID；API Key 模式在 URL 中使用 trim 后的值，空值保留 Express 行为；服务账号模式仍读取服务账号自身的项目。
- 本地经典 `EditChannelModal` 增加可选项目 ID 输入，通过 `vertexChannelSettings.js` 统一加载/保存。切换非 Vertex 或 JSON 模式后保存会清理残留字段；清空输入会删除已保存的项目 ID；其他 settings 保留。
- URL 回归覆盖 global、区域流式、Express、空白项目、Claude 流式、自定义网关与服务账号优先级，保留批次 1–2 的工具 ID 处理。
- `go test ./relay/channel/vertex -count=1` 通过；`bun test src/helpers/vertexChannelSettings.test.js` 为 3 pass、0 fail。

### 4. 真实上游联调：仍受配置缺失阻塞，未宣称完成

检查到当前进程没有 Gemini/Vertex 联调环境变量，本机数据库没有类型 24/41 的 Gemini/Vertex 渠道；没有可用凭据、模型和项目/区域。已询问配置文件路径或环境变量名。没有发送未经配置的真实生成请求，也没有把本地测试当成真实联调通过。

新增 `relay/channel/vertex/gemini_sync_live_test.go` 作为显式启用的真实请求验证入口，使用合成提示词，经本地 OpenAI→Gemini 转换和 Gemini→OpenAI 响应处理，覆盖完整 Schema、thinking、partialArgs 工具流、grounding 引用和搜索计数，以及 Vertex adaptor URL。每个场景最多 1024 输出 token、90 秒超时；只访问官方端点，拒绝重定向，不输出密钥、请求 URL 或上游错误响应体。上游未产生 partialArgs 或 grounding 引用时，相应用例会失败，不能计为覆盖成功。

运行方式（先在本机安全配置环境变量，不将密钥写入仓库）：

```powershell
# Gemini: GEMINI_SYNC_API_KEY、GEMINI_SYNC_MODEL
# Vertex: VERTEX_SYNC_API_KEY、VERTEX_SYNC_MODEL、VERTEX_SYNC_REGION
# 标准 Vertex 项目端点另设 VERTEX_SYNC_PROJECT_ID；Express 可留空
$env:GEMINI_SYNC_LIVE = '1'
go test ./relay/channel/vertex -run '^TestGeminiSyncLive/gemini$' -v -count=1
go test ./relay/channel/vertex -run '^TestGeminiSyncLive/vertex$' -v -count=1
Remove-Item Env:GEMINI_SYNC_LIVE
```

默认未启用时明确 SKIP。真实 v1 Schema 降级仍需选取支持 v1 的模型单独联调；本地降级回归已通过。

### 5. 分支差异：完成本轮筛选和同文件整合，不代表整仓同步完成

以 `7301bad10b` / `a0bf6a8a41` 为固定比较基准，对 Gemini/Vertex adaptor、Gemini DTO、shared/gemini 与 gemini_chat 转换目录的差异进行筛选，并按提交主题补查 thinking 和路由。Git 的 210/1141 是祖先差异，不是缺失功能数量；手工移植后不应以该数字归零作为验收条件。

| upstream 项目 | 处置与结果 |
|---|---|
| `a0bf6a8a41` 项目 ID | 已适配到本地 DTO、Vertex URL 和经典 UI，不覆盖已有 ID/网关逻辑 |
| `c10780e054`、`4b13d9388e` Schema | 已移植；核查发现空对象带 minProperties/maxProperties/description 时约束会被遗漏，本轮补齐并增加回归 |
| `036ba376da`、`fa90b2312c` thinking | 沿用本地 normalizer 与 budget/level 回归，保留本地映射行为 |
| `78bd5b1cbf`、`5810573162` usage | 累计快照合并部分已移植；不能据此声称整个 Responses 媒体链路已支持 |
| `d10f11ffff`、`acb347a197` grounding | Gemini 搜索去重/计量/互斥收费已移植，活动策略覆盖价和次数上限通过测试 |
| `81336fc69b` countTokens | 核查发现本地遗漏，本轮补到 `/v1`、`/v1beta`，在鉴权/分发前返回 404；路由回归通过，generateContent 仍进入鉴权 |
| `987b7ecd22` 自定义 Vertex 网关 | 本地已有 URL builder；本轮增加“网关 + 项目 ID”的组合回归 |
| `1a4166d8e8` Responses custom tools、`78bd5b1cbf` Responses 工具媒体 | 尚未移植完整链路：本地 Gemini `ConvertOpenAIResponsesRequest` 仍为 not implemented，不能宣称这些能力已同步 |
| `86ac0f7745` relaykit、`bb28f2ccad` tokenkit、通用 TLS/i18n/指标与其他厂商改造 | 整仓架构迁移不在本次 Gemini 协议批次范围；保留本地 dto/service/adaptor、计费策略和 usage 倍率机制 |

本轮实际处理的同文件差异没有待解决 Git 冲突。仍未做的是整仓合并及完整 Responses 转换批次；后者需要同时实现请求、非流式响应、SSE 状态机和失败结算，不能只接通请求方法。这些限制与第 4 项真实联调缺口均保留在记录中。

## 后续修复最终复验

| 检查 | 结果 |
|---|---|
| service 全套（含固定 SQLite fixture 和分层边界） | 通过，不再跳过本地定价测试 |
| Gemini、Vertex、router 包测试 | 通过，包括新增空对象 Schema、项目 ID URL 和 countTokens 防护 |
| DTO、relay/helper、relay/common 包测试 | 通过 |
| 全仓 `go test ./... -run '^$'` | 通过，仅编译检查，不代表全仓测试全跑 |
| Vertex 前端配置 helper 测试 | 3 pass，0 fail |
| i18n lint | 通过，No issues found |
| 前端 `bun run build` | 通过 |
| gofmt、`git diff --check` | 通过 |
| `TestGeminiSyncLive` | SKIP：未设置 `GEMINI_SYNC_LIVE=1`，且缺少真实联调凭据；不计为真实上游通过 |

测试日志保存在当前机器临时目录的 `gemini-followup-go.log`、`gemini-followup-relay.log`、`gemini-followup-compile.log`、`gemini-followup-web.log`、`gemini-followup-vertex.log`，它们不是仓库交付文件。真实上游验证与完整 Responses 转换链路仍未完成。

## 工作区状态

本记录对应代码改动仍未提交。批次 1–4 改动保留；后续修复已手工移植 Vertex 项目 ID、空对象 Schema 约束及 countTokens 防护，没有整分支合并或 rebase。未修改本机 SQLite 数据库。真实上游联调仍等待本机安全配置凭据。

## Gemini 本地 Responses 完整转换链路（2026-10-14 后续实现）

本节更新前文“完整 Responses 转换链路尚未完成”的状态：**本地请求、非流式响应、SSE 状态机及失败结算已实现，并通过本地测试**。保留当前 dto/service/adaptor 架构，没有引入 upstream 的 relaykit/tokenkit 整仓迁移。真实 Gemini/Vertex 端点联调仍未执行，不能把模拟上游测试视为真实端点验收。

### 实现范围

- Gemini 和 Vertex Gemini adaptor 接通 `ConvertOpenAIResponsesRequest`，按 Responses relay mode 分发到专用响应处理器。即使启用全局/渠道请求透传，Responses 请求也先转换为 generateContent 的请求体，避免向 Gemini 直接发送 Responses JSON。
- 请求转换支持字符串/消息历史、instructions/system/developer、function call/output、custom tool call/output、图片/文档/音频/视频输入、web_search、tool_choice、JSON Schema 和 reasoning。保留 temperature/top_p/max_output_tokens 的显式零值；支持 JSON Schema 的端点保留 additionalProperties 等约束，旧 v1 沿用已有降级规则。`tool_choice: none` 同时禁用函数和搜索工具。
- Custom tools 转换为接收单个 `input` 字符串参数的函数，返回时恢复为 `custom_tool_call`；grammar 定义作为工具说明传递，并非 Gemini 原生语法约束保证。
- 工具输出内容数组中的媒体不再作为 JSON/base64 文本交给 tokenizer。Gemini 3 的图片/文档进入 functionResponse.parts；Gemini 1/2 的媒体以及音频/视频作为同一 user turn 的独立媒体 part。普通 JSON 数据数组仍保留为工具结果，加载失败或不支持的媒体明确报错。
- 非流式输出包含独立的 message、reasoning summary、function/custom call、图片生成结果及搜索调用，保留调用 ID、grounding 引用和 Gemini 原生计费用量。Vertex 继续遵守现有去除 function ID 的渠道设置。
- SSE 使用稳定 item ID/output_index 和递增 sequence_number，产生 created/in_progress、item/part added、文本/思考/工具参数 delta/done、引用、item done 以及唯一终态。partialArgs 先按 JSONPath 重建，调用完成后输出完整参数 delta；包含 args 快照的 willContinue 帧也保留参数。长度/安全截断返回 `response.incomplete`，不误报 completed。
- 继续读取 finishReason 后的 usage/grounding 帧；累计 usage 按快照归并，客户端 usage 倍率仅在输出边界应用，账单使用原始 Gemini usage。补齐 Responses output_tokens_details 的倍率处理。
- 坏帧、上游错误、读取失败、超时、缺失 finishReason 或未完成工具调用均不产生成功终态。流已开始时返回部分真实/估算 usage、禁止重试，通过现有 BillingSession 结算一次；异常终态包含已发送的部分输出。客户端断连也结算已有用量，并记录取消标记。首个事件之前的错误保留 JSON 错误响应；请求校验失败返回 400，退还预扣费。

### 明确边界

这是无状态的本地协议转换：调用方须提交完整可见历史；`previous_response_id`、conversation、托管 prompt、context_management、item_reference 和供应商 file_id 返回 400。不提供 Responses 存储/检索或 `/responses/compact`。跨供应商的加密 reasoning 不会作为普通消息转发；Gemini 函数签名沿用已有 thought-signature 兼容设置。内置工具当前映射 web_search/web_search_preview，其他未支持工具明确拒绝。Responses 输出支持文本、工具和图片；不支持的 Gemini 音频或 fileData 输出会明确失败。

### 验证

| 检查 | 结果 |
|---|---|
| `go test ./relay/channel/gemini ./relay/channel/vertex ./relay/helper ./relay/common ./dto ./service ./relay ./controller -count=1` | 通过；包括 service 全套计费测试 |
| Gemini/Vertex/controller 后续定向及整包复验 | 通过；覆盖新增 reasoning、工具快照、取消日志等修正 |
| `go test ./... -run '^$'` | 通过；全仓编译检查 |
| 本地 HTTP + 临时 SQLite 的 controller 端到端测试 | Gemini/Vertex 的成功流、部分失败、客户端取消、全局/渠道透传均只调用一次上游、只产生一次消费日志，用户与令牌余额一致；非法请求和首帧前阻断退还预扣费 |
| SSE/转换回归 | 覆盖事件顺序、稳定索引、custom tools、工具媒体、Unicode 引用、尾帧 usage、累计快照、倍率、截断、坏帧、读错误、超时、断连和缺失用量估算 |
| gofmt、`git diff --check` | 通过 |
| 真实 Gemini/Vertex Responses 联调 | 未执行；原有 live test 也不代表新增 Responses 链路的真实端点覆盖 |

新增实现主要位于 `relay/channel/gemini/responses_request.go`、`responses_response.go`、`relay_responses.go`；回归位于 `relay/channel/gemini/responses_test.go`、`controller/relay_gemini_responses_test.go` 及现有 Vertex/helper 测试文件。测试使用临时数据库和本地 HTTP 服务，未修改本机业务 SQLite。工作区原有改动继续保留，本次没有提交或合并分支。

### Namespace 工具兼容修复（2026-10-15）

本地调试报告 `unsupported Gemini Responses tool "namespace"`。原因是首版请求转换只枚举顶层 function/custom/web_search，未展开 Responses namespace 容器；这是本地转换遗漏，请求在发往 Gemini 前即被拒绝。

新增 `responses_tools.go` 展开 namespace 内的 function/custom 声明，保留分组说明和参数 Schema，并维护请求级名称映射。非默认 namespace 使用 `namespace__name` 的 Gemini 函数名，过长或包含不适用字符时使用稳定短别名；返回的 function/custom call 恢复独立的 `namespace` 与原始 `name`。Codex 默认 `functions` namespace 沿用 upstream 的裸名称约定。历史调用、工具输出和指定 tool_choice 同步映射；名称冲突明确拒绝，避免工具被错误分发。非流式和 SSE 的 added/done/最终 output 均应用反向映射。

结构依据仓库 `CODEX-INNER-TOOLS-CN.md` 的 Namespace/MCP 工具说明及已获取的 upstream 转换实现；本次官方文档访问受站点拦截，未据此声称完成在线文档核验。新增回归覆盖多 namespace 同名工具、custom tools、默认 namespace、历史/结果、tool_choice、长别名、冲突及非法声明，并为 Gemini/Vertex 本地 HTTP 端到端结算测试增加 namespace 请求场景。真实上游联调仍未执行。

### upstream 全链路复核与状态更正（2026-10-15）

用户要求重新核查 upstream 后，执行 fetch，基准更新为 `718a4e7f9751c5bf1958747cd0c2c895e2b937d4`。沿 router、请求 registry、toolconv、Gemini 请求主体、响应组合转换和 SSE/结算完整检查，确认此前参考范围不完整，漏掉了共享工具前处理和默认转换损失策略。这些逻辑在上一基准 `a0bf6a8a41` 已存在。

六条本地合成请求复现确认：additional_tools、tool_search、带版本号的 web_search 和 hosted history 仍被拒绝；末尾 assistant 仍保留；parallel_tool_calls=false 没有等价控制或转换损失记录。详细源码链接、调用链、行为差异和优先级见 [Gemini Responses upstream 实现对照](gemini_responses_upstream_audit_261015.md)。

因此更正前文“完整链路完成”的含义：请求/响应/SSE/结算已连通，且已有测试覆盖，但**尚未完成与 upstream 的兼容性对齐**。本节是复核记录，上述新增发现并未在本轮宣称修复。

### 按 upstream 思路补齐（2026-10-15，后续实现）

复核后的实现已补齐工具提取和附加、additional_tools、版本化搜索、默认 allow 与可选 safe/strict 损失策略、托管/不透明历史处理、末尾 assistant 清理、媒体别名/工具结果语义、媒体输出和 finishReason 映射。诊断进入请求关联日志及消费日志 admin_info，32 项上限不会绕过 safe/strict 检查，重试状态隔离。

已实际执行固定版本 upstream relaykit，对 11 组相同输入生成参考结果；本地消息、工具、choice、策略结果和诊断代码匹配。相关 9 包全套测试与 Gemini/Vertex JSON/SSE、单次结算、拒绝退款回归通过。完整实现、参考 fixture、验证及明确边界见 [复核报告的修复与验收](gemini_responses_upstream_audit_261015.md#修复与验收2026-10-15)。此前“尚未修复”条目保留为历史快照，以本节与报告末尾状态为准。真实上游联调仍未执行。
