# reasoning / compaction 密文重试规则

本文记录当前工作区在本次调整后的 Responses 重试行为（2026-09-27）。

## 场景规则

下表的恢复操作以普通 `/v1/responses` 请求满足安全重试条件为前提。“普通错误”指错误码不是 `invalid_encrypted_content`；明确的密文拒绝优先按该错误码处理，即使其 HTTP 状态也是 502/503。

| 场景 | 原渠道有损恢复重试 | 后续常规重试 / 换渠道 |
| --- | --- | --- |
| reasoning 密文，明确返回 `invalid_encrypted_content`，历史可重建 | 删除 reasoning 后，在原渠道、原 Key 下重试一次 | 成功即返回；再次失败直接结束，不换渠道 |
| reasoning 密文，实际 HTTP 为普通 502/503，历史可重建 | 删除 reasoning 后，在原渠道、原 Key 下重试一次 | 成功即返回；再次失败按最终错误进入常规重试判定；若变为明确的 `invalid_encrypted_content`，则结束、不换渠道 |
| reasoning 密文，实际 HTTP 为普通 502/503，但包含 compaction、服务端历史引用或不完整工具调用 | 不做有损恢复 | 按常规重试规则处理，可换渠道 |
| 明确返回 `invalid_encrypted_content`，但历史无法重建（含 compaction、服务端历史引用或不完整工具调用等） | 不做有损恢复 | 直接结束，不换渠道 |
| 只有 compaction 密文，明确返回 `invalid_encrypted_content` | 不重试，不删除 compaction | 直接结束，不换渠道 |
| 只有 compaction 密文，实际 HTTP 为普通 502/503 | 不做有损恢复 | 按常规重试规则处理，可换渠道 |
| reasoning / compaction 密文，普通 500、504 等未命中恢复条件的错误 | 不做有损恢复 | 按常规重试规则处理 |

**普通 502/503 且历史可重建时，顺序是：原始请求失败 → 有损重建 → 原渠道、原 Key 重试一次 → 若仍失败，再判定常规重试。不是直接跳到换渠道重试。**

## 有损恢复的具体操作

- 删除输入中的 reasoning 项。
- 清除保留项的顶层 `id`。
- 保留完整消息、配对的工具调用及结果；保留 `call_id` 和嵌套资源 ID。
- 不删除 compaction 来强行恢复历史。
- 包含 `previous_response_id`、`conversation` 等有效服务端历史引用时，不做有损恢复。
- 工具调用和结果不完整、不配对，或包含不支持重放的历史项时，不做有损恢复。

恢复发生在一次渠道中转尝试内部，使用同一渠道、同一 Key 和同一计费会话，每次渠道尝试最多恢复一次。普通 502/503 后续若进入新的渠道尝试，该尝试仍可按相同条件执行一次恢复。

## 重试边界与常规规则

- 已向客户端输出正式流事件、请求已取消、已有计费用量或可计费流输出，以及错误本身禁止重试时，不执行恢复重放；命中恢复分支时也保留禁止后续重试的安全限制。
- `/v1/responses/compact` 不执行此有损恢复；命中恢复分支时仍按原逻辑结束，不进入渠道轮换。
- 明确的 `invalid_encrypted_content` 可来自非 200 HTTP 错误、HTTP 200 JSON 错误，或尚未输出正式流事件时的 SSE 错误。
- 普通 502/503 恢复触发依据是上游实际 HTTP 状态。渠道状态码映射生成的 502/503，或 HTTP 200 流解析失败生成的本地 502，不因此触发有损恢复。
- “按常规规则处理”不保证一定重试或一定换渠道：仍受重试次数、状态码配置、渠道亲和性、指定渠道等规则约束。常规状态码判定使用最终错误的映射后状态码。
- 对普通 502/503，不再因为尝试过恢复或历史不可重建而设置全局禁止重试标志；符合条件时，原有“全局额度不足关键词强制重试”路径也可继续生效。
- 明确密文拒绝产生的禁止重试标志同时拦截普通重试和上述强制重试。因明确拒绝而开始的恢复，即使第二次变成普通错误或网络错误，也不再换渠道。

## 日志标签

| 标签 | 含义 |
| --- | --- |
| E1 | 本次错误映射后状态为 5xx，原始输入含非空 reasoning 密文 |
| E2 | 本次错误映射后状态为 5xx，原始输入含非空 compaction 密文 |
| R1 | 本次渠道尝试实际执行过一次 reasoning 有损恢复重试 |

E1/E2 是日志分类标签，不代表上游一定明确报告了解密失败，也不能仅凭标签判断是否换渠道。应结合错误码、实际 HTTP 状态、恢复结果和常规重试配置判断。

## 相关实现

- [恢复触发、原渠道重试和禁止重试标志](relay/responses_reasoning_recovery.go)
- [历史重建条件与请求内容处理](relay/common/responses_reasoning_recovery.go)
- [外层常规重试与强制重试](controller/relay.go)
- [日志标签判定](relay/common/responses_log_badges.go)
- [恢复边界回归测试](relay/responses_reasoning_recovery_test.go)
- [凭据保持与单次结算测试](controller/relay_responses_retry_test.go)
- [日志标签与后续重试判定测试](controller/relay_responses_log_badges_test.go)
