# 第四批：Gemini 客户端接入 Responses 上游

本次将上游 `acb347a197` 中 Gemini → Responses 高级自定义渠道能力适配到本地架构，基准仍为 `7d8b91268d`。沿用本地 DTO、计费策略及 Semi UI，不引入 relaykit/tokenkit 整仓迁移。

## 使用方法

1. 新建渠道，类型选择 **Advanced Custom (Gemini → Responses)**。
2. 填写上游 API 地址和密钥。默认请求路径 `/v1/responses`，默认请求头为 `Authorization: Bearer {key}`。
   无需鉴权的上游可填写占位渠道密钥，并选择“无需鉴权”；该值不会发送给上游。
3. 添加客户端使用的模型名称，并通过现有模型映射配置实际 Responses 模型。例如客户端模型 `gemini-proxy` 映射到上游支持的模型。
4. 路由编辑器默认包含 Gemini `v1beta` 和 `v1` 入口。`generateContent` 路由自动兼容 `streamGenerateContent`，客户端查询参数及客户端密钥不会复制到上游 URL。
5. 可分别修改每条路由的上游路径和鉴权：请求头、查询参数或无需鉴权。路径支持绝对 HTTP(S) 地址；相对路径会追加到渠道 API 地址后，支持网关前缀。
6. 选择转换损失策略：默认 `allow` 允许转换并记录诊断；`safe` 拒绝改变工具执行语义的转换；`strict` 拒绝所有已识别的有损转换。

配置保存到已有 `settings.advanced_custom`，不需要数据库迁移。可用单一路由配置示例：

```json
{
  "advanced_custom": {
    "advanced_routes": [
      {
        "incoming_path": "/v1beta/models/{model}:generateContent",
        "upstream_path": "/v1/responses",
        "converter": "gemini_generate_content_to_openai_responses",
        "auth": {
          "type": "header",
          "name": "Authorization",
          "value": "Bearer {key}"
        }
      }
    ]
  },
  "tool_loss_policy": "safe"
}
```

渠道类型为 **59**。上游原编号 58 在本项目已经属于 Xiaomi MiMo，因此没有复用或改写既有编号。该渠道只注册本次所需的 Gemini → Responses 转换，不接受其他上游 converter ID。普通 Chat/Responses 请求会在渠道选择阶段排除此渠道；数据库路由、内存缓存、重试及现有亲和性检查均复用同一路由筛选。

## 已实现行为

- 请求：system instruction、消息历史、显式零值、函数定义/调用/返回、无调用 ID 时的顺序配对、强制工具选择、JSON Schema、思考参数和 Google Search → Responses web_search。
- Schema：Gemini typed Schema 的大写类型和 nullable 转换为 JSON Schema；`parametersJsonSchema` / `responseJsonSchema` 保留完整内容；不可转换的 propertyOrdering 记录诊断。
- 媒体：图片、PDF、UTF-8 文本及工具返回媒体；外部文件通过现有受保护的文件加载器读取。供应商私有文件 URI、音频/视频和不支持的托管工具按损失策略处理。
- 返回：非流式 JSON 与 Gemini SSE；正文和函数调用同时存在时都保留；完整函数参数重建、思考摘要、缓存/思考 token、搜索引用及 UTF-8 引用位置。
- 流式终态：completed → STOP；max_output_tokens 截断 → MAX_TOKENS；其他 incomplete → SAFETY。坏帧、上游失败或缺少终态不会伪造成功结束。
- 即使开启全局或渠道请求体透传，该转换渠道仍将 Gemini 请求转换为 Responses JSON。
- 管理页可配置、保存、重新加载路由和鉴权；“测试渠道”使用 Gemini 请求和已配置入口。

Responses 使用 `stream: true` 控制流式，不使用 Chat 的 `stream_options.include_usage`，因此没有将该渠道加入 Chat StreamOptions 支持列表。

## 计费与失败处理

- 以 Responses 原始 usage 作为账单来源，客户端 Gemini usage 倍率仅影响返回边界。
- 缺失 usage 时估算已收集的文本、思考及函数参数；显式零 usage 保留零值，不按缺失用量保留预扣费。
- 已发送内容后发生失败或断连：结算已有用量、禁止再次重试，退款入口遵守 BillingSession 的幂等性。
- 未发送内容前失败、非法请求或 safe/strict 拒绝：退还预扣费。
- 按实际完成的 `web_search_call` 去重计数，使用 Responses 搜索价格及对应工具策略键，避免误用 Google grounding 价格。搜索价格分档使用实际上游模型，文本定价身份仍保持客户端原模型。
- 异常超大 usage 饱和到现有额度边界并保留 `QuotaClamp`，不允许整数溢出变成负数计费。

## 验证与边界

使用本地模拟 HTTP 上游、临时 SQLite 和单元测试验证 JSON/SSE、partial failure、断连、显式零用量、强制转换、退款、搜索去重、Schema/媒体、渠道编号及路由选择。前端配置 helper 测试、i18n lint 和生产构建通过。

检查结果：

- 新渠道、DTO、service、controller、middleware、model、relay、relay/common、relay/helper，以及既有 Gemini、Vertex、OpenAI、Claude 包回归通过；最终计费/零用量修正后复跑新渠道、service、controller、relay/common。
- `go test -p 2 ./... -run '^$'` 全仓编译通过；这是编译检查，不代表全仓用例全部运行。
- `bun test src/helpers/advancedCustomSettings.test.js`：2 项通过。
- `bun run i18n:lint`：通过。
- `bun run build`：通过；构建有已有的 Browserslist 数据过期提示。
- `gofmt`、`git diff --check`：通过。

真实 Responses 供应商联调未执行。不同上游模型对于工具、多模态、reasoning effort 和结构化输出仍有自身限制。完整上游高级自定义框架中的其他协议转换器、模型正则路由、模型发现/余额管理路由不属于这次 Gemini → Responses 移植范围。

本次没有改写已有渠道和价格数据，也没有重启正在运行的服务。使用新渠道前需按本项目现有方式构建并启动更新后的后端及前端。
