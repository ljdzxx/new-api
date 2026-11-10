# 上游用量与 token 结算

按 token 计费的请求只使用上游响应中明确报告的用量。收到响应头、正文、工具事件、图片或正常结束标记，都不能代替上游 usage。

- 缺少 usage：本次实际扣费为零，已预扣的钱包、令牌或订阅额度通过正常结算流程退还。
- 明确返回零用量：保留零值，不以预估输入、正文分词或音频时长补充扣费。
- 返回部分 usage：只按已经报告的计数结算。例如 Claude 的 message_start 报告输入及缓存用量后连接中断，可以结算这些输入及缓存计数，但不能估算未报告的输出计数。
- 流式取消、上游 EOF、失败和正常结束遵守同一规则；Responses 不再因缺少最终 usage 而保留预扣费。
- Realtime 只累计上游 response.done 中的 usage，不再结算本地事件计数或断开时的本地余额。

这次调整针对 token 计费，也包括异步任务：按 token 计费的任务完成后没有报告用量，将全额退还预扣费，并冻结新任务的计费类型。既有按张、次、时长计费规则保持原样；它们的计费依据不是 token usage。已完成的历史账单不会因代码更新自动重算或退款。

## 实现约定

`RelayInfo.RecordUpstreamUsage` 保存上游计数的独立副本。适配器应在解析到 usage 后、进行本地补估或客户端用量缩放之前调用。流式适配器传入已合并的上游累计计数，不能传入本地估算，也不能把增量当作累计值覆盖。

值类型的响应 DTO 无法区分 usage 缺失和全部为零，可用 `RecordUpstreamUsageFromJSON` 检查原始响应对应字段是否确实为对象。Claude、Gemini 和其他原生协议先将上游计数转换为通用结构，再记录快照。`InitChannelMeta` 会清除前一次渠道尝试的快照。

`PostTextConsumeQuota` 和 `PostAudioConsumeQuota` 通过 `usageForSettlement` 读取该快照。适配器为兼容响应格式生成的估算 usage 可以继续用于响应转换，但不能用于 token 结算。后续新增适配器若没有记录上游用量，默认不收取 token 费用。

消费日志的 `other.admin_info.upstream_usage_reported` 标识是否收到明确的上游计数；缺少 usage 而免收费用时，记录 `other.billing_skipped_reason=missing_upstream_usage`，账单 token 数和扣费为零。`local_count_tokens` 仍表示存在本地计数，不能再据此推断账单使用了这些估算。

## 排查覆盖

本次检查涵盖 Chat Completions、Responses 与协议互转、Claude（含透传及 AWS）、Gemini（含 Vertex）、Embedding、Rerank、自定义渠道、音频与 Realtime。

同时接入百度、Cohere、Coze、Dify、Ollama、腾讯、xAI、小米、讯飞、智谱、MokaAI、阿里及 SiliconFlow 原生用量；仅返回本地估算的渠道与接口会被统一结算入口拦下，不再据此扣费。
