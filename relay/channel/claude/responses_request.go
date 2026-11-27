package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/claudethinking"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service/openaicompat"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const claudeResponsesToolsKey = "claude_responses_tool_mapping"

// Adapted from upstream oai_responses/to_claude_messages_req.go. The local
// gateway retains its own diagnostics, thinking renderer and billing paths.
func ConvertResponsesToClaude(c *gin.Context, info *relaycommon.RelayInfo, req dto.OpenAIResponsesRequest) (*dto.ClaudeRequest, error) {
	p, err := newClaudeConversion(c, info)
	if err != nil {
		return nil, err
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if err = req.ValidateStatelessConversion(); err != nil {
		return nil, err
	}
	if req.PreviousResponseID != "" || rawPresent(req.Conversation) || rawPresent(req.Prompt) {
		return nil, fmt.Errorf("Claude requires inline history; Responses state references are unsupported")
	}
	if err = validateClaudeMaxTokens(req.MaxOutputTokens); err != nil {
		return nil, err
	}
	if rawPresent(req.ContextManagement) {
		p.loss("context_management", "Responses context management cannot be represented in Claude Messages", true)
	}
	if rawPresent(req.Truncation) && rawString(req.Truncation) != "disabled" {
		p.loss("truncation", "Responses automatic truncation cannot be represented in Claude Messages", true)
	}
	if rawPresent(req.Text) {
		var options struct {
			Format struct {
				Type string `json:"type"`
			} `json:"format"`
			Verbosity string `json:"verbosity"`
		}
		if err = common.Unmarshal(req.Text, &options); err != nil {
			return nil, fmt.Errorf("invalid text options: %w", err)
		}
		if options.Format.Type != "" && options.Format.Type != "text" {
			p.loss("text.format", "Responses output format constraints cannot be preserved by this Claude conversion", true)
		}
		if options.Verbosity != "" {
			p.loss("text.verbosity", "Responses verbosity cannot be represented in Claude", false)
		}
	}
	if req.MaxToolCalls != nil {
		p.loss("max_tool_calls", "Claude cannot enforce the Responses tool call limit", true)
	}
	if req.TopLogProbs != nil {
		p.loss("top_logprobs", "Claude does not expose log probabilities", false)
	}
	toolsRaw, err := p.responsesTools(req.Tools)
	if err != nil {
		return nil, err
	}
	tools, choice, mapping, err := openaicompat.ResponsesToolsToFunctions(toolsRaw, req.ToolChoice)
	if err != nil {
		return nil, err
	}
	out := &dto.ClaudeRequest{Model: req.Model, Stream: req.Stream, Temperature: req.Temperature, TopP: req.TopP, MaxTokens: req.MaxOutputTokens}
	var claudeTools []any
	for _, tool := range tools {
		schema, err := claudeFunctionSchema(tool.Function.Parameters)
		if err != nil {
			return nil, err
		}
		claudeTools = append(claudeTools, dto.Tool{Name: tool.Function.Name, Description: tool.Function.Description, InputSchema: schema})
	}
	if len(claudeTools) > 0 {
		out.Tools = claudeTools
	}
	var parallel *bool
	if rawPresent(req.ParallelToolCalls) {
		if err = common.Unmarshal(req.ParallelToolCalls, &parallel); err != nil {
			return nil, fmt.Errorf("invalid parallel_tool_calls: %w", err)
		}
	}
	if choice != nil || parallel != nil {
		out.ToolChoice = mapToolChoice(choice, parallel)
	}
	source := &dto.GeneralOpenAIRequest{Model: req.Model}
	if req.Reasoning != nil {
		source.ReasoningEffort = req.Reasoning.Effort
		if rawPresent(req.Reasoning.Mode) || rawPresent(req.Reasoning.Context) {
			p.loss("reasoning", "Responses reasoning mode/context cannot be replayed in Claude", true)
		}
	}
	if out.MaxTokens != nil && *out.MaxTokens == 0 {
		out.MaxTokens = nil
	}
	if err = claudethinking.ApplyOpenAI(c, out, source, info); err != nil {
		return nil, err
	}
	if rawPresent(req.Instructions) {
		var instruction string
		if err = common.Unmarshal(req.Instructions, &instruction); err != nil {
			return nil, fmt.Errorf("instructions must be a string")
		}
		if instruction != "" {
			appendClaudeBlocks(out, "system", dto.ClaudeMediaMessage{Type: "text", Text: &instruction})
		}
	}
	var items []map[string]json.RawMessage
	if common.GetJsonType(req.Input) == "string" {
		items = []map[string]json.RawMessage{{"role": json.RawMessage(`"user"`), "content": req.Input}}
	} else if err = common.Unmarshal(req.Input, &items); err != nil || items == nil {
		return nil, fmt.Errorf("input must be a string or an array")
	}
	for index, item := range items {
		path := fmt.Sprintf("input[%d]", index)
		kind := rawString(item["type"])
		switch kind {
		case "function_call", "custom_tool_call":
			name, err := openaicompat.ResponsesFunctionName(rawString(item["namespace"]), rawString(item["name"]))
			if err != nil {
				return nil, err
			}
			id := rawString(item["call_id"])
			if id == "" {
				return nil, fmt.Errorf("%s requires call_id", path)
			}
			identity := dto.ResponsesToolIdentity{Name: rawString(item["name"]), Namespace: rawString(item["namespace"]), Custom: kind == "custom_tool_call"}
			if identity.Namespace == "functions" {
				identity.Namespace = ""
			}
			if previous, exists := mapping[name]; exists && previous != identity {
				return nil, fmt.Errorf("tool history conflicts with definition %q", name)
			}
			mapping[name] = identity
			var input any
			if kind == "custom_tool_call" {
				var text string
				if err = common.Unmarshal(item["input"], &text); err != nil {
					return nil, fmt.Errorf("custom tool input must be a string")
				}
				input = map[string]string{"input": text}
			} else {
				var arguments string
				if err = common.Unmarshal(item["arguments"], &arguments); err != nil {
					return nil, fmt.Errorf("function arguments must be a JSON string")
				}
				var obj map[string]json.RawMessage
				if err = common.Unmarshal([]byte(arguments), &obj); err != nil || obj == nil {
					return nil, fmt.Errorf("function arguments must contain a JSON object")
				}
				input = json.RawMessage(arguments)
			}
			appendClaudeBlocks(out, "assistant", dto.ClaudeMediaMessage{Type: "tool_use", Id: id, Name: name, Input: input})
		case "function_call_output", "custom_tool_call_output":
			id := rawString(item["call_id"])
			if id == "" {
				return nil, fmt.Errorf("%s requires call_id", path)
			}
			var content any
			if responsesToolOutputHasParts(item["output"]) {
				content, err = p.responsesContent(item["output"], path+".output")
				if err != nil {
					return nil, err
				}
			} else if common.GetJsonType(item["output"]) == "string" {
				content = rawString(item["output"])
			} else {
				content = string(item["output"])
			}
			appendClaudeBlocks(out, "user", dto.ClaudeMediaMessage{Type: "tool_result", ToolUseId: id, Content: content})
		case "", "message":
			role := rawString(item["role"])
			if role == "" {
				role = "user"
			}
			if role != "user" && role != "assistant" && role != "system" && role != "developer" {
				return nil, fmt.Errorf("unsupported role %q", role)
			}
			blocks, err := p.responsesContent(item["content"], path+".content")
			if err != nil {
				return nil, err
			}
			if role == "system" || role == "developer" {
				texts := blocks[:0]
				for _, block := range blocks {
					if block.Type == "text" {
						texts = append(texts, block)
					} else {
						p.loss(path, "Claude system messages only accept text", true)
					}
				}
				blocks = texts
			}
			appendClaudeBlocks(out, role, blocks...)
		case "reasoning":
			p.loss(path, "Responses reasoning history cannot be replayed as signed Claude thinking; it was omitted", false)
		default:
			p.loss(path, "Responses item "+kind+" cannot be represented in Claude; it was omitted", true)
		}
	}
	// Upstream omits a trailing assistant text prefill, which newer Claude models reject.
	for len(out.Messages) > 0 && out.Messages[len(out.Messages)-1].Role == "assistant" {
		parts, _ := out.Messages[len(out.Messages)-1].Content.([]dto.ClaudeMediaMessage)
		hasTool := false
		for _, part := range parts {
			hasTool = hasTool || part.Type == "tool_use"
		}
		if hasTool {
			break
		}
		p.loss("input", "trailing assistant text was omitted because Claude does not support this prefill", false)
		out.Messages = out.Messages[:len(out.Messages)-1]
	}
	if p.err != nil {
		return nil, p.err
	}
	if err = finishClaudeRequest(out); err != nil {
		return nil, err
	}
	if c != nil {
		c.Set(claudeResponsesToolsKey, mapping)
	}
	return out, nil
}

func rawString(raw json.RawMessage) string { var s string; _ = common.Unmarshal(raw, &s); return s }

// Arbitrary JSON arrays are tool data, not necessarily multimodal content.
func responsesToolOutputHasParts(raw json.RawMessage) bool {
	var parts []map[string]json.RawMessage
	if common.GetJsonType(raw) != "array" || common.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		switch rawString(part["type"]) {
		case "text", "input_text", "output_text", "input_image", "input_file", "input_audio", "input_video":
		default:
			return false
		}
	}
	return true
}

func (p *claudeRequestConversion) responsesTools(raw json.RawMessage) (json.RawMessage, error) {
	if !rawPresent(raw) {
		return nil, nil
	}
	var tools []map[string]json.RawMessage
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil, err
	}
	kept := make([]map[string]json.RawMessage, 0, len(tools))
	for _, tool := range tools {
		kind := rawString(tool["type"])
		switch kind {
		case "namespace":
			children, err := p.responsesTools(tool["tools"])
			if err != nil {
				return nil, err
			}
			tool["tools"] = children
			p.loss("tools", "tool namespace was flattened with a reversible name mapping", false)
		case "custom":
			p.loss("tools", "custom tool was encoded as a function with a string input argument", false)
			var format map[string]json.RawMessage
			if rawPresent(tool["format"]) && common.Unmarshal(tool["format"], &format) != nil {
				return nil, fmt.Errorf("invalid custom format")
			}
			if rawString(format["type"]) == "grammar" {
				desc := rawString(tool["description"]) + "\nInput must match this " + rawString(format["syntax"]) + " grammar:\n" + rawString(format["definition"])
				tool["description"], _ = common.Marshal(desc)
				tool["format"] = json.RawMessage(`{"type":"text"}`)
			}
		case "function":
		default:
			p.loss("tools", "hosted tool "+kind+" cannot be represented in Claude; it was omitted", true)
			continue
		}
		if rawPresent(tool["strict"]) {
			p.loss("tools.strict", "Responses strict function validation cannot be preserved", false)
		}
		kept = append(kept, tool)
	}
	return common.Marshal(kept)
}

func (p *claudeRequestConversion) responsesContent(raw json.RawMessage, path string) ([]dto.ClaudeMediaMessage, error) {
	if !rawPresent(raw) {
		return nil, nil
	}
	if common.GetJsonType(raw) == "string" {
		text := rawString(raw)
		return []dto.ClaudeMediaMessage{{Type: "text", Text: &text}}, nil
	}
	var parts []map[string]json.RawMessage
	if err := common.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("%s must be text or content parts", path)
	}
	blocks := make([]dto.ClaudeMediaMessage, 0, len(parts))
	for _, part := range parts {
		kind := rawString(part["type"])
		switch kind {
		case "text", "input_text", "output_text":
			text := rawString(part["text"])
			if text != "" {
				blocks = append(blocks, dto.ClaudeMediaMessage{Type: "text", Text: &text})
			}
		case "input_image", "input_file":
			url, mime := rawString(part["image_url"]), ""
			if kind == "input_file" {
				url = rawString(part["file_data"])
				if url == "" {
					url = rawString(part["file_url"])
				}
				mime = openAIFileMimeType(rawString(part["filename"]))
			} else if url == "" {
				var image map[string]json.RawMessage
				_ = common.Unmarshal(part["image_url"], &image)
				url = rawString(image["url"])
			}
			if url == "" {
				return nil, fmt.Errorf("%s: file_id references require a native upstream", path)
			}
			if strings.HasPrefix(url, "data:") {
				mime = ""
			}
			block, err := p.media(types.NewFileSourceFromData(url, mime), path)
			if err != nil {
				return nil, err
			}
			if block != nil {
				blocks = append(blocks, *block)
			}
		default:
			p.loss(path, "Claude cannot carry content part "+kind+"; it was omitted", true)
		}
	}
	return blocks, nil
}
