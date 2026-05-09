# Codex 内部工具

本文档总结了 `../codex` 中检出的 Codex 源码所使用的工具协议。
重点说明模型在 OpenAI Responses 请求中会看到什么、模型可以发出哪些输出项，
以及 Codex 在执行客户端侧工具后会回传什么。

使用的源码位置：

- `../codex/codex-rs/tools/src/tool_spec.rs`
- `../codex/codex-rs/tools/src/tool_registry_plan.rs`
- `../codex/codex-rs/protocol/src/models.rs`
- `../codex/codex-rs/core/src/tools/router.rs`
- `../codex/codex-rs/core/src/tools/context.rs`

## 高层模型

Codex 有三类不同的“内置”工具：

- 托管的 Responses 工具：由上游模型平台执行。在该源码树中包括 `web_search` 和 `image_generation`。
- Responses 原生本地工具：模型发出 provider 专用的调用项，但由 Codex 在本地执行。在该源码树中是 `local_shell`。
- Codex 客户端侧工具：Codex 以 `function`、`namespace`、`tool_search` 或 `custom` 工具的形式声明给模型；模型发出调用项后，Codex 在本地执行，然后在下一次 Responses 请求中发送工具输出项。

当前 Codex 源码使用 `web_search` 作为 Responses 网络搜索工具。我没有在 `../codex` 的当前 Codex 工具注册路径中找到 `web_search_preview` 或 `file_search`。

## 通用工具声明

### Function 工具

请求声明形状：

```json
{
  "type": "function",
  "name": "tool_name",
  "description": "...",
  "strict": false,
  "parameters": {
    "type": "object",
    "properties": {},
    "required": [],
    "additionalProperties": false
  }
}
```

模型调用项：

```json
{
  "type": "function_call",
  "name": "tool_name",
  "arguments": "{\"field\":\"value\"}",
  "call_id": "call_..."
}
```

Codex 输出项：

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "plain text output"
}
```

`output` 值也可以是多模态内容数组：

```json
[
  { "type": "input_text", "text": "text result" },
  { "type": "input_image", "image_url": "data:image/png;base64,...", "detail": "high" }
]
```

### Namespace 工具

请求声明形状：

```json
{
  "type": "namespace",
  "name": "namespace_name",
  "description": "Tools in the namespace.",
  "tools": [
    {
      "type": "function",
      "name": "tool_name",
      "description": "...",
      "strict": false,
      "parameters": {}
    }
  ]
}
```

模型调用项：

```json
{
  "type": "function_call",
  "namespace": "namespace_name",
  "name": "tool_name",
  "arguments": "{\"field\":\"value\"}",
  "call_id": "call_..."
}
```

Codex 通常会先将命名空间工具解析到 MCP 或动态工具处理器，然后返回 `function_call_output`。

### Custom Freeform 工具

请求声明形状：

```json
{
  "type": "custom",
  "name": "apply_patch",
  "description": "...",
  "format": {
    "type": "grammar",
    "syntax": "lark",
    "definition": "..."
  }
}
```

模型调用项：

```json
{
  "type": "custom_tool_call",
  "name": "apply_patch",
  "input": "*** Begin Patch\n...\n*** End Patch",
  "call_id": "call_..."
}
```

Codex 输出项：

```json
{
  "type": "custom_tool_call_output",
  "call_id": "call_...",
  "name": "apply_patch",
  "output": "patch applied"
}
```

## Provider 侧 Responses 工具

### `web_search`

声明来源：`tools/src/tool_spec.rs` 中的 `create_web_search_tool`。

请求声明形状：

```json
{
  "type": "web_search",
  "external_web_access": true,
  "filters": {
    "allowed_domains": ["example.com"]
  },
  "user_location": {
    "type": "approximate",
    "country": "US",
    "region": "CA",
    "city": "San Francisco",
    "timezone": "America/Los_Angeles"
  },
  "search_context_size": "medium",
  "search_content_types": ["text", "image"]
}
```

重要字段：

- `external_web_access`：缓存模式为 `false`，实时模式为 `true`。
- `filters.allowed_domains`：可选的域名白名单。
- `user_location`：可选的近似位置元数据。
- `search_context_size`：可选的上下文大小提示。
- `search_content_types`：纯文本模型会省略；当模型支持图像搜索内容时为 `["text", "image"]`。

模型/provider 输出项：

```json
{
  "type": "web_search_call",
  "id": "ws_...",
  "status": "completed",
  "action": {
    "type": "search",
    "query": "weather Shenzhen Guangming"
  }
}
```

Codex 不会在本地执行该工具，也不会为它发送 `function_call_output`。provider 会执行搜索，并在响应流或响应输出中包含搜索结果/引用。

网关含义：这是唯一一个可以合理映射到上游 `web_search` chat 工具的 Codex provider 侧搜索工具。它不是普通函数调用。

### `image_generation`

声明来源：`tools/src/tool_spec.rs` 中的 `create_image_generation_tool`。

请求声明形状：

```json
{
  "type": "image_generation",
  "output_format": "png"
}
```

模型/provider 输出项：

```json
{
  "type": "image_generation_call",
  "id": "ig_...",
  "status": "completed",
  "revised_prompt": "optional revised prompt",
  "result": "base64-or-provider-result"
}
```

Codex 不会在本地执行该工具。上游 Responses provider 拥有图像生成工作流。

网关含义：这不能映射到 `web_search`，应发送到支持 Responses 图像生成的渠道，或按策略拒绝/转发。

### `local_shell`

声明来源：`tools/src/tool_spec.rs` 中的 `create_local_shell_tool`。

请求声明形状：

```json
{
  "type": "local_shell"
}
```

模型调用项：

```json
{
  "type": "local_shell_call",
  "call_id": "call_...",
  "status": "completed",
  "action": {
    "type": "exec",
    "command": ["git", "status"],
    "working_directory": "E:\\go_project\\new-api",
    "timeout_ms": 10000
  }
}
```

Codex 会将其转换到本地 shell 处理器，并返回普通函数输出：

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "Wall time: 0.1234 seconds\nProcess exited with code 0\nOutput:\n..."
}
```

网关含义：尽管声明是 provider 专用的（`local_shell`），执行仍然发生在 Codex 本地。不应把它当作托管的 `web_search` 处理。

## Codex 客户端侧工具

### Shell 工具：`shell`、`shell_command`、`exec_command`、`write_stdin`

声明来源：`tools/src/local_tool.rs`。

`shell` 请求参数：

```json
{
  "command": ["powershell.exe", "-Command", "Get-ChildItem"],
  "workdir": "E:\\go_project\\new-api",
  "timeout_ms": 10000,
  "sandbox_permissions": "use_default",
  "justification": "optional",
  "prefix_rule": ["git", "pull"],
  "additional_permissions": {
    "network": { "enabled": true },
    "file_system": {
      "read": ["E:\\path"],
      "write": ["E:\\path"]
    }
  }
}
```

`shell_command` 请求参数：

```json
{
  "command": "Get-ChildItem",
  "workdir": "E:\\go_project\\new-api",
  "login": true,
  "timeout_ms": 10000,
  "sandbox_permissions": "use_default"
}
```

`exec_command` 请求参数：

```json
{
  "cmd": "Get-ChildItem",
  "workdir": "E:\\go_project\\new-api",
  "shell": "powershell",
  "tty": false,
  "yield_time_ms": 1000,
  "max_output_tokens": 6000,
  "login": true,
  "environment_id": "optional",
  "sandbox_permissions": "use_default"
}
```

`write_stdin` 请求参数：

```json
{
  "session_id": 123,
  "chars": "y\n",
  "yield_time_ms": 1000,
  "max_output_tokens": 6000
}
```

输出形状：

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "Chunk ID: optional\nWall time: 0.1234 seconds\nProcess exited with code 0\nOutput:\n..."
}
```

对于 code-mode 消费方，`exec_command` 有一个等价的结构化结果：

```json
{
  "chunk_id": "optional",
  "wall_time_seconds": 0.1234,
  "exit_code": 0,
  "session_id": 123,
  "original_token_count": 10000,
  "output": "..."
}
```

### `apply_patch`

声明来源：`tools/src/apply_patch_tool.rs`。

Freeform 声明：

```json
{
  "type": "custom",
  "name": "apply_patch",
  "description": "...",
  "format": {
    "type": "grammar",
    "syntax": "lark",
    "definition": "..."
  }
}
```

Freeform 调用：

```json
{
  "type": "custom_tool_call",
  "name": "apply_patch",
  "call_id": "call_...",
  "input": "*** Begin Patch\n*** Update File: file.go\n@@\n-old\n+new\n*** End Patch"
}
```

JSON 函数变体：

```json
{
  "type": "function_call",
  "name": "apply_patch",
  "call_id": "call_...",
  "arguments": "{\"input\":\"*** Begin Patch\\n...\\n*** End Patch\"}"
}
```

输出：

```json
{
  "type": "custom_tool_call_output",
  "call_id": "call_...",
  "name": "apply_patch",
  "output": "patch applied"
}
```

或者，对于 JSON 函数变体：

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "patch applied"
}
```

### `view_image`

声明来源：`tools/src/view_image.rs`。

请求参数：

```json
{
  "path": "E:\\path\\image.png",
  "detail": "original"
}
```

输出：

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": [
    {
      "type": "input_image",
      "image_url": "data:image/png;base64,...",
      "detail": "original"
    }
  ]
}
```

### `update_plan`

声明来源：`tools/src/plan_tool.rs`。

请求参数：

```json
{
  "explanation": "optional",
  "plan": [
    { "step": "Inspect code", "status": "completed" },
    { "step": "Write docs", "status": "in_progress" }
  ]
}
```

输出：

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "Plan updated"
}
```

### `request_user_input`

声明来源：`tools/src/request_user_input_tool.rs`。

请求参数：

```json
{
  "questions": [
    {
      "id": "routing_choice",
      "header": "Routing",
      "question": "Which routing behavior should we use?",
      "options": [
        {
          "label": "Forward",
          "description": "Use the fallback channel for unsupported tools."
        }
      ]
    }
  ]
}
```

客户端会增加对 `Other` 回答路径的支持。输出是普通函数结果，其具体文本取决于客户端交互结果。

### `request_permissions`

声明来源：`tools/src/local_tool.rs`。

请求参数：

```json
{
  "permissions": {
    "network": { "enabled": true },
    "file_system": {
      "read": ["E:\\path"],
      "write": ["E:\\path"]
    }
  },
  "reason": "Need to run tests that fetch dependencies."
}
```

输出是普通函数结果，用于描述权限已授予或被拒绝。

### 多 Agent 工具

声明来源：`tools/src/agent_tool.rs`。

可能的 v1 工具：

- `spawn_agent`
- `send_input`
- `resume_agent`
- `wait_agent`
- `close_agent`

可能的 v2 工具：

- `spawn_agent`
- `send_message`
- `followup_task`
- `wait_agent`
- `close_agent`
- `list_agents`

代表性请求参数：

```json
{
  "message": "Investigate this module.",
  "items": [
    { "type": "text", "text": "..." },
    { "type": "image", "image_url": "https://..." },
    { "type": "local_image", "path": "E:\\path\\image.png" },
    { "type": "skill", "path": "skill/path", "name": "skill-name" },
    { "type": "mention", "path": "app://connector", "name": "Connector" }
  ],
  "agent_type": "worker",
  "fork_context": true,
  "model": "optional",
  "reasoning_effort": "optional"
}
```

代表性输出：

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "{\"agent_id\":\"agent_...\",\"nickname\":null}"
}
```

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "{\"status\":{\"agent_...\":{\"completed\":\"final text\"}},\"timed_out\":false}"
}
```

源码为这些输出定义了 JSON schema，但 Responses 线路字段仍然是 `function_call_output.output`，因此它会被序列化为字符串或内容项 payload。

### MCP 工具

声明来源：`tools/src/mcp_tool.rs`、`tools/src/responses_api.rs`、`tools/src/tool_registry_plan.rs`。

直接 MCP 工具会作为 namespace function 工具暴露：

```json
{
  "type": "namespace",
  "name": "server_namespace",
  "description": "Tools for ...",
  "tools": [
    {
      "type": "function",
      "name": "tool_name",
      "description": "...",
      "strict": false,
      "parameters": {
        "type": "object",
        "properties": {}
      }
    }
  ]
}
```

模型调用：

```json
{
  "type": "function_call",
  "namespace": "server_namespace",
  "name": "tool_name",
  "arguments": "{\"arg\":\"value\"}",
  "call_id": "call_..."
}
```

Codex 输出：

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "Wall time: 0.1234 seconds\nOutput:\n..."
}
```

当 MCP 返回文本/图像内容时，MCP 输出也可以变成多模态内容项：

```json
[
  { "type": "input_text", "text": "text result" },
  { "type": "input_image", "image_url": "data:image/png;base64,...", "detail": "high" }
]
```

### 延迟工具发现：`tool_search`

声明来源：`tools/src/tool_discovery.rs`。

请求声明：

```json
{
  "type": "tool_search",
  "execution": "client",
  "description": "...",
  "parameters": {
    "type": "object",
    "properties": {
      "query": { "type": "string" },
      "limit": { "type": "number" }
    },
    "required": ["query"],
    "additionalProperties": false
  }
}
```

模型调用：

```json
{
  "type": "tool_search_call",
  "call_id": "call_...",
  "execution": "client",
  "arguments": {
    "query": "search terms",
    "limit": 8
  }
}
```

Codex 输出：

```json
{
  "type": "tool_search_output",
  "call_id": "call_...",
  "status": "completed",
  "execution": "client",
  "tools": [
    {
      "type": "function",
      "name": "loaded_tool",
      "description": "...",
      "strict": false,
      "parameters": {}
    }
  ]
}
```

`tools` 数组也可以包含 namespace 规格。

### 动态工具

声明来源：`tools/src/dynamic_tool.rs`。

动态工具来自当前活跃的 Codex 线程。它们会被转换为 `function` 声明或 namespaced 声明。延迟动态工具可以稍后通过 `tool_search` 发现。

请求和输出遵循与普通 function 工具相同的 `function_call` 和 `function_call_output` 形状。

### Code Mode 工具：`exec` 和 `wait`

声明来源：`tools/src/code_mode.rs`。

`exec` 是一个 freeform custom 工具，其输入是源码文本：

```json
{
  "type": "custom",
  "name": "exec",
  "description": "...",
  "format": {
    "type": "grammar",
    "syntax": "lark",
    "definition": "..."
  }
}
```

模型调用：

```json
{
  "type": "custom_tool_call",
  "name": "exec",
  "call_id": "call_...",
  "input": "// @exec: optional\nsource code"
}
```

`wait` 是一个 function 工具：

```json
{
  "cell_id": "cell_...",
  "yield_time_ms": 1000,
  "max_tokens": 6000,
  "terminate": false
}
```

输出以 `custom_tool_call_output` 或 `function_call_output` 返回，根据运行时结果，内容可以是文本或内容项。

### Goal 工具：`get_goal`、`create_goal`、`update_goal`

声明来源：`tools/src/goal_tool.rs`。

请求参数：

```json
{}
```

```json
{
  "objective": "Finish the task",
  "token_budget": 100000
}
```

```json
{
  "status": "complete"
}
```

输出是普通函数结果，通常是描述目标状态的文本或序列化 JSON。

### Plugin/Connector 安装建议：`request_plugin_install`

声明来源：`tools/src/tool_discovery.rs`。

请求参数：

```json
{
  "tool_type": "connector",
  "action_type": "install",
  "tool_id": "connector-id",
  "suggest_reason": "Concise user-facing reason."
}
```

输出是普通函数结果，取决于用户/客户端安装流程。

### Agent Job 工具

声明来源：`tools/src/agent_job_tool.rs`。

`spawn_agents_on_csv` 请求：

```json
{
  "csv_path": "input.csv",
  "instruction": "Process {column}",
  "id_column": "id",
  "output_csv_path": "output.csv",
  "max_concurrency": 16,
  "max_workers": 16,
  "max_runtime_seconds": 1800,
  "output_schema": {}
}
```

`report_agent_job_result` 请求：

```json
{
  "job_id": "job_...",
  "item_id": "row-id",
  "result": {},
  "stop": false
}
```

输出是普通函数结果。

### 仅测试工具：`test_sync_tool`

声明来源：`tools/src/utility_tool.rs`。

该工具受 experimental supported tools 门控，用于集成测试。

请求参数：

```json
{
  "sleep_before_ms": 100,
  "sleep_after_ms": 100,
  "barrier": {
    "id": "shared-id",
    "participants": 2,
    "timeout_ms": 1000
  }
}
```

输出是普通函数结果。

## 网关适配映射说明

可以合理映射到上游 chat `web_search` 工具：

- `web_search`：它是该源码树中唯一活跃的 Codex provider 侧搜索工具。映射需要将 Responses 声明字段转换为目标 chat provider 支持的 `web_search` schema，并在需要时将 provider 引用/用量转换回 Responses 兼容的输出。

不得映射到 chat `web_search`：

- `image_generation`：不同的托管能力。
- `local_shell`：由 Codex 本地执行，不是托管网络搜索。
- `tool_search`：客户端侧延迟工具发现，返回工具声明。
- `function`/`namespace` 工具：客户端侧 function 或 MCP 执行。
- `custom` 工具，例如 `apply_patch` 和 code-mode `exec`：客户端侧 freeform 工具。
- `view_image`、shell 工具、plan、permissions、user input、multi-agent、goal、plugin install、dynamic tools、MCP tools 和 agent job tools：全部都是 Codex/客户端侧工具。

当前 Codex 源码工具注册表中未找到：

- `file_search`
- `web_search_preview`

如果这些工具来自另一个客户端或旧版/新版 Codex 构建，请仅在检查其实际请求 schema 后，才将 `web_search_preview` 视为可能的搜索工具别名。除非上游 provider 暴露了匹配的文件/向量存储搜索能力，否则应将 `file_search` 视为不等价于网络搜索。
