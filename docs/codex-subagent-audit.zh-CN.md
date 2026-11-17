# Codex 子代理与 Responses 转发审计

检查日期：2026-11-14–15。项目：new-api。

## 切回渠道 312 后 SSE 复测（2026-11-15 00:25–00:28，Asia/Shanghai）

**结论：同一 API Key 已切回渠道 312；使用真实 codex-cli 0.167.0，经 SSE 成功完成子代理创建、任务执行、结果回传以及同一子代理的后续任务。** 本轮所有 15 次生产请求均为 POST /v1/responses，HTTP 200 / text/event-stream / response.completed，且在 new-api master 日志逐一匹配 channel_id=312。没有发送 WebSocket 请求。15 次包含六步协议探针、首次本地临时会话失败的两次父请求，以及启用隔离会话存储后成功的七次真实父子请求；HTTP 成功不代表首次子代理创建也成功。

沿用用户提供的公网入口、现有 KEY 和 gpt-6.1-sol；新建独立测试目录与会话，不复用渠道 345 的 response ID 或结果。客户端 provider 显式 supports_websockets=false，回环记录转发器只允许 stream:true 的普通 HTTP POST，GET/Upgrade 不向生产转发。本轮两次真实 Codex 运行均未出现本地被拒绝的 GET/Upgrade。未改变生产渠道设置、用户常用 Codex 配置或代码。

### 实际 Codex 子代理通过项

| 阶段 | 复测结果 |
| --- | --- |
| 原生创建 | collaboration.spawn_agent 创建 /root/sse_child；运行时返回真实子任务名称。 |
| 子代理首轮 | 独立请求带 X-Codex-Parent-Thread-Id 与 X-Openai-Subagent: collab_spawn，输出 CODEX_SSE_CHILD_OK。 |
| 结果回传 | 父请求中记录 author=/root/sse_child、recipient=/root 的 FINAL_ANSWER agent_message。 |
| 原生追加任务 | collaboration.followup_task 的 target 为同一 /root/sse_child。 |
| 子代理续轮 | 保留首轮输入历史和首轮回答，收到第二次任务后输出 CODEX_SSE_FOLLOWUP_OK。 |
| 父代理完成 | 已收到两次子代理结果，最终输出 CODEX_SSE_REAL_SUBAGENT_OK，Codex 退出码 0。 |

成功运行包含父代理 5 次、子代理 2 次请求，均命中 master 的渠道 312（channel_type=1、api_type=0、channel_pass_through=true）。实际记录中的 spawn/followup 的 message 是不透明密文，Codex 将其放入子请求 agent_message 的 encrypted_content 内容块；已按字节匹配这两份密文与相应工具参数。没有解密或替换载荷。父代理收到的两次 FINAL_ANSWER 为明文测试标记。

原先针对渠道 345 的“任务必须包含明文标记”校验不适用于本轮加密载荷；另一个全对象比较仅因 Codex 重放回答时省略 response metadata 和 internal_chat_message_metadata_passthrough 两个字段而失败。保留原始校验结果，再按实际协议逐项验证密文、收发对象、call_id、历史前缀与回答字段，20 项校验通过；未删除生产响应中的字段。此处证明客户端所发密文及实际后续生成正确，未抓取渠道上游原始入站请求，不扩大为任意头部或字段逐字节透传保证。

| 成功运行请求 | master 请求 ID |
| --- | --- |
| 父代理创建 | 20261114162748808451663JYk85jH7 |
| 父代理第一次等待 | 20261114162755687183896FFvRt2DI |
| 子代理首轮 | 2026111416275536141147780e7VMKw |
| 父代理追加任务 | 20261114162800816911928UJLLNf5D |
| 子代理续轮 | 20261114162805824385712ggDU59sb |
| 父代理第二次等待 | 20261114162806158880049WQnF9XR5 |
| 父代理完成 | 20261114162812972119983bEjhGMw7 |

### 首次临时会话失败与重试

首次沿用 --ephemeral，模型本次 spawn_agent 未指定 fork_turns。Codex 本地创建子代理时报 invalid thread-store request: no rollout found for thread id，尚未发出子代理模型请求。两次父请求均 SSE completed；第二次如实报告创建失败。对应 master 请求 ID：20261114162644300568373DCI2gAuJ、20261114162650192299447Am14P3z2，均为渠道 312。

只在新建隔离测试目录中移除 --ephemeral，保留测试专用 CODEX_HOME、read-only sandbox、一个子代理及其余配置后重试，成功完成上方七次请求。这是本地测试运行器缺少会话历史时的创建失败，不能归因为渠道 312 的 SSE 转发失败。此前 345 测试显式 fork_turns=none，条件不同；本轮未把失败记录覆盖或当作通过。

### 六步协议探针与差异

自定义 production_probe namespace 的六次请求均 completed，父子工具结果、namespace/call_id、增量拼接、后续任务和最终 PARENT_CHILD_ROUNDTRIP_OK 均正确；子会话完整返回 CHILD_PROBE_OK|9007199254740993|opaque_tool_keep。

严格检查为 17 项通过、1 项不符、1 项未覆盖：custom 工具实际 input 为 probe_status() 加一个末尾换行，未与要求的无换行字面值完全相等；SSE delta、done 和最终 input 三者一致，因此不能据此认定代理转发截断。该工具为脚本模拟返回，不把测试标记当作真实工具执行证据。此轮没有返回 reasoning.encrypted_content 项，所以该项重放校验记录为未覆盖；原生协作消息中的 encrypted_content 另按上节验证，不混为一项。

六步请求 ID 依次为：20261114162533292729864Q4W02Bt6、20261114162539103050937PXLOFHus、20261114162544515819128OvAFDIP、20261114162547872956525S6vjI8iC、20261114162551109341634OfmMJaZ9、20261114162554284424210Pu1KMOUA。

### 用量与范围

- 六步协议探针报告输入 1,697、输出 122、合计 1,819 tokens，应用结算日志合计 USD 0.000612。
- 首次本地创建失败的两次父请求报告输入 30,915、输出 85、合计 31,000 tokens，结算日志合计 USD 0.001058。
- 成功真实父子运行报告输入 110,271（cached 92,544）、输出 165、合计 110,436 tokens，结算日志合计 USD 0.006164。
- 15 次请求各观察到一条应用结算记录，总计 USD 0.007834；用量为上游报告，未独立审计 tokenizer 或数据库余额。
- 本轮验证一个真实子代理及一次后续任务，采用完整历史重放，不使用 previous_response_id；未扩大到多代理并发、深层嵌套、故障恢复或服务端托管 multi_agent。

记录保存于本机临时目录 new-api-production-responses-probe/sse-channel312-20261115-002517；real-codex 保留首次临时会话失败，persistent-codex/real-codex 保留成功重试。仓库仅更新审计报告；未部署、重启或修改生产配置，且本轮不改变此前 WebSocket 测试结论。

## SSE 实际 Codex 子代理验收（2026-11-14 23:50–23:55，Asia/Shanghai）

**结论：当前 new-api master、渠道 345、gpt-6.1-sol 已通过真实 Codex 子代理的 SSE 创建、任务分派、结果回传和同一子代理后续任务验证。本轮没有发送 Responses WebSocket 请求。** 先完成六步脚本协议探针，再以本机 codex-cli 0.167.0 发起真实协作调用，弥补此前仅模拟客户端编排的验收边界。

### 真实客户端与传输约束

独立运行 codex exec，使用隔离的临时 CODEX_HOME 和工作目录、--ignore-user-config、--ephemeral、read-only sandbox、features.multi_agent=true；只允许一个子代理。父子任务使用用户指定模型 gpt-6.1-sol，不执行文件、shell、搜索或其他业务工具。未修改用户现有 Codex 配置。

本轮自定义 provider 显式设置 supports_websockets=false、wire_api=responses。为确保不会误发 WS，在本机回环地址增加只允许 POST /v1/responses 且 stream:true 的记录转发器：任何 GET/Upgrade 都在本地拒绝；原 JSON 请求体直接转发至用户提供的公网入口，不替换协作工具 schema，不模拟工具执行。API Key 仅在进程内使用，未写入配置或结果文件。实际记录为 7 次 POST/SSE，0 次被拒绝的 GET/Upgrade，全部 HTTP 200、text/event-stream、response.completed。

### 真实协作链路

| 阶段 | 结果与证据 |
| --- | --- |
| 创建子代理 | 模型调用原生 namespace=collaboration、name=spawn_agent，task_name=sse_child、fork_turns=none；Codex 执行后返回 task_name=/root/sse_child。 |
| 子代理执行 | 子代理独立请求带 X-Codex-Parent-Thread-Id 和 X-Openai-Subagent: collab_spawn，返回 CODEX_SSE_CHILD_OK。 |
| 结果回传父代理 | 父代理后续请求包含 type=agent_message、author=/root/sse_child、recipient=/root，内容为 FINAL_ANSWER / CODEX_SSE_CHILD_OK。 |
| 追加任务 | 父代理实际调用 collaboration.followup_task，target=/root/sse_child，要求返回 CODEX_SSE_FOLLOWUP_OK。 |
| 同一子代理续轮 | 第二个子代理请求保留首轮历史与首轮回答，新的 NEW_TASK 仍发给 /root/sse_child，返回 CODEX_SSE_FOLLOWUP_OK。 |
| 父代理最终汇总 | 父代理输入已包含两次实际子代理 FINAL_ANSWER，最终返回 CODEX_SSE_REAL_SUBAGENT_OK；Codex 退出码 0。 |

以上由实际 Codex 运行时完成，Python 转发器仅记录和转发 HTTP。不能仅凭最终成功标记判定成功；本轮另行验证了 spawn 返回、父子请求、call_id、agent_message 收发对象、原生 followup 调用和子代理历史连续性，16 项独立断言全部通过。父代理共 5 次请求，子代理共 2 次请求。7 次请求均在 new-api master 日志精确匹配，选择 channel_id=345、channel_type=1、api_type=0、channel_pass_through=true。

| 实际 Codex 请求 | master 请求 ID |
| --- | --- |
| 父代理创建 | 20261114155507776357636phiUtYFa |
| 父代理第一次等待 | 20261114155513487796159VYUltocK |
| 子代理首轮 | 20261114155513308544217UhXiEeDx |
| 父代理追加任务 | 20261114155518530485363T5fmOEoO |
| 子代理后续任务 | 202611141555247447954068oh7fvDm |
| 父代理第二次等待 | 20261114155524846900844N9KbT58R |
| 父代理最终结果 | 20261114155530765298997iZdNe3xf |

### SSE 协议补充探针

23:50–23:51 先执行自定义 production_probe namespace 六步链路：父任务创建 → 子会话 custom 工具调用 → custom_tool_call_output 回传 → 父任务 followup_task → 子会话后续回答 → 父任务完成。六次均 HTTP 200 / SSE / response.completed，也全部在 master 日志命中渠道 345。19 项断言全部通过，包括 namespace、call_id、function/custom 参数增量拼接、文本增量与最终输出一致、父子会话隔离、子会话历史续传，以及返回的 reasoning.encrypted_content 在后续请求中原样重放。

子会话精确返回 CHILD_PROBE_OK|9007199254740993|opaque_tool_keep，后续返回 CHILD_FOLLOWUP_OK，父会话最终返回 PARENT_CHILD_ROUNDTRIP_OK。这部分由脚本模拟编排，用于覆盖 custom 工具和文本载荷保留；真实原生协作结论以本节上方 Codex 测试为准。大整数是在 input_text JSON 文本中验证，不扩大为任意 JSON 数值路径的无损保证。

六步探针请求 ID 依次为：20261114155032881193800NA31ujxb、20261114155036927275231kzN1T3FB、20261114155040368154830XizfAalq、20261114155045146192661cnIZGxto、20261114155053309938056cMRmsdZ5、20261114155057765570938X9R2ABP4。

### 用量与边界

- 六步探针上游报告输入 2,474、输出 271、合计 2,745 tokens，应用结算日志合计 USD 0.002044。
- 真实 Codex 父子合计上游报告输入 85,586（其中 cached 61,479）、输出 385、合计 85,971 tokens；应用结算日志合计 USD 0.016590。Codex CLI 最后显示的 turn usage 只覆盖父代理，不能用其代替父子总量。所有响应的 customer_usage 均标记 estimated；这些是上游报告值，不是独立 tokenizer 审计。
- 两组共 13 个请求，每个观察到一条应用结算记录，合计 USD 0.018634；没有读取数据库余额，不能代替持久化账务对账。
- 本轮证明本机该版本 Codex 经当前公网入口 / new-api master / 渠道 345，能够使用 SSE 完成一个子代理及一次追加任务。没有验证多子代理并发、深层嵌套、故障恢复、跨渠道/凭据切换或服务端托管 multi_agent。
- 实际 Codex 请求完整重放历史，未使用 previous_response_id；本轮不改变此前 HTTP 服务端历史续轮的兼容性结论。记录到了客户端发出的父子标识头，未捕获渠道上游原始入站请求，不能宣称头部逐字节透传已证实。
- 原生 collaboration 工具这次由 Codex 自己提供 schema，实际通过。此前手写保留工具 schema 被拒绝的结果不能用于推断真实 Codex 子代理不可用。
- 此测试在客户端明确禁用 WS 后成功，说明本轮验收的子代理链路无需 WS。未修改生产渠道开关，未重新测试此前 WS 上游账号问题。

本轮保持生产只读，未部署、重启或修改渠道。脱敏请求、流事件、校验脚本和按请求 ID 筛选的日志保存在本机临时目录 new-api-production-responses-probe/sse-channel345-20261114-235014；其中 real-codex 保存真实客户端记录。仓库仅追加本报告，保留全部历史结论。

## 渠道 345 启用 WS 后复测（2026-11-14 23:45，Asia/Shanghai）

**结论：渠道 345 的 Responses WebSocket 开关已在 master 生效；请求已通过网关渠道门禁并连接上游。当前阻断是上游随后发送 WebSocket close 1013，原因为 no available account。普通 HTTP/SSE 正常。** 下方 23:40 的开关未生效记录为此前状态。

使用原 API Key 与 gpt-6.1-sol，全部建立新连接，不复用旧 response ID。一次 namespace 工具请求、一次公网最小消息请求、一次直连源站最小消息请求均失败；没有继续发送缺少有效 response_id/call_id 的续轮、注入或取消探针。

| 检查项 | 客户端结果 | master 日志 |
| --- | --- | --- |
| 公网 WS namespace 调用 | 握手 101，create 返回 502 / bad_response | 请求 0a25c9c9b8cb45c0aad5d55c06e8e142，渠道 345，scanner error: websocket: close 1013: no available account，received=0。 |
| 公网最小 WS 推理 | 握手 101，create 返回相同 502 | 请求 824fa7c63b7142df9b0058e8bed11af0，同样由上游 close 1013 / no available account 终止。 |
| 源站最小 WS 推理 | 握手 101，create 返回相同 502 | 请求 b5a0bfd497334aa4b1a70b485d3ae34c；直连 167.160.90.42，TLS SNI/Host 为 alb.jucodex.com、证书校验开启，绕过 Worker 后仍相同。 |
| HTTP/SSE 对照 | 200 → response.completed，精确输出 MASTER_HTTP_SSE_OK | 请求 20261114154509677430546M0x5h3rr，同样命中渠道 345，输入 23 / 输出 9 / 合计 32 tokens。 |

三个 WS 请求均为原生 OpenAI 类型（channel_type=1、api_type=0），body passthrough 开启。它们已进入 responses relay selected channel 之后的上游读取阶段，没有再出现未开启 WS 的 400、Worker 1101 或 HTTP/2 二进制解析错误。最小消息探针不带 event_id，也没有 namespace/tools，因此本轮失败不依赖此前 event_id 参数或工具 schema 问题。

客户端仅看到上游流提前结束的泛化 502；master 内部日志保留了上游 close 的明确原因。该证据说明需要排查 **渠道 345 对应上游的 WS 账号可用性/调度条件**，不能断言账号永久不支持 WS，也不能根据 HTTP 正常推断 WS 账号选择一定正常。可能需要核对账号的 WS 启用状态、该凭据/分组的可用账号及模型权限、临时容量限制；这些是后续核查项，并非本轮已确认原因。

尝试读取已授权 sub2api master 的诊断日志，在返回的最近 2,000 行及本次时间窗内未找到可关联记录；尚未证明渠道 345 的上游就是该实例，不能用未匹配日志认定它没有收到请求。未读取上游凭据或修改调度配置。

三个失败 WS 请求均显示 effective_preconsume_quota=0、received=0，没有模型 usage。HTTP 对照应用结算日志显示 USD 0.000026；没有核对账务数据库余额。续轮、response.inject、response.cancel 和 event_id 兼容性仍未能在渠道 345 上验收。

本轮保持远程只读，仅发出上述用户授权探针；未修改生产代码、渠道、Nginx 或容器。完整结果及按请求 ID 筛选的 master 日志保存于本机临时目录 new-api-production-responses-probe/channel345-ws-enabled-20261114-234455，保留前轮记录。

## API Key 切换渠道后复测（2026-11-14 23:40，Asia/Shanghai）

用户将现有 API Key 改到另一渠道后，使用同一凭据及 gpt-6.1-sol 新建连接复测，没有复用此前 WS 或 response ID。master 应用日志确认本轮两个请求均命中 **渠道 345**，不再是上一轮渠道 312。new-api-master 仍为 23:25:49 启动的容器，healthy，重启次数 0。

| 检查项 | 结果 | 证据 |
| --- | --- | --- |
| 公网 WS 握手 | 101 | CF-Ray a5a71d0d7fa284e9-LAX；Worker / Nginx 接入仍正常。 |
| WS response.create | 网关 400 / invalid_request | Responses WebSocket requires an enabled native OpenAI/Codex channel；master 请求 ID a5ac2c6713294bf0984ca41b0c5c1004，日志记录 channel #345。 |
| 普通 HTTP/SSE | 200 → response.completed | 精确输出 MASTER_HTTP_SSE_OK；请求 ID 20261114154016723560016zf7S7Ee2，channel_id=345、channel_type=1、api_type=0、channel_pass_through=true。 |
| WS 工具结果续轮、inject、cancel | 未执行 | 首次 create 已被渠道门禁拒绝，尚未连接新渠道的 WS 上游；重复控制探针不能验证上游能力。 |

对应 ResponsesHelper 检查在调用上游前要求原生 OpenAI/Codex 类型且 ResponsesWebSocketEnabled 为 true。渠道 345 的 HTTP 日志确认是原生 OpenAI 类型；结合 WS 拒绝结果，当前运行时的该渠道 Responses WebSocket 未启用。未读取数据库原始 setting，因此不区分未保存配置与缓存尚未生效。此结果不是新上游不支持 WebSocket 的证据，也不能用于判断此前 event_id、注入、取消问题是否已解决。

后续若要继续 WS 对比，需使 **渠道 345** 的「启用 Responses WebSocket」设置生效（setting.responses_websocket_enabled=true），再建立新连接。此次保持远程只读，没有代为修改渠道、重启或部署。普通 HTTP/SSE 请求不受这个开关阻断。

本轮仅有一次完成的模型响应，usage 为输入 23、输出 9、合计 32 tokens；应用日志显示结算 USD 0.000038。被拒绝的 WS create 显示实际预扣 0，未生成模型响应；未做数据库余额对账。

探针结果单独保存在本机临时目录 new-api-production-responses-probe/channel-switch-20261114-234005，未覆盖上一轮渠道 312 的记录。以下 master 成功记录均属于此前渠道 312，不可套用到本次渠道 345。

## master 与 Worker 更新后复测（2026-11-14 23:31–23:34，Asia/Shanghai）

本节为渠道 312 的生产结果，后文保留此前 slave 与未更新 Worker 的历史结论。用户明确要求当轮检查 **new-api master（13000）**。

**结论：公网及源站 WS 握手、基础生成、namespace 工具往返、同连接 previous_response_id 续轮均通过。此前 Worker 1101 和 TLS/HTTP2 二进制解析错误未再出现，两处修复的预期行为已在生产验证。response.inject 与 response.cancel 尚未通过当前渠道的实际功能验证。**

### 目标实例与路由证据

- nginx 诊断输出确认 alb.jucodex.com 的 newapi_backend 已指向 127.0.0.1:13000；/v1/responses 保留 HTTP/1.1 Upgrade 配置。master.jucodex.com 是另一个 sub2api 的入口，不以域名中的 master 判断本项目角色。
- new-api-master 容器 ID：05d091aefd30356edecc1fc16299b9709c8d2949f5ced9a194e78930495ce58a；healthy，重启次数 0。
- 镜像 new-api-local:master，image ID：sha256:ffc25001588dda9cef5fc81d834578c80ed16780795dfe2c9c9835a1fabc4a1a；镜像创建于 2026-11-14T15:25:39Z，容器启动于 15:25:49Z（北京时间 23:25:49）。
- 本地 HEAD 为 1df7f3d33b（fixed bug&worker）；生产镜像没有 revision 标签，不能据此证明精确 commit。生产请求日志在 new-api master 中匹配，全部测试模型请求选择渠道 312（OpenAI 类型、body passthrough）。

### 通过的实际请求

| 项目 | 结果 | 证据与边界 |
| --- | --- | --- |
| 公网 WS | 101 → response.completed，输出 WS_MASTER_DEPLOYED_OK | 经 api.jucodex.com / Cloudflare；CF-Ray a5a71094dead1f56-LAX。 |
| 源站 WS | 101 → response.completed，同样输出 WS_MASTER_DEPLOYED_OK | 直连 167.160.90.42:443，TLS SNI/Host 保持 alb.jucodex.com，并验证证书；绕过 Worker。 |
| namespace 工具调用 | response.completed，namespace=production_probe、name=inspect，call_id 完整 | 自定义测试 namespace，不是实际 Codex 子代理工具 schema。 |
| 工具结果回传 | 同连接带 previous_response_id 再次 create，response.completed | 精确输出 CHILD_WS_OK｜9007199254740993｜opaque_tool_keep；这里用全角分隔符展示，实际输出是 ASCII 竖线。 |
| 后续请求 | 第三次同连接 create 引用第二次 response ID，response.completed | 精确重现前轮文本并追加 ASCII 竖线及 FOLLOWUP_OK。 |
| 普通 HTTP/SSE | 200 → response.completed，输出 MASTER_HTTP_SSE_OK | 请求 ID 20261114153252944100249BHGZ8RnN；说明 WS 修改后该路径仍正常。 |

工具结果通过 function_call_output 字符串回传，大整数与嵌套 encrypted_content 哨兵完整重现。这是所测样例的语义验证，不等价于捕获上游原始请求，也不证明全部透传路径均无损。编排由测试脚本执行，没有启动真实 Codex 子代理进程。

### 未通过与兼容性边界

1. 带 event_id 的 response.create：公网、源站均返回上游 400 / Unsupported parameter: event_id。去掉此可选字段后两条路径生成成功。当前网关会把客户端 event_id 加回上游，故携带该字段的真实客户端仍有兼容性问题，不能宣称所有客户端请求形状都通过。
2. 字符串 input 的 WS create：当前上游返回 400 / Input must be a list；改成消息数组后通过。普通 HTTP/SSE 的字符串 input 请求则成功。探针已按两种传输实际接受的形状区分，未修改生产参数策略。
3. response.inject：先完成 function_call，再向同连接、真实 response_id/call_id 注入工具结果；公网及直连源站均未收到注入回执，而是异常断连（无 close frame）。此次仅测试完成后的工具结果注入，不能扩大为对托管多代理活动响应注入的完整测试。
4. response.cancel：收到 response.created 后立即发送针对该 ID 的取消；公网和源站均继续输出到 128 tokens，并以 response.incomplete / reason=max_output_tokens 终止，未观察到取消成功。

控制事件的问题绕过 Worker 仍复现，不能归咎于 Cloudflare。当前证据还不足以在 new-api 转发与上游事件实现之间最终归因，需要上游接收/处理日志或直接上游对照；本轮未读取渠道密钥或绕过访问权限，也未为未确认的兼容性原因修改生产代码。

### master 日志与结算核对

| 探针 | master 内部请求 ID |
| --- | --- |
| 公网基础生成 | 3a3ffae5b3624505a15f3ea07d4b7bc3 |
| 源站基础生成 | 1457752bfe5445ac90795b0d050834bd |
| namespace 工具调用 | acaf0bffe7bf4057a38115171272009d |
| 工具结果续轮 | a1685b76b81f430187cd8487ddb8c846 |
| 后续请求 | 3245b07fb34d4c8fac8fb69c64abfbb8 |
| 公网注入前工具生成 | dadb989fa8a64e05b5b111533cbf6c30 |
| 公网取消未生效 | b915ad94875840b5b06384ee82227d22 |
| 源站注入前工具生成 | 7fb5bf0d39824e099090bd31ff271eed |
| 源站取消未生效 | 56fcab40f04d40a789f2e8762f4a755d |

本轮共有 10 个带 usage 的终止响应（8 completed、2 达 token 上限的 incomplete），累计 1,214 tokens（输入 798、输出 416）。另有 4 次参数被拒绝的 create，无模型输出。

抓取并按本次 token/请求 ID 过滤 master 应用日志，10 个推理请求各观察到一条结算日志，合计显示 USD 0.000766；均显示实际预扣 0 后补扣。捕获范围内未见这 10 个请求重复结算，也未见此前的 malformed HTTP response 或 WS panic。此处是应用日志证据，没有读取账务数据库或核对余额，不能代替持久化账务审计。

本轮只进行远程只读诊断与用户授权的小额模型探针；没有部署、重启、修改渠道或 Nginx。完整探针结果及筛选日志留在本机临时目录 new-api-production-responses-probe 的 master_* 文件，仓库仅记录脱敏结论。当前可使用已验证的 create + function_call_output + previous_response_id 路径；注入、取消以及 event_id 兼容性保留为未解决项。

## 生产复测（2026-11-14）

目标为用户提供的 api.jucodex.com，模型 gpt-6.1-sol，使用用户授权的 API token。复测时间为 2026-11-14 22:30–22:36（Asia/Shanghai）；本地对照提交为 2f0cb1373f。无法通过外部接口证明生产二进制的精确 commit，以下结论以实际响应为准。密钥未写入仓库或测试报告，未修改生产渠道开关及配置。

**结论：HTTP/SSE 工具往返和新增绑定拒绝逻辑已在生产观察到；WebSocket 公网握手未通过；不能宣称所有子代理能力已经端到端验收。**

| 项目 | 生产结果 | 结论与边界 |
| --- | --- | --- |
| Responses JSON | 200 / completed，输出 HTTP_PROBE_OK | 基础 HTTP 请求正常。请求中的 store:true 不代表当前账号支持服务端历史续轮。 |
| SSE 协作工具六步往返 | 六次均 200 / completed；最终输出 PARENT_CHILD_ROUNDTRIP_OK | 自定义 namespace 创建任务调用 → 子会话 custom 工具调用 → 工具结果回传 → 父会话 followup_task → 子会话后续请求 → 父会话最终结果均通过，call_id/namespace 可关联。 |
| 工具载荷 | 文本载荷中的 9007199254740993 与嵌套 encrypted_content 哨兵完整返回；另一个 schema 数字 enum 探针也返回精确整数 | 支持这些样例的黑盒语义验证；未捕获上游原始 JSON，不能证明所有数值、字段顺序和重复字段都无损。 |
| 无绑定 previous_response_id | 409，previous_response_id binding is unavailable，并返回 x-should-retry:false | 新增拒绝逻辑已生效。未用第二个 token 或更换渠道凭据测试隔离。 |
| 已返回 ID 的 HTTP 续轮 | 400，previous_response_id requires an OpenAI API-key account for HTTP requests | 通过了本地未知 ID 拒绝路径，但当前上游账号不支持该种 HTTP 续轮；完整历史重放的六步链路正常。不能据此证明跨实例缓存及账号稳定性。 |
| 顶层 multi_agent | 400，Unsupported parameter: multi_agent | 带该字段的请求被拒绝，和基础请求形成差异；与字段保留修复相符。当前上游接口未接受此参数，没有验证服务端托管代理。 |
| 原名 collaboration.spawn_agent | 400，reserved for use by this model and must match the configured schema | 两个手写 schema 变体均被拒绝，不能用此探针证明实际 Codex 工具 schema 兼容。六步成功链路使用 production_probe namespace，工具编排由测试脚本执行，没有启动真实 Codex 子代理进程。 |
| Responses WebSocket | 多次握手均 HTTP 400 / Bad Request，未得到 101；独立 HTTP/1.1 手工握手同样失败 | 失败发生在发送 response.create 之前，尚未检查渠道 responses_websocket_enabled，也没有验证上游 WS、注入、取消及计费。 |
| compact hosted 拒绝 | 503 / model_not_found，gpt-6.1-sol-openai-compact 无可用渠道 | 分发先于 compact 内容验证，当前配置无法触达新增的 400 校验；不是该校验通过。 |
| function_call_output 直接 JSON object | 400，expected one of a string or array of objects | 当前上游拒绝直接 object；六步链路使用受接受的字符串及 input_text 数组。不能把该协议类型错误归因于 JSON 精度修复。 |

### WebSocket 排查依据

使用 websockets 16.0 与独立 HTTP/1.1 握手发出合法 Connection: Upgrade、Upgrade: websocket、Sec-WebSocket-Version: 13 和随机 16 字节 base64 key；去掉压缩、补充 Beta 头均未成功。诊断响应包含 Sec-WebSocket-Version: 13 和 X-Oneapi-Request-Id，与当前 Gorilla upgrader 的 400 输出一致。

结合 controller/responses_websocket.go 先 Upgrade、后读取 create 并校验渠道的顺序，公网入口上的 WebSocket Upgrade 请求头未完整转发到应用是优先排查项。外部黑盒结果不能确定具体是哪一层反向代理，也不能直接证明生产渠道开关状态。应按请求 ID 对照入口代理与应用日志，检查到应用的 HTTP 版本及 Upgrade/Connection/version/key 头，再尝试 101 后的推理。不要仅通过打开渠道开关来处理当前握手 400。

### SSH 源站复核（2026-11-14 22:58–23:01，Asia/Shanghai）

用户安装四服务只读诊断脚本后，已通过 SSH 复核 new-api slave，而非此前只读白名单指向的 sub2api slave。容器 new-api-slave 运行于 13001，healthy，重启次数 0；镜像为 new-api-local:slave，image ID 为 sha256:2535857750f4e81927c89ec06db70d9f6afa5bc36c19ede1ec6c7769ba32a306，创建于 2026-11-14T14:02:40Z。镜像没有 revision 标签，/api/status 也未返回非空版本号，不能证明精确 commit。

同一 API token 的握手对照结果：

| 路径 | 结果 |
| --- | --- |
| SSH 受限探针 → 127.0.0.1:13001/v1/responses | **101 Switching Protocols** |
| 本机测试端 → 167.160.90.42:443，TLS SNI/Host 为 alb.jucodex.com | **400**，请求 ID 202611141459024431674916V9V9WTZ |
| 本机测试端 → api.jucodex.com 公网入口 | **400**，请求 ID 20261114145902491079944T0qxRm2y |

读取 nginx -T 的过滤结果确认：/etc/nginx/conf.d/alb.jucodex.com.conf 的 newapi_backend 指向 127.0.0.1:13001；TLS server 设置 proxy_set_header Connection ""，普通 API 正则 location 没有转发 Upgrade；只有 /v1/realtime 的专用 location 配置了 Upgrade 与 $connection_upgrade。/v1/responses 命中普通 API 规则。**已确认该源站 Nginx 配置存在 Responses WebSocket 握手阻断，应用本机握手正常。** 直接连接源站也复现 400，因此该问题不需要经过 Cloudflare 即可复现；不据此推断所有边缘配置均正确。

建议在 alb.jucodex.com 的现有 TLS server 内加入 location = /v1/responses，转发到 newapi_backend，并设置 proxy_http_version 1.1、Upgrade $http_upgrade、Connection $connection_upgrade，以及现有 Host/X-Forwarded-* 头。沿用 nginx.conf 中已有的 http 级 map。可审阅片段已保存为本机 Downloads/tmp-ssh/new-api-responses-ws.location.conf；该片段不能直接放到 conf.d 顶层。应用前须验证 nginx -t，再由管理员 reload。

在 new-api slave 容器日志中已匹配上述两个请求 ID，均为 2026-11-14 22:59:02 的 GET /v1/responses / 400。这确认公网与源站 Nginx 探针都实际到达该 slave，并非误测 master 或其他服务。

本次保持远程只读，未修改 Nginx、渠道开关或容器；只进行握手，没有发送 response.create。因此 101 证明的是客户端到应用的 WebSocket 接入，尚不证明渠道开关已开启、远端支持 WS、注入/取消及计费正常。

### Nginx 修正后的复测与第二轮修复（2026-11-14 23:08–23:09，Asia/Shanghai）

用户应用 Nginx 片段后，读取配置确认 /v1/responses 已设置 HTTP/1.1 及 Upgrade 转发。源站 TLS 入口握手已从 400 变为 **101**，但公网入口与上游推理仍有两个独立阻断：

| 路径 | 实测结果 | 定位 |
| --- | --- | --- |
| 公网 api.jucodex.com | HTTP 500，正文 error code: 1101；CF-Ray a5a6ef8d1fcf1ddd-LAX | 发生在 Cloudflare 层，没有得到 new-api 请求 ID。仓库 Worker 对上游 101 重新构造普通 Response，丢失 webSocket，与该现象相符；未取得线上 Worker 异常堆栈或部署版本，最终归因仍需发布后复测。 |
| 直连源站 IP，SNI/Host 为 alb.jucodex.com | 握手 101；发送 response.create 后收到 do_request_failed / 500 | 请求 ID cd6b20d49dff48129d3d6071d63e8918；slave 日志选择渠道 312（OpenAI 类型、body passthrough），随后出现 malformed HTTP response 与 \x00\x00\x12\x04 等二进制内容。这些内容是 HTTP/2 帧，客户端正在按 HTTP/1.1 解析。 |

该 create 已通过渠道 WebSocket 开关检查，当前故障不是该渠道开关关闭。101 仍只证明接入握手成功；这次没有获得模型输出，也没有验证注入、取消及结算全链路。诊断过程中未修改远程服务或渠道配置。

本地补充修复：

- web/jucodex-worker/src/index.js：上游返回 101 时直接返回原 Response，保留其升级连接；普通 HTTP/SSE 继续使用原有 no-cache 处理。
- controller/responses_websocket.go：通过独立 helper 构造 WS dialer，先 Clone 共享 HTTP transport，再使用独立 TLS 配置，把该 WS 连接的 ALPN 限定为 http/1.1；保留代理、DialContext、证书和其他 TLS 设置，不关闭普通 HTTP 请求的 HTTP/2。
- 新增 TLS 回归先用普通 HTTP/2 请求预热共享 transport，再通过同一配置发起 WSS。修复前复现与生产相同的二进制帧解析错误；修复后握手、消息读取、HTTP/1.1 协商及共享配置隔离断言通过。
- Worker 新增四项无需额外依赖的回归：Responses/Realtime 的原始升级 Response 与 socket 保留、请求头/query 转发、POST/SSE 无缓冲与缓存头、401 握手拒绝正文。升级用例修复前失败，修复后四项通过。

Worker 测试使用 Node VM 和模拟 fetch/升级对象，不等价于真实边缘 WebSocket 帧端到端测试。本机 Miniflare/workerd 原生运行时出现 access violation，未完成该路径的验证；Wrangler deploy --dry-run 打包检查已通过，未发布 Worker。

修复后的检查（均退出码 0）：

- go test ./controller -count=1 -timeout=180s：controller 全包通过。
- go test -race ./controller -run '^TestResponses(WebSocket|HTTPContinuation)' -count=1 -timeout=180s：WebSocket 和 HTTP 续轮定向 race 检查通过。
- go build -o 临时目录/new-api-responses-ws-check.exe .：根模块编译通过，未替换部署产物。
- web/jucodex-worker 下 bun run test：4/4 通过；bun x --no-install wrangler deploy --dry-run --outdir 临时目录/new-api-worker-ws-dry-run：打包检查通过。
- git diff --check：通过。未新增依赖，未改 lockfile。

部署范围是 **new-api slave 后端镜像（13001）及 Cloudflare jucodex-worker 两个组件**；只重建后端不会更新 Worker，只发布 Worker 也不会解决上游 TLS 协商。Nginx 修改已由用户应用，本地修复尚未提交或部署。更新后应再次验证公网 101、response.create 的完整输出，再检查续轮、注入/取消及计费，不能据此提前认定生产 WS 全链路已通过。

### 可关联的生产请求 ID

| 探针 | X-Oneapi-Request-Id |
| --- | --- |
| JSON 基础请求 | 20261114143023278366060i4KpedAm |
| 无绑定 ID 拒绝 | 20261114143023294244441mraddpgD |
| compact 渠道缺失 | 20261114143024267916946uakM4osn |
| 已返回 ID 的续轮被上游拒绝 | 20261114143149307133245WqzRydg3 |
| 原名协作工具 schema 被拒绝 | 20261114143215835799180JK8gAIBD |
| 六步链路：创建任务 | 2026111414340433862363hVixTeTx |
| 六步链路：子会话工具调用 | 20261114143408417410971UYn20oGm |
| 六步链路：子会话结果 | 20261114143412782426609CjJJNcD |
| 六步链路：后续任务 | 202611141434163580384761nsJQGcK |
| 六步链路：子会话续轮 | 20261114143421119562030XXwaCfBm |
| 六步链路：父会话最终结果 | 20261114143424328742150DQN3sa1W |
| hosted 参数被拒绝 | 20261114143404519556856AUAmtv9m |
| 独立 HTTP/1.1 WS 握手 | 2026111414345465741109816rqXOfb |
| 数字 enum 与 opaque schema 探针 | 20261114143554123496761z6WhCDhz |

8 次完成的推理响应累计报告 6,294 tokens（输入 6,100 / 输出 194）；未读取生产消费日志或实际金额，不能据此验证扣费/退款金额。Session/parent/subagent/Beta 头随探针发送，但上游接收值不可见；跨渠道、跨账号、多实例、凭据变更、WS 终止时结算等仍仅有下述本地 mock/单测证据。

测试脚本及脱敏的逐请求 JSON 保存在本机临时目录 new-api-production-responses-probe，仓库只记录上述结论。以下本地回归与原始审计均保留各自检查阶段的历史边界。

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

