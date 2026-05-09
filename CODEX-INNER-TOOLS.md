# Codex Inner Tools

This document summarizes the tool protocol used by the Codex source checked out at `../codex`.
It focuses on what the model sees in the OpenAI Responses request, what output items the model can
emit, and what Codex sends back after it executes client-side tools.

Source points used:

- `../codex/codex-rs/tools/src/tool_spec.rs`
- `../codex/codex-rs/tools/src/tool_registry_plan.rs`
- `../codex/codex-rs/protocol/src/models.rs`
- `../codex/codex-rs/core/src/tools/router.rs`
- `../codex/codex-rs/core/src/tools/context.rs`

## High-Level Model

Codex has three different categories of "built-in" tools:

- Hosted Responses tools: the upstream model platform executes them. In this source tree these are `web_search` and `image_generation`.
- Responses-native local tool: the model emits a provider-specific call item, but Codex executes it locally. In this source tree this is `local_shell`.
- Codex client-side tools: Codex declares them to the model as `function`, `namespace`, `tool_search`, or `custom` tools, executes them locally after the model emits a call item, then sends a tool output item in the next Responses request.

The current Codex source uses `web_search` for Responses web search. I did not find `web_search_preview` or `file_search` in the active Codex tool registry path in `../codex`.

## Common Tool Declarations

### Function Tool

Request declaration shape:

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

Model call item:

```json
{
  "type": "function_call",
  "name": "tool_name",
  "arguments": "{\"field\":\"value\"}",
  "call_id": "call_..."
}
```

Codex output item:

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "plain text output"
}
```

The `output` value can also be a multimodal content array:

```json
[
  { "type": "input_text", "text": "text result" },
  { "type": "input_image", "image_url": "data:image/png;base64,...", "detail": "high" }
]
```

### Namespace Tool

Request declaration shape:

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

Model call item:

```json
{
  "type": "function_call",
  "namespace": "namespace_name",
  "name": "tool_name",
  "arguments": "{\"field\":\"value\"}",
  "call_id": "call_..."
}
```

Codex generally returns `function_call_output` for namespaced tools after resolving them to MCP or dynamic tool handlers.

### Custom Freeform Tool

Request declaration shape:

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

Model call item:

```json
{
  "type": "custom_tool_call",
  "name": "apply_patch",
  "input": "*** Begin Patch\n...\n*** End Patch",
  "call_id": "call_..."
}
```

Codex output item:

```json
{
  "type": "custom_tool_call_output",
  "call_id": "call_...",
  "name": "apply_patch",
  "output": "patch applied"
}
```

## Provider-Side Responses Tools

### `web_search`

Declaration source: `create_web_search_tool` in `tools/src/tool_spec.rs`.

Request declaration shape:

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

Important fields:

- `external_web_access`: `false` for cached mode, `true` for live mode.
- `filters.allowed_domains`: optional domain allowlist.
- `user_location`: optional approximate location metadata.
- `search_context_size`: optional context-size hint.
- `search_content_types`: omitted for text-only models; `["text", "image"]` when the model supports image search content.

Model/provider output item:

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

Codex does not execute this locally and does not send a `function_call_output` for it. The provider performs the search and includes search results/citations in the response stream or response output.

Gateway implication: this is the only Codex provider-side search tool that can reasonably be mapped to an upstream `web_search` chat tool. It is not a normal function call.

### `image_generation`

Declaration source: `create_image_generation_tool` in `tools/src/tool_spec.rs`.

Request declaration shape:

```json
{
  "type": "image_generation",
  "output_format": "png"
}
```

Model/provider output item:

```json
{
  "type": "image_generation_call",
  "id": "ig_...",
  "status": "completed",
  "revised_prompt": "optional revised prompt",
  "result": "base64-or-provider-result"
}
```

Codex does not execute this locally. The upstream Responses provider owns the image generation workflow.

Gateway implication: this cannot be mapped to `web_search` and should go to a channel that supports Responses image generation, or be rejected/forwarded by policy.

### `local_shell`

Declaration source: `create_local_shell_tool` in `tools/src/tool_spec.rs`.

Request declaration shape:

```json
{
  "type": "local_shell"
}
```

Model call item:

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

Codex converts this to its local shell handler and returns a normal function output:

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "Wall time: 0.1234 seconds\nProcess exited with code 0\nOutput:\n..."
}
```

Gateway implication: even though the declaration is provider-specific (`local_shell`), execution is local to Codex. It should not be treated like hosted `web_search`.

## Codex Client-Side Tools

### Shell Tools: `shell`, `shell_command`, `exec_command`, `write_stdin`

Declaration sources: `tools/src/local_tool.rs`.

`shell` request arguments:

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

`shell_command` request arguments:

```json
{
  "command": "Get-ChildItem",
  "workdir": "E:\\go_project\\new-api",
  "login": true,
  "timeout_ms": 10000,
  "sandbox_permissions": "use_default"
}
```

`exec_command` request arguments:

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

`write_stdin` request arguments:

```json
{
  "session_id": 123,
  "chars": "y\n",
  "yield_time_ms": 1000,
  "max_output_tokens": 6000
}
```

Output shape:

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "Chunk ID: optional\nWall time: 0.1234 seconds\nProcess exited with code 0\nOutput:\n..."
}
```

For code-mode consumers, `exec_command` has a structured result equivalent to:

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

Declaration sources: `tools/src/apply_patch_tool.rs`.

Freeform declaration:

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

Freeform call:

```json
{
  "type": "custom_tool_call",
  "name": "apply_patch",
  "call_id": "call_...",
  "input": "*** Begin Patch\n*** Update File: file.go\n@@\n-old\n+new\n*** End Patch"
}
```

JSON function variant:

```json
{
  "type": "function_call",
  "name": "apply_patch",
  "call_id": "call_...",
  "arguments": "{\"input\":\"*** Begin Patch\\n...\\n*** End Patch\"}"
}
```

Output:

```json
{
  "type": "custom_tool_call_output",
  "call_id": "call_...",
  "name": "apply_patch",
  "output": "patch applied"
}
```

or, for the JSON function variant:

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "patch applied"
}
```

### `view_image`

Declaration source: `tools/src/view_image.rs`.

Request arguments:

```json
{
  "path": "E:\\path\\image.png",
  "detail": "original"
}
```

Output:

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

Declaration source: `tools/src/plan_tool.rs`.

Request arguments:

```json
{
  "explanation": "optional",
  "plan": [
    { "step": "Inspect code", "status": "completed" },
    { "step": "Write docs", "status": "in_progress" }
  ]
}
```

Output:

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "Plan updated"
}
```

### `request_user_input`

Declaration source: `tools/src/request_user_input_tool.rs`.

Request arguments:

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

The client adds support for an `Other` answer path. Output is a normal function result whose exact text depends on the client interaction result.

### `request_permissions`

Declaration source: `tools/src/local_tool.rs`.

Request arguments:

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

Output is a normal function result describing granted or denied permissions.

### Multi-Agent Tools

Declaration source: `tools/src/agent_tool.rs`.

Possible v1 tools:

- `spawn_agent`
- `send_input`
- `resume_agent`
- `wait_agent`
- `close_agent`

Possible v2 tools:

- `spawn_agent`
- `send_message`
- `followup_task`
- `wait_agent`
- `close_agent`
- `list_agents`

Representative request arguments:

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

Representative outputs:

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

The source defines JSON schemas for these outputs, but the Responses wire field is still `function_call_output.output`, so it is serialized as a string or content item payload.

### MCP Tools

Declaration sources: `tools/src/mcp_tool.rs`, `tools/src/responses_api.rs`, `tools/src/tool_registry_plan.rs`.

Direct MCP tools are exposed as namespace function tools:

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

Model call:

```json
{
  "type": "function_call",
  "namespace": "server_namespace",
  "name": "tool_name",
  "arguments": "{\"arg\":\"value\"}",
  "call_id": "call_..."
}
```

Codex output:

```json
{
  "type": "function_call_output",
  "call_id": "call_...",
  "output": "Wall time: 0.1234 seconds\nOutput:\n..."
}
```

MCP output can also become multimodal content items when MCP returns text/image content:

```json
[
  { "type": "input_text", "text": "text result" },
  { "type": "input_image", "image_url": "data:image/png;base64,...", "detail": "high" }
]
```

### Deferred Tool Discovery: `tool_search`

Declaration source: `tools/src/tool_discovery.rs`.

Request declaration:

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

Model call:

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

Codex output:

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

The `tools` array can also contain namespace specs.

### Dynamic Tools

Declaration source: `tools/src/dynamic_tool.rs`.

Dynamic tools come from the active Codex thread. They are converted into either a `function` declaration or a namespaced declaration. Deferred dynamic tools can be discovered later through `tool_search`.

Request and output follow the same `function_call` and `function_call_output` shapes as normal function tools.

### Code Mode Tools: `exec` and `wait`

Declaration source: `tools/src/code_mode.rs`.

`exec` is a freeform custom tool whose input is source text:

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

Model call:

```json
{
  "type": "custom_tool_call",
  "name": "exec",
  "call_id": "call_...",
  "input": "// @exec: optional\nsource code"
}
```

`wait` is a function tool:

```json
{
  "cell_id": "cell_...",
  "yield_time_ms": 1000,
  "max_tokens": 6000,
  "terminate": false
}
```

Outputs are returned as `custom_tool_call_output` or `function_call_output`, with either text or content items depending on the runtime result.

### Goal Tools: `get_goal`, `create_goal`, `update_goal`

Declaration source: `tools/src/goal_tool.rs`.

Request arguments:

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

Outputs are normal function results, generally text or serialized JSON describing the goal state.

### Plugin/Connector Install Suggestion: `request_plugin_install`

Declaration source: `tools/src/tool_discovery.rs`.

Request arguments:

```json
{
  "tool_type": "connector",
  "action_type": "install",
  "tool_id": "connector-id",
  "suggest_reason": "Concise user-facing reason."
}
```

Output is a normal function result depending on the user/client install flow.

### Agent Job Tools

Declaration source: `tools/src/agent_job_tool.rs`.

`spawn_agents_on_csv` request:

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

`report_agent_job_result` request:

```json
{
  "job_id": "job_...",
  "item_id": "row-id",
  "result": {},
  "stop": false
}
```

Outputs are normal function results.

### Test-Only Tool: `test_sync_tool`

Declaration source: `tools/src/utility_tool.rs`.

This is gated by experimental supported tools and is used by integration tests.

Request arguments:

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

Output is a normal function result.

## Mapping Notes For Gateway Adaptation

Can plausibly map to an upstream chat `web_search` tool:

- `web_search`: it is the only active Codex provider-side search tool in this source tree. Mapping requires translating the Responses declaration fields into the target chat provider's supported `web_search` schema and translating provider citations/usage back into Responses-compatible output where needed.

Must not be mapped to chat `web_search`:

- `image_generation`: different hosted capability.
- `local_shell`: local execution by Codex, not hosted web search.
- `tool_search`: client-side deferred tool discovery, returns tool declarations.
- `function`/`namespace` tools: client-side function or MCP execution.
- `custom` tools such as `apply_patch` and code-mode `exec`: client-side freeform tools.
- `view_image`, shell tools, plan, permissions, user input, multi-agent, goal, plugin install, dynamic tools, MCP tools, and agent job tools: all are Codex/client-side tools.

Not found in current Codex source tool registry:

- `file_search`
- `web_search_preview`

If these appear from another client or older/newer Codex build, treat `web_search_preview` as a possible search-tool alias only after checking its actual request schema. Treat `file_search` as not equivalent to web search unless the upstream provider exposes a matching file/vector-store search capability.
