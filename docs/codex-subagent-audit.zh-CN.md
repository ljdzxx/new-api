# Codex 子代理与 Responses 转发审计

检查日期：2026-11-14。项目：new-api。

## 本地修复状态（2026-11-14）

本节描述当前工作树的修复结果。后续「原始审计记录」与 upstream 复核保留历史结论，不代表修复后的状态。此次在当前分支就地修复，没有合并 upstream/main。

### 已实施

| 问题 | 修复与边界 |
| --- | --- |
| hosted 配置丢失 | OpenAIResponsesRequest.MultiAgent 使用 json.RawMessage，保留显式 false/0 和未知嵌套配置。原生 input/tools 中的协作内容继续保持不透明转发。 |
| Beta/子代理请求头缺失 | Responses 及 compact 自动转发白名单内的 Beta、session/thread、parent/subagent 元数据；渠道覆盖仍可生效。body passthrough 分支也执行请求头覆盖操作，不套用请求体修改。客户端 Authorization 不作为元数据转发。 |
| 数值精度与 schema 顺序 | 禁用字段过滤及 item ID 清理改为按原始 JSON 路径删除；测试覆盖 9007199254740993、属性次序与重复字段。未宣称任意自定义参数覆盖操作都无损。 |
| sanitizer 误删 | encrypted_content 清理仅作用于协议 reasoning/compaction 项及 message 直接内容项，不递归清理工具结果或 agent_message。未知/托管项的 ID 保留。 |
| Responses WebSocket | 新增 GET /v1/responses，原生 OpenAI/Codex 渠道可选择启用；支持 response.create、response.inject、response.cancel。每个 create 重新执行鉴权、限流、渠道/凭据检查和现有计费链；同一连接只允许一个活动 create。 |
| 跨协议工具数据损失 | 修复 Responses→Chat 辅助转换中的 namespace/custom 文本工具、结构化结果、strict:false 及反向映射。Gemini/Chat 明确拒绝无法表达的 hosted agent tree 及带协议 agent 归属的历史。Chat 辅助函数未新增为本分支的生产 Responses→Chat adaptor。custom grammar 不做有损仿真。 |
| 续轮路由/密钥不稳定 | 原生 response ID 按用户和 token 绑定渠道、密钥指纹、模型/分组、endpoint 及有效鉴权头；HTTP previous_response_id 和同一 WS 连接保持绑定。无绑定、权限变化、密钥失效或路由变化时明确报错，不重试到其他账号。 |

WebSocket 终止事件在 Relay 结算完成后发送；忽略重复终止事件，继续回传迟到的注入回执。上游断线完成结算/退款后关闭客户端，不能误标为用户取消或再次消费。无上游 usage 时沿用现有本地 token 估算策略，不新增臆测收费规则。

/v1/responses/compact 不支持 hosted agent tree：显式 multi_agent 配置（包括 enabled:false）或顶层托管协作历史项返回 400，避免丢字段后假成功；普通客户端工具历史仍可压缩。

### 启用方式与部署影响

1. 在 OpenAI/Codex 渠道编辑页打开「启用 Responses WebSocket」，对应 setting 中 responses_websocket_enabled: true，默认关闭。上游也必须实现对应协议；开关不会赋予远端账号新能力。
2. POST Responses 子代理工具调用继续使用普通渠道。创建、分派、后续任务由 Codex 运行时协作工具执行，网关承接模型请求；没有新增独立 /agents/create 执行器。
3. response ID 绑定有效期为 24 小时，按用户/API token 隔离。多实例需要共享 Redis；无 Redis 时使用有界内存缓存，重启、淘汰或请求落到另一实例后可能无法续轮。
4. 升级前的 ID、过期 ID、不同 token、凭据替换/刷新或路由变更返回绑定错误（通常为 409）。客户端应移除 previous_response_id 并重放完整历史；同一 WS 凭据已变化时需重新连接。
5. 未提交或部署改动，原有 deploy/README.md 和 deploy/deploy.sh 用户修改保留。未调用付费上游验证远端账号能力。

### 回归验证

- HTTP：OpenAI/Codex × JSON/SSE × 普通/渠道 passthrough/全局 passthrough，12 个组合共 60 次请求，检查默认 Beta/子线程头、hosted 配置、工具调用/结果续轮与独立结算。
- WS：本地 mock 上游与真实 TokenAuth/Distribute/Relay；创建→工具调用→注入→回执→完成→同连接续轮，检查两笔消费、一次握手，撤销 token 后拒绝下次 create。
- WS 边界：完成后立即断线、失败后断线、无终止事件的断线（含有/无 usage）、取消、并发 create、重复终止事件、完成后的注入回执；检查单次结算、用户/token 余额一致、上游断线未标成客户端取消。
- HTTP 续轮：轮询密钥保持绑定，其他 token/已变更密钥拒绝。Redis 单测覆盖 owner 隔离、过期、凭据无明文存储及有效鉴权头变化；密钥单测覆盖 disabled/changed。
- 验证命令与结果：

    go test ./controller ./relay ./relay/common ./relay/helper ./relay/channel/openai ./relay/channel/codex ./relay/channel/gemini ./relay/channel ./service/openaicompat ./dto ./middleware ./setting/operation_setting -count=1 -timeout=180s

  首轮受影响包完整测试通过（11 个包有测试，Codex adaptor 无独立测试文件）。补充边界后 dto/relay/controller、relay/helper 完整包测试均复核通过；WS 与 HTTP 续轮定向 race 测试通过两轮。最后补充 agent 归属拒绝校验后，重新执行同一命令并加入 ./router，13 个包全部退出码 0（12 个包有测试，Codex adaptor 无独立测试文件）。

    go test -race ./controller -run '^TestResponses(WebSocket|HTTPContinuation)' -count=1 -timeout=180s

  服务绑定单测使用 go test -c 生成独立测试程序再执行，两个用例均通过且退出码 0（避免 Windows 临时 test.exe 清理权限问题）。go test ./router -run '^$' 路由编译通过；根模块 go build 通过；web 下 bun run build 通过（原有 Browserslist 数据过期提示）。git diff --check 通过。

  所有网络上游均为本地 mock。没有执行全仓库 go test ./...，没有实测付费远端服务。

## 原始审计记录（修复前）

## 结论

客户端执行的 Codex 子代理工具，可以经过现有 `/v1/responses` HTTP/JSON/SSE 链路工作；但当前实现不能称为对所有多代理协议的完整、无损透传。客户端启用 `multi_agent`，与向上游 Responses 请求发送顶层 `multi_agent` 配置，是不同的能力。

创建和管理本地代理由 Codex 运行时完成。new-api 不必为普通客户端子代理另建 `/agents/create` 路由；其职责是转发工具声明、调用、工具结果，以及子线程发起的普通模型请求。如果要提供脱离 Codex 客户端的服务器任务调度接口，那是另一个包含线程状态、执行器与权限边界的功能，本次检查未实现该功能。

## 当前会话实测

通过实际协作工具创建 `/root/codex_child_probe`，完成以下闭环：

1. `collaboration.spawn_agent` 创建并分派只读代码审计。
2. 子代理使用 `collaboration.send_message` 返回 `probe_created_ok`，随后返回完整审计结果。
3. `collaboration.followup_task` 给已完成的同一子代理下发第二轮任务。
4. 子代理返回 `probe_followup_ok`，能回忆第一轮检查的文件路径，并完成第二轮工具请求。
5. `collaboration.list_agents` 确认子代理完成及其第二轮结果。

工具目录还提供 `wait_agent`、`interrupt_agent`，本次没有实际调用这两项。没有 `close_agent`，不能照搬其他客户端版本的工具名。另有 `mcp__codex_tui` 的线程创建、读取、发消息和等待工具；这些不等于本项目的 HTTP 接口，本次未另建 TUI 任务。

这证明当前运行时的协作工具可用；不能单凭当前会话推断线上部署一定经过本地检出的 new-api。

## 项目链路验证

新增 `controller/relay_codex_subagent_test.go`，从 `controller.Relay` 进入真实请求校验、Responses 转发、上游 HTTP、返回处理和结算，使用本地 `httptest` 上游与隔离的 SQLite 测试数据。

- OpenAI 与 Codex OAuth 两类 adaptor。
- JSON 与 SSE 两种返回方式。
- 普通、渠道 body passthrough、全局 body passthrough 三种模式。
- 每组合依次验证创建调用、子线程请求、父线程后续分派、子线程工具结果续轮、后续分派结果回传；共 12 个组合、60 次请求。
- 检查 `namespace`、function/custom 声明、`call_id`、自定义工具原文、多模态形状的工具结果、`additional_tools`、显式 `false`、`client_metadata`、`prompt_cache_key`、代理归属字段。
- 检查配置显式 Header Override 后的 session/parent-thread/subagent 请求头、正确上游路径、上游鉴权替换及每请求独立计费。
- OpenAI 组合还检查 `store` 与 `previous_response_id` 转发；不表示远端账号一定支持存储。

新增 `relay/channel/gemini/responses_subagent_test.go`，覆盖 Gemini/Vertex、JSON/SSE 四个组合，确认 `collaboration.spawn_agent` 历史与结果关联，以及返回的 `followup_task` namespace 和 call ID 恢复。

这些是协议及本地链路测试，不会实际访问付费上游，也不证明远端模型能正确选择工具。Controller 测试直接准备认证和渠道上下文，没有覆盖线上反向代理、鉴权中间件、渠道调度或多密钥池。

## 确认的缺口

### 1. 顶层 hosted multi_agent 配置丢失

`dto/openai_request.go` 的 `OpenAIResponsesRequest` 没有 `multi_agent` 字段或顶层扩展字段容器。非 body-passthrough 路径将 DTO 再序列化，导致该配置消失。临时探针已确认：

```text
multi_agent preserved after DTO: false
```

原生 `input`、`tools`、`tool_choice` 是 `json.RawMessage`，普通 function/custom 协作内容本身不需要增加专门的 spawn DTO。Hosted multi-agent 则需要保留其配置、`multi_agent_call`、`multi_agent_call_output`、`agent_message` 及代理归属。普通客户端工具与托管协作动作不能混为一谈。

### 2. 请求头透传有条件，body passthrough 不是 header passthrough

`relay/channel/api_request.go:SetupApiRequestHeader` 默认只处理 Content-Type/Accept；不自动转发 `OpenAI-Beta` 或子代理标识头。Codex adaptor 会填自己的 `responses=experimental` 默认值。

项目已在 `setting/operation_setting/channel_affinity_setting.go` 内列出 `X-Codex-Parent-Thread-Id`、`X-OpenAI-Subagent`、session/thread 等头，但它们属于亲和性参数覆盖模板：默认需模型匹配 `^gpt-.*$`、路径匹配 Responses 且 `prompt_cache_key` 非空。

`relay/responses_handler.go` 的 body-passthrough 分支不执行 `ApplyParamOverrideWithRelayInfo`，因此不能仅依赖该模板转发头。显式渠道 Header Override 在 `DoApiRequest` 中仍生效。测试使用显式覆盖来验证三种模式，不能据此宣称默认配置全部透传。默认 Codex 亲和性头列表也没有 `OpenAI-Beta`。

### 3. 普通字段过滤存在大整数精度损失

`relay/common/relay_info.go:RemoveDisabledFields` 在非 passthrough 模式使用 `map[string]interface{}` 重编组。临时探针把工具参数 schema 的 const 送入真实过滤函数，得到：

```text
before: 9007199254740993
after:  9007199254740992
```

`SanitizeInvalidResponsesItemIDs` 在需要删除 ID 时也会重新编码 `map[string]any`。因此保留 RawMessage 的 DTO 不足以保证整个处理链无损。

### 4. encrypted_content 清理会递归修改工具结果

`relay/common/responses_sanitize.go` 遍历 `input` 的所有嵌套 map/array，遇到数组元素中的可疑 `encrypted_content` 就可能删除该元素，没有将清理限制为协议层 reasoning/compaction 项。

临时探针将普通工具结果内的 `metadata:[{"encrypted_content":"plain tool data"}]` 放入 `function_call_output.output`，结果该 metadata 数组变为空数组，删除数为 1。Hosted agent_message 的不透明消息也不应被套用 reasoning 启发式规则。

### 5. Responses WebSocket 未接入

`router/relay-router.go` 仅提供 POST `/v1/responses`、POST `/v1/responses/compact`；现有 WebSocket GET 路由是 `/v1/realtime`。没有 Responses WebSocket 升级路由及 `response.inject` 转发链路。不能用已有 Realtime 支持推断 Responses WebSocket 可用。

### 6. 跨协议转换不是完整透传

Gemini/Vertex 可以通过 namespace 展平、反向映射实现客户端 function 协作；新测试已覆盖。但 `previous_response_id` 等服务端历史引用明确不支持，未知 opaque items 会按照 loss policy 被遗漏或拒绝，不能承载 OpenAI hosted agent tree 的完整语义。`strict` 策略也会拒绝 namespace 展平诊断。

`service/openaicompat/responses_chat_compat.go` 的 Responses→Chat 辅助函数还存在 namespace/custom 历史和结构化工具结果损失；全库查找未发现当前 `/responses` 生产链路调用该请求转换，只发现定义、包装和测试，因此不是原生 `/responses` 已确认的阻断。

### 7. 子线程后续请求与上游状态

当前没有独立的子代理后续请求接口。父线程和子线程继续请求同一 `/v1/responses`，按各自模型、凭据、额度和渠道条件处理。项目有基于 `prompt_cache_key` 的渠道亲和性，但这不是对任意 `previous_response_id` 的账号/密钥绑定保证。测试验证字段转发，不验证线上多渠道、多密钥状态一致性。

## 验证结果与改动范围

上述两个新增测试通过；定向执行 controller、Gemini、OpenAI、relay/common、service 与 operation_setting 六个包的相关测试均 PASS。

Windows 直接 `go test` 在断言通过后曾报临时 test.exe 删除 Access is denied；改用 `go test -c` 构建到临时目录，再直接执行测试二进制，最终退出码 0。没有把清理失败当成测试通过。

本次只增加两个回归测试和本文档，未修改生产转发逻辑、配置或部署。仓库原有 `deploy/README.md`、`deploy/deploy.sh` 未提交变更保留。

建议修复顺序：保留 hosted multi_agent 配置及 Beta 头 → 限定 sanitizer 范围、保留原始 JSON 数值 → 覆盖所有转发模式的子线程头 → 明确跨协议拒绝策略。若实际客户端使用 Responses WebSocket，再单独实现升级、注入、断线及计费生命周期。

## 核对资料

- 仓库 `CODEX-INNER-TOOLS-CN.md` 的「多 Agent 工具」：客户端工具声明与 function_call_output 形状。
- 通过官方文档工具读取 Codex subagents 指南，核对客户端编排职责。
- 通过官方文档工具读取 Responses Multi-agent 指南，核对顶层配置、Beta 请求头、托管协作输出项与 WebSocket 注入。文档描述的是支持该功能的上游协议，不能推断所有 Codex OAuth 或第三方通道都支持。

```text
https://developers.openai.com/codex/multi-agent/
https://developers.openai.com/api/docs/guides/responses-multi-agent
```

## upstream/main 最新代码复核（2026-11-14）

### 版本与检查方式

已执行 `git fetch --no-tags upstream +refs/heads/main:refs/remotes/upstream/main`，并在检查结束前通过 `git ls-remote upstream refs/heads/main` 再次核对远端。两者均指向：

- upstream：`QuantumNous/new-api`。
- 完整提交：`b388140acfd80369ec6243b16869daff1b4051f4`。
- 提交时间：2026-11-14 01:49:05 +08:00；标题：`docs(plugins): recommend a summary length that fits one marketplace line`。
- 当前工作分支 HEAD：`550d4b0009f807a344bfdee7cc1a985d24a65d54`。
- `git rev-list --left-right --count HEAD...upstream/main`：本地独有 224 个提交，上游独有 1288 个提交。

以下文件与行号均以该 upstream 提交为准，不能直接对应当前工作树同名文件。通过 `git show upstream/main:<path>` 检查源码，并将上游 `relaykit` 导出到临时目录执行测试；没有切换、合并或重置工作分支，也没有修改生产代码。

### 逐项结论

| 原问题 | 最新上游状态 | 源码依据与边界 |
| --- | --- | --- |
| 顶层 hosted `multi_agent` 配置丢失 | **未解决** | `relaykit/dto/openai_request.go:1060–1130` 仍无该字段及未知字段容器；普通路径重新编码 DTO。临时探针实际输出 `multi_agent preserved after upstream DTO: false`。原始 body passthrough 能绕过这个 DTO 丢字段问题。 |
| Beta/session/subagent 请求头与 passthrough 分支 | **未解决默认完整透传** | `relay/channel/api_request.go:52` 默认仅处理 Content-Type/Accept；`relay/channel/codex/adaptor.go:182–183` 仍默认写 `responses=experimental`。亲和性模板有 parent/subagent 头，但没有 `OpenAI-Beta`；`relay/responses_request.go:53–62` 的 body passthrough 提前返回，跳过 `:82–86` 参数覆盖。仍需适当的显式 Header Override。 |
| `RemoveDisabledFields` 重编码导致大整数损失 | **该边界已修复** | `relay/common/relay_info.go:1212–1270` 使用 `ojson` 按路径删除指定成员，保留其余原始 JSON 字节；不再全体解码为 float64 map。不能据此保证所有跨协议转换都无损。 |
| 递归清理 `encrypted_content` 误删工具结果 | **上游不存在这段本地实现** | upstream 没有 `relay/common/responses_sanitize.go` 或 `SanitizeInvalidResponses*` 调用。此前问题来自本地分支代码，不能称为上游提供了一个可直接 cherry-pick 的修复。合并上游也不保证自动删除本地 sanitizer。 |
| Responses WebSocket 与 `response.inject` | **部分解决** | `router/relay-router.go:78` 已接入 GET `/v1/responses`；但 `relay/responses_websocket.go:160–166` 仅接受 `response.create` 及有活动请求时的 `response.cancel`，明确拒绝 `response.inject`。渠道还需启用 `ResponsesWebSocketEnabled`。 |
| namespace/custom/结构化工具结果跨协议转换 | **明显改善，不能称为托管代理完整透传** | 转换已迁移至 `relaykit/relayconvert` 并接入生产链路；支持 namespace 名称映射、custom 调用与结果以及 JSON 结构化工具输出。`internal/oai_responses/to_oai_chat_req.go:122–139` 仍拒绝 `previous_response_id`、conversation、prompt、context_management。未发现 Responses hosted agent tree 的专用映射。 |
| 后续请求的渠道/密钥连续性 | **同一 WebSocket 连接内改善** | `relay/responses_websocket.go:378–379` 固定模型、渠道、分组及 key/index；`:653` 起重新检查有效性。不能扩大为任意 HTTP 后续请求或跨连接 `previous_response_id` 的账号/密钥绑定保证。 |

### 可参考的上游提交

以下为提交时间（+08:00），并非作者时间：

- `d0b840e75ff36d731b04ff981ee361819acd593f`，2026-10-22：`fix(relay): keep the client's JSON Schema property order`。包括过滤及相关 JSON 处理的原始次序/数值保留改进；后续 `70e6af6429`、`f3d8b737b6` 又调整了 JSON/请求体实现。
- `9fe0457ee1f4b9de407a254500d54f5a8f41ee29`，2026-09-14：`Add Responses WebSocket relay support (#5062)`。
- `ae249f4ecbaf3ae9b1fc62cf377ca5a764b9a002`，2026-09-18：`feat(responses-ws): extend channel support and share routing with HTTP`。
- `335ebcff71b843655bf2c64f7a9a33ca21b792ea`，2026-09-30：保留 Chat Completions 上游的 Responses custom tools。
- `1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5`，2026-10-01：保留 Claude/Gemini 上游的 custom tools。
- `0a03088e0e737e99bc7ca936d3d2276e8d636fb8`，2026-10-07：改进有损转换及转换流失败的计费。

上游已发生 `relaykit` 模块及处理链重构，加上双方提交明显分叉，这些提交应作为迁移参考；不宜在未经依赖核对时直接批量 cherry-pick。

### 本轮实际验证

在临时导出的上游 `relaykit` 中执行以下定向测试，四个包全部退出码 0：

```powershell
go test ./dto ./relayconvert/internal/oai_responses ./relayconvert/internal/oai_chat ./relayconvert/internal/toolconv -run 'TestOpenAIResponses|TestResponsesRequestToChatCompletions|TestChatCompletionsResponseToResponsesRestoresCustomToolCalls|TestChatCompletionsStreamToResponsesRestoresCustomToolCalls|TestResponsesCustomTool' -count=1
```

覆盖 custom 历史到 function 的编码、custom 返回类型恢复（含流式）、工具输出内容以及对不支持状态字段的拒绝。它们是转换单元测试，不是对整个上游 controller/认证/调度/计费链的重新实测。

另在临时快照新增并执行 `TestUpstreamAuditMultiAgentRoundTrip`：实测顶层 `multi_agent` 丢失，同时原始 input 中的大整数 `9007199254740993`、嵌套普通工具数据 `encrypted_content` 和显式 `stream:false` 在 DTO 往返后保留。该探针用例以确认现有缺口为目的，PASS 不表示 `multi_agent` 已支持。临时测试未加入当前仓库。

本轮没有运行上游根模块完整测试或 WebSocket 端到端测试：上游根模块声明 Go 1.26.10，当前本机 Go 1.26.2；独立 relaykit 声明 Go 1.25.1，可直接完成上述验证。字段过滤、header 分支、WebSocket 事件及 key 固定的结论来自对应提交源码。前文的 60 次 HTTP 请求与 Gemini 四组合测试属于本地分支的上一轮验证，不应归算为此次 upstream 测试。

### 对原始需求的影响

Codex 客户端的创建、分派、结果回传和后续任务工具仍由 Codex 运行时执行，网关转发其模型请求；此前实测的客户端协作能力不依赖顶层 hosted `multi_agent`。最新上游增强了这类工具调用的跨协议兼容性，但仍不足以宣称服务端托管多代理、注入事件及所有后续请求状态已完整透传。需要继续修补的重点是 hosted 字段保留、Beta/子线程头策略、`response.inject`（若实际协议使用），以及本地 sanitizer 的范围。

