# API Summary

本文档基于当前代码实现整理，聚焦项目对外暴露的 relay API，不包含后台管理用的 `/api/*` CRUD 接口。

参考代码：
- 路由入口：[router/relay-router.go](/e:/go_project/new-api/router/relay-router.go:13)
- 视频路由：[router/video-router.go](/e:/go_project/new-api/router/video-router.go:10)
- 请求校验：[relay/helper/valid_request.go](/e:/go_project/new-api/relay/helper/valid_request.go:20)
- OpenAI 兼容 DTO：[dto/openai_request.go](/e:/go_project/new-api/dto/openai_request.go:29)
- Claude 兼容 DTO：[dto/claude.go](/e:/go_project/new-api/dto/claude.go:192)

## 1. Authentication

支持以下认证方式：

- 标准 Bearer Token
  - Header: `Authorization: Bearer sk-xxx`
- Anthropic 兼容
  - Header: `x-api-key: sk-xxx`
  - 常见同时携带：`anthropic-version: 2023-06-01`
- Gemini 兼容
  - Header: `x-goog-api-key: xxx`
  - 或 query: `?key=xxx`
- Realtime WebSocket
  - Header: `Sec-WebSocket-Protocol: realtime, openai-insecure-api-key.sk-xxx, openai-beta.realtime-v1`

认证逻辑见：[middleware/auth.go](/e:/go_project/new-api/middleware/auth.go:248)

## 2. Exposed API List

### 2.1 Model APIs

- `GET /v1/models`
- `GET /v1/models/:model`
- `GET /v1beta/models`
- `GET /v1beta/openai/models`

说明：
- `/v1/models` 会根据请求头自动识别 OpenAI / Anthropic / Gemini 风格。

### 2.2 Text APIs

- `POST /v1/completions`
- `POST /v1/chat/completions`
- `POST /v1/responses`
- `POST /v1/responses/compact`
- `POST /v1/messages`

### 2.3 Embedding / Moderation / Rerank

- `POST /v1/embeddings`
- `POST /v1/moderations`
- `POST /v1/rerank`

### 2.4 Image APIs

- `POST /v1/images/generations`
- `POST /v1/images/edits`
- `POST /v1/edits`

未实现：
- `POST /v1/images/variations`

### 2.5 Audio APIs

- `POST /v1/audio/speech`
- `POST /v1/audio/transcriptions`
- `POST /v1/audio/translations`

### 2.6 Realtime API

- `GET /v1/realtime`

### 2.7 Video APIs

- `POST /v1/video/generations`
- `GET /v1/video/generations/:task_id`
- `POST /v1/videos`
- `GET /v1/videos/:task_id`
- `POST /v1/videos/:video_id/remix`
- `GET /v1/videos/:task_id/content`
- `POST /kling/v1/videos/text2video`
- `POST /kling/v1/videos/image2video`
- `GET /kling/v1/videos/text2video/:task_id`
- `GET /kling/v1/videos/image2video/:task_id`

### 2.8 Billing Compatibility APIs

- `GET /v1/dashboard/billing/subscription`
- `GET /v1/dashboard/billing/usage`

## 3. Common Error Format

OpenAI 兼容错误：

```json
{
  "error": {
    "message": "error message",
    "type": "invalid_request_error",
    "param": "",
    "code": ""
  }
}
```

Anthropic 兼容错误：

```json
{
  "type": "error",
  "error": {
    "type": "invalid_request_error",
    "message": "error message"
  }
}
```

## 4. OpenAI Compatible Text APIs

DTO 结构见：[dto/openai_request.go](/e:/go_project/new-api/dto/openai_request.go:29)

### 4.1 `POST /v1/completions`

用途：
- OpenAI Completions 兼容接口

必填字段：
- `model: string`
- `prompt: string | array`

常用可选字段：
- `suffix: any`
- `stream: boolean`
- `stream_options.include_usage: boolean`
- `max_tokens: integer`
- `temperature: number`
- `top_p: number`
- `top_k: integer`
- `stop: string | string[]`
- `n: integer`
- `seed: number`
- `logprobs: boolean`
- `top_logprobs: integer`
- `user: any`
- `metadata: any`

示例：

```json
{
  "model": "gpt-3.5-turbo-instruct",
  "prompt": "Write a short slogan for an AI gateway.",
  "max_tokens": 64,
  "temperature": 0.7,
  "stream": false
}
```

### 4.2 `POST /v1/chat/completions`

用途：
- OpenAI Chat Completions 兼容接口

必填字段：
- `model: string`
- `messages: Message[]`

例外：
- 如果走 FIM 模式，允许不传 `messages`，改传 `prefix`/`suffix`

常用可选字段：
- `stream: boolean`
- `stream_options: object`
- `max_tokens: integer`
- `max_completion_tokens: integer`
- `reasoning_effort: string`
- `verbosity: any`
- `temperature: number`
- `top_p: number`
- `top_k: integer`
- `stop: any`
- `n: integer`
- `tools: ToolCallRequest[]`
- `tool_choice: any`
- `parallel_tool_calls: boolean`
- `response_format: { type, json_schema }`
- `frequency_penalty: number`
- `presence_penalty: number`
- `seed: number`
- `service_tier: any`
- `modalities: any`
- `audio: any`
- `web_search_options: object`
- `metadata: any`
- `store: any`
- `prompt_cache_key: string`
- `prompt_cache_retention: any`
- `search_parameters: any`
- `reasoning: any`
- `extra_body: any`

`Message` 结构见：[dto/openai_request.go](/e:/go_project/new-api/dto/openai_request.go:305)

字段：
- `role: string`
- `content: string | MediaContent[]`
- `name?: string`
- `tool_calls?: any`
- `tool_call_id?: string`
- `reasoning_content?: string`
- `reasoning?: string`

`MediaContent` 支持类型：
- `text`
- `image_url`
- `input_audio`
- `file`
- `video_url`

示例：

```json
{
  "model": "gpt-4o-mini",
  "messages": [
    {
      "role": "system",
      "content": "You are a helpful assistant."
    },
    {
      "role": "user",
      "content": [
        {
          "type": "text",
          "text": "Describe this image"
        },
        {
          "type": "image_url",
          "image_url": {
            "url": "https://example.com/cat.png",
            "detail": "high"
          }
        }
      ]
    }
  ],
  "stream": true,
  "stream_options": {
    "include_usage": true
  },
  "max_tokens": 1024,
  "temperature": 0.7
}
```

### 4.3 `POST /v1/responses`

用途：
- OpenAI Responses 兼容接口

DTO 结构见：[dto/openai_request.go](/e:/go_project/new-api/dto/openai_request.go:814)

必填字段：
- `model: string`
- `input: string | Input[]`

常用可选字段：
- `include: any`
- `conversation: any`
- `context_management: any`
- `instructions: any`
- `max_output_tokens: integer`
- `top_logprobs: integer`
- `metadata: any`
- `parallel_tool_calls: any`
- `previous_response_id: string`
- `reasoning: { effort, summary }`
- `service_tier: string`
- `store: any`
- `prompt_cache_key: any`
- `prompt_cache_retention: any`
- `safety_identifier: any`
- `stream: boolean`
- `stream_options: object`
- `temperature: number`
- `text: any`
- `tool_choice: any`
- `tools: any`
- `top_p: number`
- `truncation: any`
- `user: any`
- `max_tool_calls: integer`
- `prompt: any`

`input` 支持的 content item 类型：
- `input_text`
- `input_image`
- `input_file`

示例：

```json
{
  "model": "gpt-4.1",
  "input": [
    {
      "role": "user",
      "content": [
        {
          "type": "input_text",
          "text": "Summarize this file"
        },
        {
          "type": "input_file",
          "file_url": "https://example.com/report.pdf"
        }
      ]
    }
  ],
  "instructions": "Answer in Chinese",
  "max_output_tokens": 1024,
  "stream": false
}
```

### 4.4 `POST /v1/responses/compact`

用途：
- 项目自带的 compact/compaction 接口

DTO 结构见：[dto/openai_responses_compaction_request.go](/e:/go_project/new-api/dto/openai_responses_compaction_request.go:12)

必填字段：
- `model: string`

可选字段：
- `input: any`
- `instructions: any`
- `previous_response_id: string`

示例：

```json
{
  "model": "gpt-4.1",
  "input": "Please compact the prior context",
  "previous_response_id": "resp_123"
}
```

## 5. Anthropic Compatible API

### 5.1 `POST /v1/messages`

用途：
- Claude Messages 兼容接口

DTO 结构见：[dto/claude.go](/e:/go_project/new-api/dto/claude.go:192)

必填字段：
- `model: string`
- `messages: ClaudeMessage[]`

常用可选字段：
- `prompt: string`
- `system: string | ClaudeMediaMessage[]`
- `inference_geo: string`
- `max_tokens: integer`
- `max_tokens_to_sample: integer`
- `stop_sequences: string[]`
- `temperature: number`
- `top_p: number`
- `top_k: integer`
- `stream: boolean`
- `tools: any`
- `tool_choice: object`
- `thinking: object`
- `metadata: any`
- `service_tier: string`
- `context_management: any`
- `output_config: any`
- `output_format: any`
- `container: any`
- `mcp_servers: any`

`ClaudeMessage` 结构见：[dto/claude.go](/e:/go_project/new-api/dto/claude.go:108)

字段：
- `role: string`
- `content: string | ClaudeMediaMessage[]`

`ClaudeMediaMessage` 常见类型：
- `text`
- `image`
- `tool_use`
- `tool_result`

示例：

```json
{
  "model": "claude-3-7-sonnet",
  "system": "You are a helpful assistant.",
  "messages": [
    {
      "role": "user",
      "content": "Hello"
    }
  ],
  "max_tokens": 1024,
  "stream": true
}
```

## 6. Embeddings / Moderations / Rerank

### 6.1 `POST /v1/embeddings`

DTO 结构见：[dto/embedding.go](/e:/go_project/new-api/dto/embedding.go:22)

必填字段：
- `model: string`
- `input: string | string[]`

可选字段：
- `encoding_format: string`
- `dimensions: integer`
- `user: string`
- `seed: number`
- `temperature: number`
- `top_p: number`
- `frequency_penalty: number`
- `presence_penalty: number`

示例：

```json
{
  "model": "text-embedding-3-small",
  "input": [
    "hello",
    "world"
  ],
  "dimensions": 1024
}
```

### 6.2 `POST /v1/moderations`

说明：
- 走 OpenAI 兼容文本请求体
- `input` 必填
- 如果未传 `model`，默认补为 `omni-moderation-latest`

示例：

```json
{
  "input": "test moderation input"
}
```

### 6.3 `POST /v1/rerank`

DTO 结构见：[dto/rerank.go](/e:/go_project/new-api/dto/rerank.go:11)

必填字段：
- `model: string`
- `query: string`
- `documents: any[]`

可选字段：
- `top_n: integer`
- `return_documents: boolean`
- `max_chunk_per_doc: integer`
- `overlap_tokens: integer`

示例：

```json
{
  "model": "rerank-v1",
  "query": "What is a proxy gateway?",
  "documents": [
    "Document A",
    "Document B"
  ],
  "top_n": 2,
  "return_documents": true
}
```

## 7. Image APIs

DTO 结构见：[dto/openai_image.go](/e:/go_project/new-api/dto/openai_image.go:14)

### 7.1 `POST /v1/images/generations`

必填字段：
- `model: string`
- `prompt: string`

可选字段：
- `n: integer`
- `size: string`
- `quality: string`
- `response_format: string`
- `style: any`
- `user: any`
- `extra_fields: any`
- `background: any`
- `moderation: any`
- `output_format: any`
- `output_compression: any`
- `partial_images: any`
- `watermark: boolean`
- `watermark_enabled: any`
- `user_id: any`

示例：

```json
{
  "model": "gpt-image-1",
  "prompt": "A futuristic city at sunset",
  "size": "1024x1024",
  "quality": "high",
  "n": 1
}
```

### 7.2 `POST /v1/images/edits`

支持两种格式：

- `application/json`
- `multipart/form-data`

`multipart/form-data` 常用字段：
- `model`
- `prompt`
- `image` 或 `image[]`
- `n`
- `size`
- `quality`
- `watermark`

### 7.3 `POST /v1/edits`

说明：
- 在本项目里同样走图片编辑 relay 流程

## 8. Audio APIs

DTO 结构见：[dto/audio.go](/e:/go_project/new-api/dto/audio.go:12)

### 8.1 `POST /v1/audio/speech`

请求格式：
- JSON

字段：
- `model: string`
- `input: string`
- `voice: string`
- `instructions?: string`
- `response_format?: string`
- `speed?: number`
- `stream_format?: string`
- `metadata?: any`

示例：

```json
{
  "model": "gpt-4o-mini-tts",
  "input": "Hello world",
  "voice": "alloy",
  "response_format": "mp3"
}
```

### 8.2 `POST /v1/audio/transcriptions`

请求格式：
- `multipart/form-data`

至少需要：
- `file`
- `model`

常见附加字段会原样透传，例如：
- `language`
- `prompt`
- `response_format`
- `temperature`

### 8.3 `POST /v1/audio/translations`

请求格式：
- `multipart/form-data`

至少需要：
- `file`
- `model`

## 9. Realtime API

### 9.1 `GET /v1/realtime`

用途：
- OpenAI Realtime WebSocket 兼容入口

事件结构见：
- [dto/realtime.go](/e:/go_project/new-api/dto/realtime.go:24)
- [dto/realtime.go](/e:/go_project/new-api/dto/realtime.go:48)

常见客户端事件类型：
- `session.update`
- `conversation.item.create`
- `response.create`
- `input_audio_buffer.append`

常见服务端事件类型：
- `session.created`
- `session.updated`
- `response.audio.delta`
- `response.audio_transcript.delta`
- `response.function_call_arguments.delta`
- `response.function_call_arguments.done`
- `response.done`
- `conversation.item.created`
- `error`

典型事件示例：

```json
{
  "event_id": "evt_123",
  "type": "session.update",
  "session": {
    "modalities": ["text", "audio"],
    "instructions": "You are a realtime assistant",
    "voice": "alloy",
    "input_audio_format": "pcm16",
    "output_audio_format": "pcm16",
    "tool_choice": "auto",
    "temperature": 0.8
  }
}
```

## 10. Video APIs

路由见：[router/video-router.go](/e:/go_project/new-api/router/video-router.go:10)

通用视频请求 DTO 见：[dto/video.go](/e:/go_project/new-api/dto/video.go:3)

字段：
- `model?: string`
- `prompt?: string`
- `image?: string`
- `duration: number`
- `width: integer`
- `height: integer`
- `fps?: integer`
- `seed?: integer`
- `n?: integer`
- `response_format?: string`
- `user?: string`
- `metadata?: object`

### 10.1 OpenAI-compatible video routes

- `POST /v1/video/generations`
- `GET /v1/video/generations/:task_id`
- `POST /v1/videos`
- `GET /v1/videos/:task_id`
- `POST /v1/videos/:video_id/remix`

提交示例：

```json
{
  "model": "kling-v1",
  "prompt": "A robot walking in the rain",
  "duration": 5,
  "width": 720,
  "height": 1280,
  "fps": 24
}
```

查询结果常见字段：
- `task_id`
- `status`
- `url`
- `format`
- `metadata`
- `error`

### 10.2 Kling routes

- `POST /kling/v1/videos/text2video`
- `POST /kling/v1/videos/image2video`
- `GET /kling/v1/videos/text2video/:task_id`
- `GET /kling/v1/videos/image2video/:task_id`

### 10.3 Video content proxy

- `GET /v1/videos/:task_id/content`

说明：
- 用于代理下载/播放已生成视频内容

## 11. Gemini Native Routes

路由入口在 [router/relay-router.go](/e:/go_project/new-api/router/relay-router.go:153) 和 [router/relay-router.go](/e:/go_project/new-api/router/relay-router.go:190)

包含：
- `POST /v1/models/*path`
- `POST /v1beta/models/*path`
- `POST /v1/engines/:model/embeddings`

说明：
- 这部分更接近 Gemini 原生 API 请求格式
- 项目只做基础校验与分发，详细字段以 Gemini 请求体为准

## 12. Billing Compatibility APIs

路由见：[router/dashboard.go](/e:/go_project/new-api/router/dashboard.go:10)

### 12.1 `GET /v1/dashboard/billing/subscription`

返回 OpenAI 风格订阅额度信息。

### 12.2 `GET /v1/dashboard/billing/usage`

返回 OpenAI 风格用量信息。

## 13. Validation Rules Summary

请求校验逻辑见：[relay/helper/valid_request.go](/e:/go_project/new-api/relay/helper/valid_request.go:20)

关键规则：
- `/v1/completions`
  - `model` 必填
  - `prompt` 必填
- `/v1/chat/completions`
  - `model` 必填
  - `messages` 必填，除非传 `prefix`/`suffix`
- `/v1/responses`
  - `model` 必填
  - `input` 必填
- `/v1/responses/compact`
  - `model` 必填
- `/v1/messages`
  - `model` 必填
  - `messages` 必填且非空
- `/v1/embeddings`
  - `input` 必填
- `/v1/moderations`
  - `input` 必填
- `/v1/rerank`
  - `query` 必填
  - `documents` 必填且非空
- `/v1/audio/speech`
  - `model` 必填
- `/v1/audio/transcriptions`
  - `model` 必填
  - multipart 中必须存在 `file`
- `/v1/audio/translations`
  - `model` 必填
  - multipart 中必须存在 `file`
- `/v1/images/generations`
  - `model` 必填
  - `prompt` 必填

## 14. Not Implemented

以下接口当前返回 `501 Not Implemented`：

- `POST /v1/images/variations`
- `GET /v1/files`
- `POST /v1/files`
- `DELETE /v1/files/:id`
- `GET /v1/files/:id`
- `GET /v1/files/:id/content`
- `POST /v1/fine-tunes`
- `GET /v1/fine-tunes`
- `GET /v1/fine-tunes/:id`
- `POST /v1/fine-tunes/:id/cancel`
- `GET /v1/fine-tunes/:id/events`
- `DELETE /v1/models/:model`
