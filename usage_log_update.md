# 远程压缩、Compact 虚拟模型、使用日志与计费技术复盘

> 文档日期：2026-08-17  
> 适用范围：当前工作区分支 `C:\vs_project\new-api` 及本次对话中完成的代码检查与改造  
> 说明：本文严格区分“代码可证明的事实”“生产日志可证明的事实”“待验证假设”。

## 1. 背景与问题范围

本次讨论围绕以下问题展开：

1. Codex 远程压缩由谁提供，以及 `remote_compaction_v2` 的作用。
2. `/v1/responses/compact` 的请求、转发和响应处理流程。
3. `*-openai-compact` 为什么存在，它是不是需要单独定价、是否会被原样发送给上游。
4. 新旧计费体系如何处理 compact 虚拟模型。
5. `/v1/models` 是否应该暴露 compact 虚拟模型，以及普通接口是否允许直接请求该后缀。
6. 使用日志是否准确记录 compact 请求、工具调用和搜索附加费用。
7. 生产环境中出现 `gpt-5.6-terra-openai-compact` 无可用渠道时，现有日志能否判断客户端原始模型名。

## 2. 核心术语

### 2.1 远程压缩

远程压缩是 Codex/OpenAI 客户端在上下文接近限制时，将历史上下文提交给远程压缩端点并获得压缩结果的能力。

本项目的角色是 API 网关和转发层：

- 对外暴露 `/v1/responses/compact`；
- 根据渠道类型转发到支持 compact 的上游；
- 处理认证、路由、计费、使用日志和错误转换；
- 本项目不是 Codex 客户端本地上下文管理逻辑的提供者。

`remote_compaction_v2 = false` 属于 Codex 客户端侧开关。关闭后，客户端不会使用对应的远程压缩流程。上下文超限后的本地压缩、截断或报错行为由 Codex 客户端版本决定，不由本项目兜底。

仅仅暴露 compact 模型名或 compact 接口，不应被理解为可以覆盖客户端明确关闭远程压缩的配置。客户端是否启用远程压缩和网关是否具备转发能力是两个独立层面。

### 2.2 `/v1/responses/compact`

这是 OpenAI Responses Compaction 兼容入口。当前路由定义位于：

- `router/relay-router.go`
- 请求格式：`types.RelayFormatOpenAIResponsesCompaction`
- 中继模式：`relayconstant.RelayModeResponsesCompact`

当前实现只允许 OpenAI/Codex API 类型处理该入口，其他 API 类型会返回不支持端点错误。

### 2.3 `-openai-compact` 虚拟后缀

当前本地分支使用：

```text
-openai-compact
```

作为内部虚拟模型后缀，例如：

```text
gpt-5.4
→ gpt-5.4-openai-compact
```

相关常量位于：

```go
setting/ratio_setting/compact_suffix.go
```

该后缀主要用于区分：

- 普通 `/v1/responses` 请求；
- `/v1/responses/compact` 请求；
- 渠道能力、路由和计费策略中的 compact 特殊处理。

它不是一个应当默认原样提交给 OpenAI 模型服务的真实模型名。

## 3. 当前本地分支的 Compact 请求流程

### 3.1 分发阶段追加后缀

`middleware/distributor.go` 会先从请求体读取客户端模型名，然后在路径为 `/v1/responses/compact` 时执行：

```go
modelRequest.Model = ratio_setting.WithCompactModelSuffix(modelRequest.Model)
```

因此：

```text
客户端提交 gpt-5.4
→ 内部路由模型 gpt-5.4-openai-compact
```

`WithCompactModelSuffix` 是幂等的：

```text
客户端提交 gpt-5.4-openai-compact
→ 内部路由模型仍为 gpt-5.4-openai-compact
```

这导致仅观察后续内部模型名时，无法区分客户端到底提交了原型模型还是已经带后缀的模型。

### 3.2 模型映射和上游模型名

在非透传请求路径中，`relay/helper/model_mapped.go` 会：

1. 识别 `RelayModeResponsesCompact`；
2. 去掉 `-openai-compact` 后再执行模型映射；
3. 将最终原型/映射模型写入上游请求体；
4. 保留带 compact 后缀的内部计费模型名。

因此非透传情况下，内部模型可能是：

```text
gpt-5.4-openai-compact
```

而上游请求模型应是：

```text
gpt-5.4
```

### 3.3 请求体透传例外

当全局或渠道启用以下任一配置时：

- `PassThroughRequestEnabled`
- `PassThroughBodyEnabled`

`relay/responses_handler.go` 会直接读取并转发原始请求体。此时模型映射生成的 `UpstreamModelName` 不一定会回写到原始 JSON 请求体。

因此透传渠道必须单独审计：

- 客户端原始模型；
- 内部路由模型；
- 实际出站请求体模型。

不能仅通过 `OriginModelName` 或 `UpstreamModelName` 推断实际发送内容。

### 3.4 Compact 请求内部复用 Responses 结构

`relay/responses_handler.go` 会将 `OpenAIResponsesCompactionRequest` 转为内部通用的 `OpenAIResponsesRequest`，以复用模型映射和渠道适配逻辑。

这是内部 DTO 转换，不代表客户端请求路径被改写为 `/v1/responses`。

Compact 请求支持的字段会被选择性转入通用请求；`tools`、`reasoning`、`text` 等字段目前不会发送给 compact 上游。

## 4. 为什么曾经暴露 `*-openai-compact`

当前本地分支的 Codex 渠道模型列表会基于原型模型生成带 compact 后缀的虚拟模型。这使同一套模型能力列表同时承担了两种职责：

1. 内部渠道能力与路由标识；
2. 对外 `/v1/models` 公共模型列表。

由于 `controller/model.go` 曾直接聚合所有适配器的 `GetModelList()`，内部虚拟模型随之泄露到公共模型列表。

这也是早期设计中 NewAPI 需要“暴露” `-openai-compact` 的根本原因：内部能力模型和公共产品模型没有完全分层，而不是 OpenAI 上游真实要求客户端请求这种模型名。

## 5. 上游项目现状

本次检查时：

- `upstream/main` 最新检查到的提交为 `e2c7aa7b102c2075eae2377df3508658d45e88dc`；
- 提交时间为 2026-08-15；
- 上游提交 `bb234ff41`（2026-08-11，`refactor(responses): remove compact model suffix handling (#6770)`）已经删除 compact 模型后缀机制；
- 删除范围包括 distributor 后缀追加、Codex compact 模型列表扩展、模型映射后缀处理和旧倍率特殊处理等。

这说明上游主线已经转向“不使用 `-openai-compact` 虚拟模型”的设计。

当前本地分支与上游主线存在明显差异，本文记录的本地改造不能直接等同于上游主线行为。

## 6. 旧版计费中的 Compact 行为

旧版计费使用两个主要 Map：

```text
ModelPrice
ModelRatio
```

当前本地代码对 compact 模型做特殊处理：

- 固定价格只查 `*-openai-compact`；
- 模型倍率优先查 `*-openai-compact`；
- 缺少配置时不会自动继承原型模型价格。

重要语义：

```json
{
  "*-openai-compact": 1
}
```

表示“所有 compact 模型使用绝对倍率 1”，而不是“原型模型价格 × 1”。

如果原型模型倍率为 `7.5`，配置 compact 倍率 `1` 不会得到与原型同价的效果。

## 7. 新版计费策略中的 Compact 行为

### 7.1 新版策略的通配符能力

新版策略存储于：

```text
ModelBillingPolicy
```

策略解析器支持：

1. 精确模型名；
2. `*` 通配符；
3. 多个通配符命中时选择更具体的键。

因此后端能够接受：

```text
*-openai-compact
gpt-5-*-openai-compact
gpt-5.4-openai-compact
```

但原有前端计费策略管理器只允许编辑已经存在的策略，没有新增任意模型键/通配符策略的完整入口。

“未定价模型”页面也只通过 `policies[name]` 做精确判断，不能识别某模型是否已经被通配符覆盖。

### 7.2 直接修改数据库的风险

直接更新 `options.ModelBillingPolicy` 技术上会被后台定时同步加载，默认同步周期由 `SYNC_FREQUENCY` 控制，默认值为 60 秒。

但不建议直接写数据库，因为会绕过：

- 策略校验；
- revision 递增；
- 内存立即更新；
- 价格缓存刷新；
- 并发修改冲突保护；
- API 的原子持久化流程。

新版策略激活后，修改 `ModelPrice` 或 `ModelRatio` 不能替代 `ModelBillingPolicy`。

### 7.3 已落地：Compact 计费原型回退

本次已修改 `setting/billing_policy/policy.go`，compact 计费策略解析顺序现在是：

1. 精确 compact 策略，例如 `gpt-5.4-openai-compact`；
2. compact 专用通配符，例如 `*-openai-compact`；
3. 去掉后缀后查原型模型，例如 `gpt-5.4`；
4. 使用原型模型可命中的普通通配符策略；
5. 最后保留其他兼容通配符行为。

因此，当以下 compact 策略都不存在时：

```text
gpt-5.4-openai-compact
*-openai-compact
```

会自动使用：

```text
gpt-5.4
```

的新版计费策略。

该回退只解决计费策略解析，不等价于渠道选择回退。

## 8. 已落地：公共模型列表和普通端点限制

### 8.1 `/v1/models` 隐藏虚拟模型

本次已修改 `controller/model.go`：

- 公共模型列表过滤所有以 `-openai-compact` 结尾的模型；
- `/v1/models/:model` 查询 compact 虚拟模型时返回 `model_not_found`；
- 内部渠道模型列表没有被删除，避免直接破坏既有渠道能力配置；
- 同时修复过滤后 Anthropic 模型列表为空时访问首尾元素可能发生的越界问题。

### 8.2 普通端点禁止直接请求后缀

本次已修改 `middleware/distributor.go`：

以下接口如果客户端直接提交 `*-openai-compact`，返回 HTTP 400：

```text
/v1/responses
/v1/chat/completions
```

允许的 compact 调用方式仍然是：

```http
POST /v1/responses/compact
```

并提交原型模型名。

注意：当前实现尚未禁止客户端在 `/v1/responses/compact` 中直接提交带后缀模型。这是后续生产治理需要考虑的重点。

## 9. Compact 响应读取错误处理

本次工作区还包含以下 compact 响应改造：

- `relay/channel/openai/relay_responses_compact.go`
- `relay/channel/openai/relay_responses_test.go`

当上游返回截断响应或读取响应体失败时：

- 记录渠道、API 类型、模型、上游状态码、已读取字节数和错误；
- 对客户端返回 HTTP 502 Bad Gateway；
- 不再将上游响应体读取失败包装为本项目内部 HTTP 500。

该改造用于更准确表达“上游流中断/响应体不完整”，与远程压缩失败日志中的 `stream disconnected before completion` 问题相关。

## 10. 使用日志中的 Compact 请求

### 10.1 请求路径

消费日志生成时，`service/log_info_generate.go` 优先读取：

```go
ctx.Request.URL.Path
```

并写入：

```json
{
  "request_path": "/v1/responses/compact"
}
```

因此当前代码不会有意把 compact 的原始请求路径写成 `/v1/responses`。

前端主列表不会突出显示请求路径；用户需要展开日志查看“请求路径”。“请求转换”字段描述的是协议/DTO 转换链，不等于原始 HTTP 路径，也不等于模型名未经过处理。

### 10.2 模型名审计缺口

当前日志中的 `model_name` 主要是内部计费/路由模型名。对于 compact 请求，它可能已经带有 `-openai-compact`。

目前没有同时记录以下三个字段：

```text
client_model   客户端原始请求体中的模型
routing_model  distributor 用于路由的模型
upstream_model 实际出站请求体中的模型
```

因此仅凭使用日志中的 `model_name`，无法判断客户端原始输入。

## 11. 生产事故日志的证据边界

生产环境捕获到：

```text
外层 Request ID：2026081709371674100445Fk7IU7tQ
渠道：131
模型：gpt-5.6-terra-openai-compact
请求路径：/v1/responses/compact
请求转换：原生格式
```

错误内容中包含：

```text
status_code=503, 分组 【GPT】codexPro分组兜底 下模型
gpt-5.6-terra-openai-compact 无可用渠道（distributor）
(request id: 202608170937203397949938268d9d63Jkaff3S)
```

已知信息：

- 内嵌 Request ID `202608170937203397949938268d9d63Jkaff3S` 属于渠道 131 的上游；
- 外层使用日志和上游错误均出现 `gpt-5.6-terra-openai-compact`；
- 外层请求路径是 `/v1/responses/compact`。

不能从该日志证明：

- 客户端提交的是 `gpt-5.6-terra`；
- 客户端提交的是 `gpt-5.6-terra-openai-compact`；
- 上游是否追加了后缀；
- 上游是否使用该模型名进行渠道选择；
- 上游错误中的模型是原始模型、转换模型还是路由模型。

原因是渠道 131 的上游实现和请求处理代码未知，不能用本项目代码替代上游代码进行推断。

当前生产排障的优先假设是：

> 用户可能直接向 `/v1/responses/compact` 提交了内部虚拟模型名 `gpt-5.6-terra-openai-compact`，该名称经过透传或其他路径到达上游，最终导致上游无法处理。

该假设具有操作价值，但现有日志不能证实或证伪。

要形成完整证据链，必须获得以下至少一项：

1. 客户端原始请求体；
2. 外层网关进入 distributor 前解析出的模型；
3. 渠道 131 实际出站请求体；
4. 上游网关入口抓取的原始请求体；
5. 新增 `client_model`、`routing_model`、`upstream_model` 审计字段后的日志。

## 12. 工具调用和搜索费用

### 12.1 哪些调用会产生独立费用

当前代码识别并可能独立计费的项目包括：

- OpenAI Web Search；
- Claude Web Search；
- File Search；
- Image Generation Call；
- 部分音频输入费用。

普通客户端函数调用，例如模型返回的 `function_call` 或 `tool_calls`，不会仅因为发生一次函数调用就单独收费。它们通常通过输入/输出 Token 计费。

只有系统识别的托管内置工具调用才会按调用次数或工具单价额外收费。

### 12.2 旧版计费

旧版结算会将工具费用作为独立 quota 加入模型 Token/按次费用：

```text
模型 Token/按次费用
+ Web Search
+ Claude Web Search
+ File Search
+ Audio Input
+ Image Generation Call
```

这些费用不会被转换成 Prompt/Completion Token 数，而是直接加入最终 `Quota`。

### 12.3 新版计费策略

新版策略通过 `BillingUsage.ToolUsage` 将工具调用传给 `CalculateBilling`。

工具价格来自策略的 `tools` 字段。策略校验阶段会补齐默认工具价格，例如：

```text
web_search.standard
web_search.premium
claude_web_search
file_search
image_generation.<quality>.<size>
```

工具费用会生成 `BillingCalculation.LineItems`，计入：

```text
SubtotalUSD
TotalUSD
最终 summary.Quota
```

因此只要工具调用被正确识别，其费用会真实扣除。

### 12.4 使用日志展示缺口

虽然总扣费包含工具费用，但当前新版计费日志的展示存在明显缺口：

1. 后端已经实现 `buildBillingPolicyAdditionalCharges`；
2. 该函数能够生成 Web Search、File Search、Image Generation 等结构化附加费用；
3. 实际写入 `billing_policy` 快照时却固定设置：

```go
AdditionalCharges:    nil
AdditionalChargesUSD: "0"
```

4. `buildBillingPolicyAdditionalCharges` 当前只有测试调用，没有接入实际日志写入链路；
5. 前端详细计费组件主要按 Token 项展示 `line_items`，对工具项的 `units` 和 `unit_price` 没有完整处理；
6. 前端紧凑摘要只识别 Token、缓存、图片输入等字段，会跳过工具费用字段。

因此当前真实行为是：

```text
总花费：包含已识别工具费
Token 数量：不包含工具费
工具费用明细：可能缺失、被跳过或显示公式不准确
```

这更像未完成的日志展示实现，而不是有意隐藏费用。

### 12.5 工具调用识别风险

工具扣费依赖上游返回标准、可识别的调用信息。

例如 Responses 流式响应通过以下事件累计调用次数：

```text
web_search_call
file_search_call
```

如果兼容上游：

- 不返回标准事件；
- 使用不同工具名称；
- 转换过程中丢失工具类型；
- 请求中未注册对应内置工具；

系统可能无法识别调用次数，从而无法计入对应费用。

## 13. 已执行的测试

本次改造已运行：

```text
go test ./setting/billing_policy -count=1
go test ./middleware ./controller ./setting/billing_policy -count=1
```

相关包测试通过。

新增或扩展的测试覆盖：

- compact 计费精确策略优先；
- compact 通配符策略优先；
- compact 回退原型模型策略；
- 原型模型普通通配符回退；
- `/v1/models` compact 虚拟模型过滤；
- `/v1/responses` 和 `/v1/chat/completions` 拒绝后缀模型；
- `/v1/responses/compact` 不被普通端点限制误伤；
- compact 上游响应体截断映射为 HTTP 502。

## 14. 当前未解决的问题

### P0：记录客户端原始模型和实际出站模型

必须增加可审计字段：

```json
{
  "client_model": "gpt-5.6-terra",
  "routing_model": "gpt-5.6-terra-openai-compact",
  "upstream_model": "gpt-5.6-terra"
}
```

其中 `upstream_model` 最好从最终序列化后的出站请求体提取或在序列化时冻结，而不是只读取内存元数据。

### P0：决定是否禁止 Compact 入口提交后缀模型

当前只禁止普通接口直接请求 `*-openai-compact`，但 `/v1/responses/compact` 仍接受带后缀模型。

建议评估改为：

```text
/v1/responses/compact 只接受原型模型名
```

客户端直接提交 `*-openai-compact` 时返回 HTTP 400，从入口消除歧义和透传风险。

### P0：修复工具费用日志明细

建议：

1. 在 `attachBillingPolicySnapshot` 中接入实际工具费用结构；
2. 明确避免与 `calculation.line_items` 重复计费或重复展示；
3. 前端按 `units`、`unit_price`、`unit` 展示工具费用；
4. 主列表提供“含附加费”标记；
5. 增加“日志总额 = Token 项 + 工具项 × 倍率”的一致性测试。

### P1：渠道选择和计费模型解耦

当前本地设计将 compact 虚拟模型同时用于：

- 渠道选择；
- 计费；
- 日志；
- 模型能力标识。

建议长期拆分为独立字段：

```text
client_model
capability/operation = responses_compact
routing_model
billing_model
upstream_model
```

避免继续依赖修改模型名表达端点能力。

### P1：透传渠道增加出站审计

透传模式可能绕开内存模型映射结果。建议在 Debug/Audit 模式下记录：

- 请求路径；
- 客户端模型；
- 最终出站模型；
- 是否透传；
- 是否发生模型映射；
- 渠道 ID；
- 上游 Request ID。

记录时不得包含完整提示词、密钥和敏感请求体。

## 15. MySQL 排查示例

查询 compact 使用日志：

```sql
SELECT
    id,
    request_id,
    channel_id,
    model_name,
    quota,
    JSON_UNQUOTE(JSON_EXTRACT(other, '$.request_path')) AS request_path,
    JSON_EXTRACT(other, '$.request_conversion') AS request_conversion,
    JSON_EXTRACT(other, '$.billing_policy') AS billing_policy,
    content,
    created_at
FROM logs
WHERE type = 2
  AND (
      model_name LIKE '%-openai-compact'
      OR JSON_UNQUOTE(JSON_EXTRACT(other, '$.request_path')) = '/v1/responses/compact'
  )
ORDER BY id DESC
LIMIT 100;
```

查询工具费用相关字段：

```sql
SELECT
    id,
    request_id,
    model_name,
    quota,
    JSON_EXTRACT(other, '$.web_search_call_count') AS web_search_calls,
    JSON_EXTRACT(other, '$.web_search_price') AS web_search_price,
    JSON_EXTRACT(other, '$.file_search_call_count') AS file_search_calls,
    JSON_EXTRACT(other, '$.file_search_price') AS file_search_price,
    JSON_EXTRACT(other, '$.image_generation_call_price') AS image_generation_price,
    JSON_EXTRACT(other, '$.billing_policy.calculation.line_items') AS line_items,
    JSON_EXTRACT(other, '$.billing_policy.actual_quota') AS actual_quota,
    created_at
FROM logs
WHERE type = 2
ORDER BY id DESC
LIMIT 100;
```

## 16. 总结

1. `/v1/responses/compact` 是端点能力，`-openai-compact` 是当前本地分支用于表达该能力的内部虚拟模型后缀。
2. 虚拟后缀不应作为公共模型暴露，也不应允许普通 Responses/Chat 接口直接请求；相关限制已落地。
3. 新版计费已经支持 compact 精确策略、compact 通配符和原型模型回退；无需为每个 compact 模型复制价格。
4. 计费回退只解决定价，不解决渠道能力、透传和原始模型审计问题。
5. 使用日志能记录真实 HTTP 请求路径，但当前不能区分客户端模型、路由模型和实际出站模型。
6. 对生产事故中的 `gpt-5.6-terra-openai-compact`，现有证据不能证明或否定用户是否直接提交了后缀模型；用户直接提交后缀是当前优先排查假设。
7. 已识别的工具调用费用会真实计入总扣费，但新版使用日志的附加费用明细展示尚未完整接通，必须单独修复。
8. 长期方案应删除“通过修改模型名表达 compact 能力”的耦合，改为显式的端点能力、路由模型、计费模型和上游模型字段。
