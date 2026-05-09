# Xiaomi Claude Tools 排查记录

## 背景

目标是把 Xiaomi 渠道做成双栈：

- OpenAI Compatible 能力继续保留。
- Claude Messages 请求走 Xiaomi 官方 Anthropic 兼容接口：
  `/anthropic/v1/messages`

用户侧主要场景来自 Claude Code / Claude VSCode 插件，请求路径为：

- `/v1/messages?beta=true`
- 模型名经映射后上游为 `mimo-v2.5-pro`
- 消费日志里可见：
  - `request_conversion:["Claude Messages"]`
  - `request_path:"/v1/messages"`
  - `usage_semantic:"anthropic"`

## 已确认的实现方向

### 双栈方案

Xiaomi 渠道不能简单替换成 Anthropic-only，需要同时保留：

- OpenAI Compatible：原有 `/v1/chat/completions`、Responses 兼容、TTS 等能力。
- Anthropic Compatible：Claude Messages 请求转发到 `/anthropic/v1/messages`。

当前代码方向：

- `relay/channel/xiaomi/adaptor.go`
  - Claude 格式请求使用 `/anthropic/v1/messages`
  - OpenAI / Responses / TTS 保持原路径
  - Claude 请求头使用 `api-key`
  - 保留 `anthropic-version`
  - 透传 Claude 常用 beta headers

### Claude tools 兼容

Xiaomi 官方示例里工具格式要求：

```json
{
  "name": "get_weather",
  "description": "...",
  "type": "custom",
  "input_schema": {}
}
```

因此当前做法：

- 普通自定义工具缺少 `type` 时补 `custom`
- 已显式带类型的工具不改写
- `web_search_20250305` 这类 Claude 内置工具保持原样透传

这意味着：

```json
{"type":"web_search_20250305","name":"web_search","max_uses":8}
```

不会被改成 `custom`，也不会被删除。

## 日志排查结论

### 旧失败现象

Claude Code 问：

```text
深圳光明的天气怎么样？
```

模型先生成：

```json
{"name":"WebSearch","input":{"query":"深圳光明区天气 2026年5月8日"}}
```

随后 Claude Code 发出 helper 请求：

```json
{
  "messages": [
    {
      "role": "user",
      "content": [
        {
          "type": "text",
          "text": "Perform a web search for the query: 深圳光明区天气 2026年5月8日"
        }
      ]
    }
  ],
  "tools": [
    {
      "type": "web_search_20250305",
      "name": "web_search",
      "max_uses": 8
    }
  ],
  "stream": true
}
```

但回填到主会话的 `tool_result` 内容不是搜索结果，而是类似：

```text
I don't have a web search tool available in this environment.
```

这说明 `web_search_20250305` 这条工具链没有真实跑通。

### 为什么有时会出现 WebFetch

在 `logs/weather-2026050802.log` 里，出现过：

1. 第一次 `WebSearch`
2. 第二次 `WebSearch`
3. 然后模型自行改走 `WebFetch`

这不是代码在同一条链路里变好了，而是 Claude Code / 模型规划差异：

- `web_search` 失败后，模型有时会继续尝试其他搜索 query。
- 如果仍失败，有时会尝试 `WebFetch` 直接抓取天气网站。
- `WebFetch` 是另一条工具路径，不代表 `web_search_20250305` 已经跑通。

### 为什么有时一轮失败就结束

在 `logs/oneapi-20260508.log` 里，出现过：

1. 主会话调用 `WebSearch`
2. helper 请求执行 `web_search_20250305`
3. helper 返回“没有 web search 工具”的兜底文本
4. 主会话直接结束，没有继续 `WebFetch`

这是模型规划差异，不是网关显式拦截后续请求。

原因是 helper 返回文本已经包含了“历史气候 + 查询建议”等可交付内容，模型可能判断足够回答用户，于是不再继续尝试。

## 当前关键未决问题

还没有最终钉死：

- Xiaomi 上游是否根本没有返回 `tool_use`
- 还是 Xiaomi 返回了 `tool_use`，但中间处理或客户端回填时丢失/改写

要回答这个问题，必须看到 Xiaomi 对 `web_search_20250305` helper 请求的原始上游 SSE chunk。

## 已加调试日志

### 目标日志

预期下一次复现时应看到：

```text
[xiaomi claude] upstream stream chunk bytes=...
[xiaomi claude] parsed stream chunk summary: ...
```

非流式时应看到：

```text
[xiaomi claude] upstream response body bytes=...
[xiaomi claude] parsed response summary: ...
```

摘要里会包含：

- `type`
- `stop_reason`
- `content_types`
- `content_names`
- `content_block_type`
- `content_block_name`
- `delta_type`
- `delta_stop_reason`
- `server_web_search_requests`

### 已发现的日志覆盖问题

第一版日志只放在 `xiaomi.Adaptor.DoResponse` 附近，实际日志证明：

- `[claude messages raw]` 有输出，说明 `DebugEnabled` 已开启。
- `[xiaomi claude]` 没输出，说明标记没有覆盖真实执行路径。

因此不能再只依赖 Xiaomi adaptor 内部标记。

### 最新修正

已把 Xiaomi Claude 调试标记前移到 `relay/claude_handler.go`：

```go
info.InitChannelMeta(c)
if info.ChannelType == constant.ChannelTypeXiaomi && info.RelayFormat == types.RelayFormatClaude {
    common.SetContextKey(c, constant.ContextKeyXiaomiClaudeDebug, true)
}
```

并在 `relay/channel/claude/relay-claude.go` 的响应处理处优先读取该标记：

```go
common.GetContextKeyBool(c, constant.ContextKeyXiaomiClaudeDebug)
```

这样只要请求进入 Claude Messages 主链路，且渠道类型为 Xiaomi，就应记录 Xiaomi Claude 原始上游响应。

## 下一轮复现检查点

复现后搜索：

```text
[xiaomi claude] upstream stream chunk
[xiaomi claude] parsed stream chunk summary
web_search_20250305
Perform a web search for the query
```

判断规则：

### 情况 A：原始 chunk 有 `tool_use`

如果看到上游原始响应含：

```json
{"type":"tool_use","name":"web_search", ...}
```

说明 Xiaomi 可能返回了工具调用，后续要查：

- `relay/channel/claude/relay-claude.go` 是否转发/解析时改坏
- Claude Code helper 对 tool_use 是否能正确执行
- tool_result 回填是否被客户端或中间层替换成兜底文本

### 情况 B：原始 chunk 没有 `tool_use`

如果原始响应只有 text / message_delta / end_turn，或直接生成：

```text
I don't have a web search tool available...
```

说明 Xiaomi 上游没有真实触发 `web_search_20250305`，当前网关侧只是正确透传了工具定义。

这时问题更偏向：

- Xiaomi 是否实际支持 Anthropic web_search built-in tool
- 是否需要额外 beta/header
- 是否只支持 `custom` function tools，不支持 Claude server tools
- 是否模型策略不稳定，看到工具但选择不用

## 已跑验证

```bash
go test ./relay/channel/xiaomi
```

结果通过。

已知无关测试失败：

```bash
go test ./relay/channel/claude
```

仍有旧的 file/document 转换相关失败，和 Xiaomi web_search 调试无关。

## 当前倾向判断

截至目前，已有事实更偏向：

- 网关已经把 `web_search_20250305` 工具定义透传到了 Xiaomi Claude 上游。
- `web_search` 没跑通，不是因为工具在请求映射阶段被删除或改成 `custom`。
- 真实原因需要通过下一轮 `[xiaomi claude] upstream stream chunk` 原始响应确认。

如果下一轮原始 chunk 显示 Xiaomi 直接输出“没有 web search tool”，基本可以判定：Xiaomi 当前 Anthropic 兼容接口不支持 Claude Code 期望的 `web_search_20250305` server tool，至少不是完整等价支持。

## 2026-05-08 12:31 最新闭环结论

本轮日志：`logs/oneapi-20260508.log`

### 关键请求拆分

1. 主会话请求：`202605080431324531363002DhuAu11`

   - 最终出站到：
     `https://api.xiaomimimo.com/anthropic/v1/messages`
   - Claude Code 的工具列表共 23 个，网关归一化为 Xiaomi 文档支持的 `custom` 工具：
     `WebSearch`、`WebFetch`、`Bash` 等。
   - Xiaomi 原始 SSE 返回：
     ```json
     {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_de4e672991cc4c0397cce085","name":"WebSearch","input":{}}}
     ```
   - 随后返回：
     ```json
     {"type":"message_delta","delta":{"stop_reason":"tool_use"}}
     ```

   结论：Xiaomi 对 `custom` 工具调用是可用的，主链路上的 Claude Code `WebSearch` 客户端工具能触发。

2. WebSearch helper 请求：`20260508043138872232200KWyLnP98`

   最终出站 body 已确认：

   ```json
   {
     "model": "mimo-v2.5-pro",
     "tools": [
       {
         "max_uses": 8,
         "name": "web_search",
         "type": "web_search_20250305"
       }
     ],
     "thinking": {"type": "adaptive"},
     "context_management": {"edits": [{"keep": "all", "type": "clear_thinking_20251015"}]},
     "output_config": {"effort": "high"},
     "stream": true
   }
   ```

   最终出站 URL/header 已确认：

   - URL：`https://api.xiaomimimo.com/anthropic/v1/messages`
   - `api-key` 存在
   - `x-api-key` 为空
   - `Authorization` 为空
   - `anthropic-version: 2023-06-01`
   - `anthropic-beta` 透传
   - `header_override=0`

   Xiaomi 原始 SSE 聚合结果：

   - `TOOL_STARTS=` 空
   - `STOPS=end_turn`
   - `INPUT_JSON_DELTA=` 空
   - `server_web_search_requests=0`
   - 文本直接输出：
     ```text
     I don't actually have a web search tool available to me in this conversation.
     ```

   结论：`web_search_20250305` 没有被网关删除、改名、改成 `custom`，也没有被 header/URL 路由错误挡住；它已经作为最终上游请求发给 Xiaomi，但 Xiaomi 没有返回 `tool_use`，而是按普通文本结束。

### 与 Xiaomi 官方文档对照

用户提供的 `xiaomi-anthropic.md` 中 `tools.type` 只列出：

```text
可选值：custom
```

并且 `tools.input_schema` 是必填。文档没有列出 Anthropic hosted/server tool：

```json
{"type":"web_search_20250305","name":"web_search"}
```

所以当前最硬结论是：

- Xiaomi 官方 Anthropic 兼容接口支持的是 `custom` 函数工具调用。
- Claude Code 主会话里的 `WebSearch` 是客户端自定义工具，被网关按 `custom` 发送后能触发。
- Claude Code helper 里的 `web_search_20250305` 是 Anthropic hosted/server web search tool，不是 Xiaomi 文档声明支持的 `custom` 工具。
- Xiaomi 目前没有执行该 hosted/server tool，至少在本轮请求和当前文档范围内不支持。

### 真凶定位

不是：

- 不是路由仍走 OpenAI Compatible。
- 不是 `/anthropic/v1/messages` 路径错误。
- 不是 `api-key` header 错误。
- 不是 `web_search_20250305` 被网关改写成 `custom`。
- 不是 `web_search_20250305` 被 `RemoveDisabledFields` 删除。
- 不是响应中返回了 `tool_use` 但被网关吃掉。

是：

- Xiaomi 上游没有把 `web_search_20250305` 当作可执行 hosted/server tool 触发，直接生成了“没有 web search tool”的普通文本并 `end_turn`。

### 下一步可选修复方向

1. 保持现状并明确标注限制：
   - Xiaomi Claude 支持 `custom` tools。
   - 不声明支持 Anthropic hosted/server tools，例如 `web_search_20250305`。

2. 网关侧做兼容降级：
   - 检测 Xiaomi Claude 请求中只有 `web_search_20250305` 这类 hosted tool 时，改写成 Xiaomi 支持的 `custom` tool 形态。
   - 难点是：Claude Code helper 期望的是 Anthropic hosted web search 的“服务端实际搜索结果”，仅改成 `custom` 只能让 Xiaomi 返回 `tool_use`，但网关本身还需要有一个真实搜索执行器，否则仍无法产出搜索结果。

3. 接入真实搜索后端：
   - 例如外部搜索 API、已有 MCP/搜索服务，或可配置搜索 provider。
   - 网关拦截 `web_search_20250305` helper 请求，自行执行搜索并返回 Claude Messages SSE/非流式结果。
   - 这是能让 Claude Code 天气/实时搜索真正跑通的方案，但需要新增搜索 provider 配置、计费、错误处理和结果格式。
