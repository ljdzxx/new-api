# Gemini 2026 年 11 月上游同步

本次按 QuantumNous/new-api 上游的 8 个提交移植行为，适配本项目的 `dto`、`relay`、`service` 和 Semi UI。基线为 `4c00989db0`。不引入 relaykit 或插件体系，不包含 Gemini Omni 视频生成。

## 提交对应关系

| 上游提交 | 本地移植 |
| --- | --- |
| `52c173038e` / #8177 | 请求、候选响应保留 continuationToken；part 和 generationConfig 保留 mediaProcessing，支持蛇形别名；CONTINUATION 转换为 Chat length／Responses max_output_tokens；Argon 思考映射及默认等级记录。 |
| `ba5f736779` / #8182 | 原生单条、批量及 OpenAI 兼容 Embedding 优先采用上游 usageMetadata；缺少有效用量时保留估算回退。 |
| `94b5216645` / #8179 | 内置 Vertex 渠道新增 vertex_api_version 配置，支持 v1／v1beta1；包含后端校验、渠道编辑表单、保存回填及七种语言翻译。仅影响 Google 模型 URL。 |
| `a5a4659266` | 按渠道和 Key 槽缓存服务账号 access token，提前五分钟失效；并发共享交换结果，错误不缓存；密钥标识或密钥内容变化后重新获取。使用本地 common JSON 封装。 |
| `e8c6a5d953` / #8136 | 连续文本 part 原样拼接，保留独立换行；媒体及代码块保留分隔；引用使用实际渲染位置及流式片段位置，避免重复文本或代码内同名文本导致引用偏移。同步到本项目独立的 Responses 输出路径。 |
| `669d774976` / #8367 | 支持 Gemini 4 Hydrogen 的 thinkingLevel 及跨协议 effort；中间版本的 max 直传规则由下一项替代。 |
| `a85f7e844d` / #8412 | Gemini 4 全家族默认规则：minimal→low；Hydrogen／Neon 的 xhigh、max→xhigh；其他 Gemini 4 的 xhigh、max→high。 |
| `c53dcf3356` / #8333 | Chat 用量转换到 Gemini 时填充缓存读取 Token；支持 prompt_tokens_details 和 input_tokens_details；非正值回退为零；仅含 usage 的流式末帧也会输出。 |

## 本项目的优先级和计费边界

1. Gemini 4 内置等级映射先执行，然后匹配渠道的模型／等级规则。例：Argon 客户端 max 先归一化为 high，再匹配 `gemini-4-argon,high`。
2. 渠道规则明确指定的最终等级最高优先，转换后的 Gemini 请求不会再将该值降级。显式指定值与客户端相同时仍保留覆盖意图，不能当成无需处理的恒等映射。透传请求也写入最终等级并清除冲突的 thinkingBudget，保留其他扩展字段。
3. 保留单跳映射、条件规则、输入 Token 阈值、重试渠道隔离。ClientReasoningEffort 保存原始客户端值，ModelMappingCandidateEffort 记录归一化后参与匹配的值，原始模型的计价身份保留。
4. Embedding 上游用量保持原始值，供账单、阈值重评估和结算使用。全局、渠道、用户三级缩放沿用现有逻辑；客户端响应用量在出口单独缩放，避免将已缩放用量再次送入计费。
5. 原生 Embedding 的 promptTokenDetails（单数）也参加客户端显示缩放。缓存读取 Token 继续属于输入 Token 的组成部分，不额外累加到输入总量。
6. 本次不修改数据库结构、价格策略、配额转换或预扣／结算算法。

## 回归验证

- DTO：Argon 字段、蛇形别名、零值和 false 保留。
- Gemini：原生、Chat、Responses、Claude 入口的 Gemini 4 等级矩阵；默认映射先于渠道规则匹配；显式渠道覆盖、透传、重试、阈值；Embedding 原始用量和三级缩放阈值；文本分片、换行、重复文本、代码／媒体引用、延迟流式引用、CONTINUATION。
- Vertex：两种鉴权的 v1／v1beta1 路径、流式 URL、已带版本的 base URL；缓存复用、到期刷新、Key 槽隔离、密钥更换、并发交换、失败与短有效期不缓存。
- Service：缓存用量转换和出口缩放；现有价格／配额计算及 SQLite 定价端到端测试。
- 前端：Vertex 配置保存／回填测试通过（4 项测试、15 项断言），变更文件的 Prettier 格式检查通过；`bun run build` 生产构建通过。

上述相关后端回归测试通过；Vertex 缓存竞态检查 `go test -race -p 1 ./relay/channel/vertex -run 'Test(GetAccessToken|VertexToken)'` 通过。`git diff --check` 通过。

初次全量 `go test ./service` 中，`TestUpstreamUsageSettlementAndRefund` 和 `TestTaskUpstreamUsageMissingRefundsTokenPricingOnly` 在 SQLite AutoMigrate 阶段报 `invalid DDL, unbalanced brackets`；修改前基线 `4c00989db0` 也复现相同两项失败。随后已按用户要求修复：`InitDB()` 现在对主、从节点均执行迁移，测试初始化仍额外调用通用 `AutoMigrate`，绕过已有 SQLite 专用迁移，重复重建 `users` 表并触发驱动的 DDL 解析错误。移除测试中重复的迁移调用，提前注册全局状态恢复；保留两个测试及全部计费、退款断言。修复后 `go test ./service -count=1` 全量通过（5.880 秒）。

没有启动前后端应用服务，没有执行生产操作或真实上游 API 探测。
