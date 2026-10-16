# Gemini Responses upstream 实现对照（2026-10-15）

**当前状态：下方审计发现的兼容缺口已在后续修复中处理。** 本地采用 upstream 的工具提取、目标编码和转换损失策略，保留现有 Gemini/Vertex handler、计费与重试机制。已执行固定版本 upstream 模块生成 11 组参考结果，并由本地回归逐项比较；详见末尾“修复与验收”。此前审计内容保留为修复前快照。

## 结论与基准

此前实现确实参考了 upstream 的 Gemini 请求转换、Responses handler 和工具媒体转换，但没有完整跟进它们的共享前处理及响应转换依赖。namespace 不是当时 upstream 缺失的能力，而是本地移植遗漏。此前“本地完整 Responses 链路已完成”的说法只能说明请求、响应、SSE 和结算已连通，不能用来表示已经实现 upstream 的全部兼容行为。

本轮执行 `git fetch upstream main` 成功：

- 之前基准：`a0bf6a8a412764bc296a8e27cf8c4aaa3f3f0ed9`。
- 本次基准：`718a4e7f9751c5bf1958747cd0c2c895e2b937d4`。
- 对 `relay/channel/gemini`、`relaykit/relayconvert`、Responses HTTP 请求准备/处理和 service 转换入口做两基准间的 `git diff --name-only`，没有差异。因此下面的主要遗漏不能归因于本次 fetch 才出现的 upstream 新功能。
- 本轮完成源码审计、六条本地转换复现及文档更正；没有把本节发现的待修项目描述为已修复，也没有执行真实上游调用。

## upstream 的实际调用链

### 1. 入口与协议选择

当前 upstream 的 `POST /v1/responses` 由 host protocol registry 注册，经过 task-plugin 分发；普通文本请求回落到 `controller.Relay(...OpenAIResponses)`，再进入 `relay.ResponsesHelper`。

`PrepareResponsesRequest` 统一进行请求复制、模型映射、reasoning suffix、adaptor 初始化、协议转换和参数覆盖。HTTP 与 Responses WebSocket 复用该准备入口。

源码：

- [host protocol 路径注册](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/pkg/jsplugin/routing.go#L90)
- [普通文本 fallback](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/router/task-plugin-protocol-router.go#L26)
- [Responses 请求准备](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relay/responses_request.go)

### 2. 请求转换不是只调用一个 Gemini 转换函数

```text
Gemini Adaptor.ConvertOpenAIResponsesRequest
  -> service.ConvertRequest(target=Gemini)
  -> relayconvert.executeRequestSteps
       -> toolconv.ExtractRequest(Responses)
            tools / namespace / additional_tools / tool_choice
            parallel_tool_calls / hosted history
       -> OpenAIResponsesRequestToGeminiChat
            instructions / input / thinking / text.format / media
            function/custom call + result / trailing assistant
       -> toolconv.AttachRequest(Gemini)
            functionDeclarations / custom-as-function / googleSearch
            tool choice / capability loss diagnostics + policy
       -> RelayInfo.SetResponsesToolState
            namespace / custom 名称供响应恢复使用
```

`ExtractRequest` 会从复制后的请求中暂时移除 Tools、ToolChoice、ParallelToolCalls，把它们交给共享工具层处理。单独阅读 `to_gemini_chat_req.go` 会误以为它只支持 function 声明，因为实际工具兼容在进入该文件前和返回后分别处理。这正是此前核查漏掉的关键依赖。

源码：

- [注册执行器与前后处理](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relaykit/relayconvert/request_registry.go#L244)
- [Responses 工具提取](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relaykit/relayconvert/internal/toolconv/decode.go#L128)
- [additional_tools 提取](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relaykit/relayconvert/internal/toolconv/decode.go#L206)
- [Gemini 工具编码](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relaykit/relayconvert/internal/toolconv/encode.go#L654)
- [Gemini 请求主体转换](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relaykit/relayconvert/internal/oai_responses/to_gemini_chat_req.go)

### 3. 响应使用组合转换，流式使用公共状态机

非流式经过 `service.ConvertResponse`，先提取 hosted response，再执行 `Gemini -> Chat -> Responses` 两段转换，最后回附搜索等 hosted output。custom/namespace 的反向恢复读取请求时记录的 `ResponsesToolState`。

SSE 由 `NewResponseStreamState(Gemini, Responses)` 创建组合状态机；逐帧经过 `ConvertStreamResponseChunk`，结束时调用 `FinalizeStreamResponse`。`GeminiHostedStreamBridge` 汇总末尾才到达的 grounding 查询，输出完整搜索调用。转换失败时通过 `FailResponsesStream` 生成失败终态。

Gemini handler 仍负责累计原生 usage 和 grounding 计量。部分流失败在发送失败终态后可以返回 `(usage, nil)`，让外层继续结算。本地实现的错误返回/部分结算入口不同，不能直接只移植该返回值而忽略本地重试与退款规则。

源码：

- [Responses 组合转换注册](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relaykit/relayconvert/text_converter_registry.go#L200)
- [响应 hosted 提取和回附](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relaykit/relayconvert/response_registry.go#L483)
- [Gemini JSON 与 SSE handler](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relay/channel/gemini/relay_responses.go)
- [namespace/custom 状态](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relaykit/relayconvert/convmeta/meta.go#L94)

## 已确认的本地差异（修复前快照）

| 项目 | upstream 行为 | 本地现状与影响 |
|---|---|---|
| namespace | 共享工具层展开，响应恢复 namespace/name；`functions` 默认分组使用裸名称 | 已在上一轮报错后补上主要往返逻辑；本地另加短别名与冲突拒绝，与 upstream 冲突降级策略仍不同 |
| `input[].type=additional_tools` | 提取工具声明并并入 tools；允许其中包含 namespace | **未移植**，本地仍直接返回 unsupported input item；动态追加工具会再次失败 |
| `tool_search` 等不可映射工具 | 默认 allow：省略工具并记录转换损失；safe/strict 可拒绝 | 本地直接拒绝整个请求。upstream 的成功转发不等于 Gemini 能执行工具搜索，不能把丢弃描述为原生支持 |
| `web_search_call` 等 hosted history | 从消息中提取；Gemini 无法保留的项按 loss policy 处理，默认允许省略并记录 | 本地直接拒绝。包括本地已输出的 web_search_call 再被客户端放回 input 的场景 |
| 带版本号的 web_search 类型 | 同时识别 `web_search_2025_08_26`、`web_search_preview_2025_03_11` 及两个无版本名称 | **未移植版本别名**，本地只识别无版本名称 |
| 末尾 assistant 历史 | 在最后一个 user/工具结果后，移除不含函数调用的末尾 model turn，并记录 `trailing_assistant_omitted` | **未移植**。重试历史可能成为 Gemini assistant prefill，存在上游拒绝风险；本轮仅验证本地仍保留，未向真实端点验证 400 |
| `parallel_tool_calls=false` | 无等价字段时记录 `unsupported_parallel_tool_control`，按 policy 决定是否继续 | 本地接受后没有等价控制，也未记录该能力损失 |
| 搜索过滤/位置/预算、function strict、复杂 tool_choice | 按字段与目标能力生成诊断；部分保留，部分省略或按 policy 拒绝 | 本地对部分字段静默忽略，对部分 choice 直接拒绝；缺少统一可审计的兼容策略 |
| 媒体内容别名 | 通用解析器处理 URL 对象、`mime_type`、`url`/`data`/`file_data`/`file_url` 等形态 | 本地解析更窄，例如 input_video 只取字符串 video_url；会拒绝部分 upstream 可转换的输入 |
| 工具媒体不能承载/加载失败 | 保留可用文本/媒体，对省略内容记录诊断；Gemini 3 functionResponse 不承载音视频时会产生损失诊断 | 本地加载失败直接拒绝；音视频转为独立 user part。属于明确行为差异，不应宣称是逐项移植 |
| 普通工具结果字符串 | 非对象/数组 JSON 的字符串保持 `{"content": "..."}` | 本地会尝试解码所有 JSON 标量并放进 result，`"false"` 等文本可能变成布尔值；需评估并统一协议语义 |
| 非流式/流式媒体输出 | Gemini Chat 中间转换把 inlineData 输出为 Markdown data URL，再进入 Responses 文本 | 本地图片变为 image_generation_call，其他 inlineData MIME 报错。存在输出类型差异 |
| 未知/OTHER finishReason | Gemini Chat 转换通常映射 content_filter，再由 Responses 转为 incomplete | 本地未列举的 finishReason 作为失败处理，终态可能与 upstream 不同 |
| 统一转换诊断 | RelayInfo 收集、去重、最多 32 项；记录请求关联 warning 和消费日志 | 本地主要是即时错误/少量日志，尚未移植结构化诊断和 loss policy |

上述前三类工具前处理、末尾 assistant 与版本别名，应当作为兼容性补齐的优先项；之后统一 capability/loss policy、媒体形态与输出语义，再用相同请求/响应 fixture 对照执行。测试应覆盖共享前处理，不能只依据 Gemini adaptor 中少量基础测试判定兼容完成。

默认策略源码：[EffectiveToolLossPolicy 默认 allow](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relaykit/relayconvert/convmeta/options.go#L127)。注意 encode.go 的局部注释仍提到 default Safe，但实际默认值和 policy 测试为 allow，本审计以可执行逻辑为准。

末尾历史处理源码：[omitTrailingAssistantText](https://github.com/QuantumNous/new-api/blob/718a4e7f9751c5bf1958747cd0c2c895e2b937d4/relaykit/relayconvert/internal/oai_responses/req_helpers.go#L69)。

## 本地复现结果

使用临时目录中的独立 Go 程序调用当前 `gemini.ConvertResponsesToGemini`，仅使用合成请求，无凭据、网络生成调用和业务数据库访问。输出如下：

```text
additional_tools: ERROR unsupported Gemini Responses input item "additional_tools"
tool_search: ERROR unsupported Gemini Responses tool "tool_search"
versioned_web_search: ERROR unsupported Gemini Responses tool "web_search_2025_08_26"
hosted_history: ERROR unsupported Gemini Responses input item "web_search_call"
trailing_assistant: OK contents=2 final_role=model tool_config=<nil>
parallel_false: OK contents=1 final_role=user tool_config=<nil>
```

最后两项分别使用 `[user("hi"), assistant("prior partial answer")]`，以及一个 function 工具加 `parallel_tool_calls:false`。它们证明本地接受/保留的具体形态，不代表真实 Gemini 端点执行验证。

## 不能直接整文件覆盖的差异

- upstream 的经典 `relay/channel/vertex/adaptor.go` 中 `ConvertOpenAIResponsesRequest` 在本次基准仍为 `not implemented`；本地 Vertex Gemini 接通属于本地扩展。upstream 的 Vertex task-plugin Responses 支持不能替代这一文本 adaptor 方法。
- upstream 默认请求透传绕过转换；本地 Responses→Gemini 即使配置透传也执行协议转换，已有本地测试覆盖。这是本地设计选择。
- 本地保留自己的 BillingSession、流已开始禁止重试、客户端断连错误分类、部分失败结算与 usage 倍率。应保留这些约束，按 upstream 行为补齐转换层，而不是覆盖整个 handler。
- upstream 的 WebSocket、task-plugin Responses 检索/后台任务入口属于更广的接口架构；不能把这些能力算作本次 Gemini HTTP 文本转换已经实现。

当前准确状态：**Gemini/Vertex Gemini HTTP Responses 基础链路及本地结算已打通，namespace 已修复；与 upstream 的兼容性对齐仍未完成。**

以上是审计时的结论；以下后续修复记录更新该状态。

## 修复与验收（2026-10-15）

### 实现

- 新增 `responses_prepare.go`，在正文转换前统一提取 tools、namespace、input.additional_tools、hosted history、parallel_tool_calls 和 tool_choice，再编码成 Gemini 工具。由 Gemini 和 Vertex Gemini 共用，不依赖 upstream relaykit 的整仓迁移。
- additional_tools 声明并入工具列表；保留名称映射和 custom-as-function 恢复，函数/自定义工具冲突按策略省略并诊断。带版本号的两种 web_search 名称已支持；不可映射的工具/托管历史/不透明输入项默认省略并记录，避免遇到新工具名或历史类型就直接拒绝整个请求。
- 移植 `types/conversion.go` 的 allow/safe/strict 语义，新增渠道 `other_settings.tool_loss_policy`。未设置或 `allow` 允许有损转换；`safe` 拒绝影响执行语义的损失；`strict` 同时拒绝表示形式损失。未知 policy 明确报错。此字段作用于本地 Gemini/Vertex Responses 请求转换；响应阶段始终保留已生成内容，不因该策略追溯拒绝。
- `RelayInfo` 按渠道尝试收集、去重、最多保留 32 项诊断；诊断日志保留 request ID，消费日志保存到 `other.admin_info.conversion_diagnostics` 和截断标记。重试会清空旧状态；即使诊断列表已满，后续语义损失仍能触发 safe 拒绝。
- 补齐末尾 assistant 清理、搜索控制/strict/parallel/复杂 choice 的能力诊断。末尾函数调用和没有此前 user 的独立 assistant 历史保留。原有 `tool_choice:none` 同时禁用函数及搜索工具的行为保留。
- 新增 `responses_media.go`，统一解析媒体 URL 对象及 mime_type/data/url/file_data/file_url 别名。普通工具字符串仅尝试解析对象/数组，`"false"`、`"0"` 等仍以 content 文本传递。工具输出媒体按模型能力承载；无法加载或表达的媒体省略并诊断，同时保留可用文本和媒体；safe/strict 会在请求发出前拒绝相应损失。
- Gemini inlineData 输出改用 upstream 的 Markdown data URL 文本表示，覆盖图片和其他媒体。未知/OTHER finishReason 转为 `response.incomplete` / content_filter。坏帧、读错误、超时和不完整 partialArgs 仍作为流失败；原生 usage、倍率、单次结算和断连分类继续走本地安全路径。
- 退款测试改为等待异步 BillingSession.Refund 完成用户与令牌两处更新，避免测试清理数据库时仍有退款任务未完成。

不提供远端 Responses 状态解析：previous_response_id/conversation/prompt/context_management 仍拒绝。item_reference、compaction 等不透明 input 项不能恢复远端内容；默认策略可在保留其他可见历史时省略并诊断，只有不透明项而没有可用正文时仍拒绝空请求。tool_search 的省略不等于 Gemini 原生支持工具搜索。

### upstream 执行对照

在临时目录用 `git archive` 提取未修改的 `718a4e7f97` 的 relaykit 模块，实际调用其 `relayconvert.ConvertRequest` 生成参考输出；没有调用生成 API。输入、输出及生成器保存在：

- `relay/channel/gemini/testdata/responses_upstream_cases.json`
- `relay/channel/gemini/testdata/responses_upstream_expected.json`
- `relay/channel/gemini/testdata/upstream_reference_gen.go.txt`
- 生成说明：`relay/channel/gemini/testdata/README.md`

`TestResponsesUpstreamReferenceFixtures` 对同样的 11 组输入比较消息内容、函数名、托管工具、tool_config、拒绝结果与诊断代码，全部通过。该比较不覆盖随机 ID、主机安全设置、Schema 字段编码形式，也不应解读为所有 Responses 扩展与上游逐字节相等。

### 验证结果

- `go test ./relay/channel/gemini ./relay/channel/vertex ./relay/helper ./relay/common ./dto ./types ./service ./relay ./controller -count=1`：9 个包全套通过。
- 11 组 upstream 执行参考 fixture：全部通过。
- Gemini/Vertex JSON/SSE + allow/safe/strict 本地 HTTP/临时 SQLite 测试：通过；默认转换后的工具名能恢复，账单原始 token 数与余额一致，仅一次调用及结算；safe/strict 拒绝时不发起上游请求，用户与令牌预扣费全退。
- 媒体、未知终态、末尾历史、标量文本、工具冲突、诊断截断与重试隔离：回归通过。
- `go test ./... -run '^$'`：全仓编译通过；gofmt 和 `git diff --check` 通过。
- 真实 Gemini/Vertex 请求：未执行；这是本地兼容与计费验收，不代替真实端点联调。

当前范围为本地 Gemini/Vertex HTTP Responses 兼容。upstream 的 WebSocket、task-plugin 后台任务/检索架构仍不在这一移植批次内；本地强制协议转换、长工具名短别名及计费保护继续保留。
