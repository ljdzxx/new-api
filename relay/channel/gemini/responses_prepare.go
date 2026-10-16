package gemini

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// Port of upstream's ExtractRequest -> body conversion -> AttachRequest flow,
// scoped to the local Gemini/Vertex adapters rather than importing relaykit.
type responsesRequestPlan struct {
	items     []map[string]any
	tools     []dto.GeminiChatTool
	choice    *dto.ToolConfig
	names     responsesToolNames
	custom    map[string]bool
	functions map[string]bool
	c         *gin.Context
	info      *relaycommon.RelayInfo
	policy    types.ConversionLossPolicy
	rejected  error
}

func (p *responsesRequestPlan) loss(path, code, message string, semantic bool) {
	severity := types.ConversionDiagnosticWarning
	if semantic {
		severity = types.ConversionDiagnosticError
	}
	diagnostic := types.ConversionDiagnostic{Code: code, Path: path, Message: message, Severity: severity, From: types.RelayFormatOpenAIResponses, To: types.RelayFormatGemini}
	p.info.RecordConversionDiagnostics(p.c, []types.ConversionDiagnostic{diagnostic})
	// Do not let the audit cap hide a later semantic error under safe policy.
	if p.rejected == nil {
		p.rejected = types.RejectConversionLoss(p.policy, []types.ConversionDiagnostic{diagnostic})
	}
}

func prepareGeminiResponses(c *gin.Context, info *relaycommon.RelayInfo, req dto.OpenAIResponsesRequest) (*responsesRequestPlan, error) {
	p := &responsesRequestPlan{c: c, info: info, names: make(responsesToolNames), custom: make(map[string]bool), functions: make(map[string]bool), policy: info.ChannelOtherSettings.ToolLossPolicy}
	if p.policy == "" {
		p.policy = types.ConversionLossPolicyAllow
	}
	switch p.policy {
	case types.ConversionLossPolicyAllow, types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict:
	default:
		return nil, fmt.Errorf("invalid tool_loss_policy %q", p.policy)
	}
	var tools []map[string]any
	if responsesRawPresent(req.Tools) {
		if err := common.Unmarshal(req.Tools, &tools); err != nil {
			return nil, fmt.Errorf("invalid tools: %w", err)
		}
	}
	var items []map[string]any
	if common.GetJsonType(req.Input) == "string" {
		var text string
		if err := common.Unmarshal(req.Input, &text); err != nil {
			return nil, err
		}
		items = []map[string]any{{"role": "user", "content": text}}
	} else if err := common.Unmarshal(req.Input, &items); err != nil || items == nil {
		return nil, fmt.Errorf("input must be a string or an array of items")
	}
	for i, item := range items {
		path := fmt.Sprintf("input[%d]", i)
		switch kind := responsesString(item["type"]); kind {
		case "additional_tools":
			raw, err := common.Marshal(item["tools"])
			if err != nil {
				return nil, err
			}
			var extra []map[string]any
			if err = common.Unmarshal(raw, &extra); err != nil {
				return nil, fmt.Errorf("%s.tools must be an array", path)
			}
			tools = append(tools, extra...)
			p.loss(path, "additional_tools_flattened", "additional_tools declarations were moved to top-level tools", false)
		case "web_search_call", "file_search_call", "code_interpreter_call", "computer_call", "computer_call_output", "image_generation_call", "local_shell_call", "local_shell_call_output", "apply_patch_call", "apply_patch_call_output", "mcp_call", "mcp_list_tools", "mcp_approval_request", "mcp_approval_response", "tool_search_call", "tool_search_output":
			p.loss(path, "hosted_tool_history_unsupported", fmt.Sprintf("Gemini cannot preserve hosted-tool continuation item %q; it was omitted", kind), true)
		case "", "message", "reasoning", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
			p.items = append(p.items, item)
		default:
			// Opaque input extensions have no Gemini representation. Apply the
			// same loss policy rather than failing on every new Responses item.
			p.loss(path, "unsupported_input_item", fmt.Sprintf("Gemini cannot preserve Responses input item %q; it was omitted", kind), true)
		}
	}
	tools, err := flattenGeminiResponsesTools(tools, p.names, p)
	if err != nil {
		return nil, err
	}
	// Function declarations win over same-name custom definitions, independent
	// of order, matching upstream's responsesCustomTools selection.
	for _, tool := range tools {
		if responsesString(tool["type"]) == "function" {
			p.functions[responsesString(tool["name"])] = true
		}
	}
	var declarations []dto.GeminiFunctionDeclaration
	seen := make(map[string]bool)
	for i, tool := range tools {
		path := fmt.Sprintf("tools[%d]", i)
		kind, name := responsesString(tool["type"]), responsesString(tool["name"])
		fn := dto.FunctionRequest{Name: name, Description: responsesString(tool["description"]), Parameters: tool["parameters"]}
		switch kind {
		case "function", "custom":
			if kind == "custom" && (p.functions[name] || seen[name]) {
				p.loss(path, "custom_tool_name_conflict", fmt.Sprintf("custom tool %q conflicts with another tool and was omitted", name), true)
				continue
			}
			if seen[name] {
				p.loss(path, "namespaced_tool_dropped", fmt.Sprintf("duplicate tool %q was omitted", name), true)
				continue
			}
			seen[name] = true
			if kind == "custom" {
				p.custom[name] = true
				fn.Parameters = map[string]any{"type": "object", "properties": map[string]any{"input": map[string]any{"type": "string"}}, "required": []string{"input"}}
				fn.Description += "\nThis tool takes freeform text. Put the complete raw text in the input argument."
				if format, ok := tool["format"].(map[string]any); ok && responsesString(format["type"]) == "grammar" {
					fn.Description += "\nThe input must match this " + responsesString(format["syntax"]) + " grammar:\n" + responsesString(format["definition"])
				}
				p.loss(path, "custom_tool_as_function", "custom tool was encoded as a function with one string input argument; its format is a description hint", false)
			}
			if _, present := tool["strict"]; present {
				p.loss(path+".strict", "unsupported_function_strict", "Gemini does not expose OpenAI function strictness", false)
			}
			declaration := convertGeminiFunctionDeclaration(c, info, fn)
			if geminiSupportsJSONSchema(info) {
				declaration.Parameters = nil
				declaration.ParametersJsonSchema = fn.Parameters
			} else {
				p.schemaLoss(path+".parameters", fn.Parameters)
			}
			declarations = append(declarations, declaration)
		case "web_search", "web_search_preview", "web_search_2025_08_26", "web_search_preview_2025_03_11":
			p.tools = append(p.tools, dto.GeminiChatTool{GoogleSearch: map[string]any{}})
			if _, ok := tool["filters"]; ok {
				p.loss(path+".filters", "unsupported_search_constraints", "Gemini cannot preserve source web-search domain constraints", true)
			}
			if _, ok := tool["external_web_access"]; ok {
				p.loss(path+".external_web_access", "unsupported_search_constraints", "Gemini cannot preserve external web access control", true)
			}
			for _, field := range []string{"user_location", "search_context_size", "return_token_budget"} {
				if _, ok := tool[field]; ok {
					p.loss(path+"."+field, "unsupported_search_tuning", "Gemini cannot preserve source web-search location or result tuning", false)
				}
			}
		case "codeExecution":
			p.tools = append(p.tools, dto.GeminiChatTool{CodeExecution: map[string]any{}})
		case "urlContext":
			p.tools = append(p.tools, dto.GeminiChatTool{URLContext: map[string]any{}})
		default:
			code := "unsupported_opaque_tool"
			switch kind {
			case "file_search", "mcp", "image_generation", "computer_use_preview", "computer":
				code = "unsupported_hosted_tool"
			case "code_interpreter", "code_execution", "url_context":
				code = "unverified_tool_mapping"
			}
			p.loss(path, code, fmt.Sprintf("Gemini cannot represent Responses tool %q; its definition was omitted", kind), true)
		}
	}
	if len(declarations) > 0 {
		p.tools = append(p.tools, dto.GeminiChatTool{FunctionDeclarations: declarations})
	}
	if err := p.prepareChoice(req.ToolChoice, len(declarations) > 0); err != nil {
		return nil, err
	}
	if responsesRawPresent(req.ParallelToolCalls) {
		var parallel bool
		if err := common.Unmarshal(req.ParallelToolCalls, &parallel); err != nil {
			return nil, fmt.Errorf("parallel_tool_calls must be a boolean")
		}
		if !parallel && len(declarations) > 0 {
			p.loss("parallel_tool_calls", "unsupported_parallel_tool_control", "Gemini does not expose a field equivalent to parallel_tool_calls=false", true)
		}
	}
	return p, nil
}

func (p *responsesRequestPlan) prepareChoice(raw []byte, hasFunctions bool) error {
	if !responsesRawPresent(raw) {
		return nil
	}
	var choice any
	if err := common.Unmarshal(raw, &choice); err != nil {
		return fmt.Errorf("invalid tool_choice: %w", err)
	}
	var mode, name string
	switch v := choice.(type) {
	case string:
		switch v {
		case "auto":
			mode = "AUTO"
		case "none":
			mode = "NONE"
		case "required":
			mode = "ANY"
		default:
			p.loss("tool_choice", "unsupported_tool_choice", "unknown tool choice was omitted", true)
		}
	case map[string]any:
		kind := responsesString(v["type"])
		if kind == "function" || kind == "custom" {
			var err error
			name, err = p.names.register(responsesString(v["namespace"]), responsesString(v["name"]))
			if err != nil {
				return err
			}
			if p.functions[name] || p.custom[name] {
				mode = "ANY"
			} else {
				p.loss("tool_choice", "unsupported_tool_choice", "the selected tool is not available after conversion", true)
			}
		} else {
			p.loss("tool_choice", "unsupported_tool_choice", "Gemini cannot represent this hosted or complex tool-choice policy", true)
		}
	default:
		return fmt.Errorf("invalid tool_choice")
	}
	if mode == "NONE" {
		p.tools = nil
		return nil
	}
	if !hasFunctions {
		if mode != "" && mode != "AUTO" {
			p.loss("tool_choice", "tool_choice_without_tools", "tool choice cannot be applied without converted function tools", true)
		}
		return nil
	}
	if mode != "" {
		p.choice = &dto.ToolConfig{FunctionCallingConfig: &dto.FunctionCallingConfig{Mode: dto.FunctionCallingConfigMode(mode)}}
		if name != "" {
			p.choice.FunctionCallingConfig.AllowedFunctionNames = []string{name}
		}
	}
	return nil
}

func (p *responsesRequestPlan) schemaLoss(path string, schema any) {
	if misfits := geminiSchemaMisfits(schema); len(misfits) > 0 {
		p.loss(path, "gemini_schema_keyword_dropped", "Gemini v1 schema cannot preserve: "+strings.Join(misfits, ", "), false)
	}
}

func (p *responsesRequestPlan) omitTrailingAssistant(req *dto.GeminiChatRequest) {
	end := len(req.Contents)
	for end > 0 {
		turn := req.Contents[end-1]
		if turn.Role != "model" {
			break
		}
		hasCall := false
		for _, part := range turn.Parts {
			if part.FunctionCall != nil {
				hasCall = true
				break
			}
		}
		if hasCall {
			break
		}
		end--
	}
	if end > 0 && end < len(req.Contents) && req.Contents[end-1].Role == "user" {
		req.Contents = req.Contents[:end]
		p.loss("input", "trailing_assistant_omitted", "assistant output after the last user message or tool result was omitted to avoid unsupported prefill", false)
	}
}
