package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
)

const geminiResponsesCustomToolsKey = "gemini_responses_custom_tools"

func responsesString(v any) string { s, _ := v.(string); return s }
func responsesRawPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && strings.TrimSpace(string(raw)) != "null"
}

// ConvertResponsesToGemini keeps Responses input items intact instead of using
// Chat messages as an intermediate (which loses custom tools and tool media).
// The existing Chat converter supplies the shared model/Schema/thinking policy.
func ConvertResponsesToGemini(c *gin.Context, info *relaycommon.RelayInfo, req dto.OpenAIResponsesRequest) (*dto.GeminiChatRequest, error) {
	info.ResetConversionDiagnostics()
	c.Set(geminiResponsesCustomToolsKey, map[string]bool(nil))
	c.Set(geminiResponsesToolNamesKey, responsesToolNames(nil))
	if req.PreviousResponseID != "" || responsesRawPresent(req.Conversation) || responsesRawPresent(req.Prompt) || responsesRawPresent(req.ContextManagement) {
		return nil, fmt.Errorf("Gemini Responses requires inline input history; previous_response_id, conversation, prompt and context_management are unsupported")
	}
	if strings.HasPrefix(info.UpstreamModelName, "imagen") || strings.Contains(info.UpstreamModelName, "embedding") {
		return nil, fmt.Errorf("Gemini Responses requires a generateContent model")
	}
	pivot := dto.GeneralOpenAIRequest{Model: req.Model, Temperature: req.Temperature, TopP: req.TopP, MaxTokens: req.MaxOutputTokens}
	if req.Reasoning != nil {
		pivot.ReasoningEffort = req.Reasoning.Effort
	}
	if responsesRawPresent(req.Text) {
		var text struct {
			Format map[string]any `json:"format"`
		}
		if err := common.Unmarshal(req.Text, &text); err != nil {
			return nil, fmt.Errorf("invalid text: %w", err)
		}
		if text.Format != nil {
			kind := responsesString(text.Format["type"])
			switch kind {
			case "text", "":
			case "json_object", "json_schema":
				pivot.ResponseFormat = &dto.ResponseFormat{Type: kind}
				if kind == "json_schema" {
					if _, ok := text.Format["schema"].(map[string]any); !ok {
						return nil, fmt.Errorf("text.format.schema must be an object")
					}
					pivot.ResponseFormat.JsonSchema, _ = common.Marshal(text.Format)
				}
			default:
				return nil, fmt.Errorf("unsupported text.format.type %q", kind)
			}
		}
	}
	plan, err := prepareGeminiResponses(c, info, req)
	if err != nil {
		return nil, err
	}
	if plan.rejected != nil {
		return nil, plan.rejected
	}
	custom, toolNames := plan.custom, plan.names
	converted, err := CovertOpenAI2Gemini(c, pivot, info)
	if err != nil {
		return nil, err
	}
	// Preserve explicit zero, including top_p=0 and max_output_tokens=0.
	converted.GenerationConfig.TopP = req.TopP
	converted.GenerationConfig.MaxOutputTokens = req.MaxOutputTokens
	if req.Reasoning != nil {
		config := converted.GenerationConfig.ThinkingConfig
		if config == nil {
			config = &dto.GeminiThinkingConfig{}
		}
		switch effort := req.Reasoning.Effort; effort {
		case "":
		case "none":
			config.ThinkingLevel = ""
			config.ThinkingBudget = common.GetPointer(0)
			config.IncludeThoughts = common.GetPointer(false)
		case "minimal", "low", "medium", "high", "xhigh", "max":
			config.ThinkingBudget = nil
			config.ThinkingLevel = effort
			config.IncludeThoughts = common.GetPointer(true)
		default:
			return nil, fmt.Errorf("unsupported reasoning.effort %q", effort)
		}
		switch summary := req.Reasoning.Summary; summary {
		case "":
		case "auto", "concise", "detailed":
			config.IncludeThoughts = common.GetPointer(true)
		case "none":
			config.IncludeThoughts = common.GetPointer(false)
		default:
			return nil, fmt.Errorf("unsupported reasoning.summary %q", summary)
		}
		converted.GenerationConfig.ThinkingConfig = config
		if err := normalizeGeminiThinkingConfig(converted, info); err != nil {
			return nil, err
		}
	}
	// Responses JSON Schema constraints (including additionalProperties=false)
	// must survive on endpoints that accept the native JSON Schema fields.
	if geminiSupportsJSONSchema(info) {
		if pivot.ResponseFormat != nil && pivot.ResponseFormat.Type == "json_schema" {
			var format struct {
				Schema json.RawMessage `json:"schema"`
			}
			if err := common.Unmarshal(pivot.ResponseFormat.JsonSchema, &format); err != nil {
				return nil, err
			}
			converted.GenerationConfig.ResponseSchema = nil
			converted.GenerationConfig.ResponseJsonSchema = format.Schema
		}
	} else if pivot.ResponseFormat != nil && len(pivot.ResponseFormat.JsonSchema) > 0 {
		var format struct {
			Schema any `json:"schema"`
		}
		if err := common.Unmarshal(pivot.ResponseFormat.JsonSchema, &format); err != nil {
			return nil, err
		}
		plan.schemaLoss("text.format.schema", format.Schema)
	}
	if len(plan.tools) > 0 {
		converted.SetTools(plan.tools)
	}
	converted.ToolConfig = plan.choice
	var system []string
	if responsesRawPresent(req.Instructions) {
		var text string
		if err := common.Unmarshal(req.Instructions, &text); err != nil {
			return nil, fmt.Errorf("instructions must be a string")
		}
		system = append(system, text)
	}
	items := plan.items
	callNames := make(map[string]string)
	model := strings.TrimPrefix(strings.ToLower(info.UpstreamModelName), "models/")
	mediaInResponse := !strings.HasPrefix(model, "gemini-1.") && !strings.HasPrefix(model, "gemini-2.")
	for _, item := range items {
		kind := responsesString(item["type"])
		switch kind {
		case "function_call", "custom_tool_call":
			id, name := responsesCallID(item), responsesString(item["name"])
			if id == "" || name == "" {
				return nil, fmt.Errorf("%s requires call_id and name", kind)
			}
			name, err = toolNames.register(responsesString(item["namespace"]), name)
			if err != nil {
				return nil, err
			}
			var args map[string]any
			if kind == "custom_tool_call" {
				input, ok := item["input"].(string)
				if !ok {
					return nil, fmt.Errorf("custom_tool_call.input must be a string")
				}
				args = map[string]any{"input": input}
				if !plan.functions[name] {
					custom[name] = true
				}
			} else {
				raw, ok := item["arguments"].(string)
				if !ok || common.UnmarshalJsonStr(raw, &args) != nil || args == nil {
					return nil, fmt.Errorf("function_call.arguments must encode an object")
				}
			}
			part := dto.GeminiPart{FunctionCall: &dto.FunctionCall{ID: &id, FunctionName: name, Arguments: args}}
			if model_setting.GetGeminiSettings().FunctionCallThoughtSignatureEnabled {
				part.ThoughtSignature, _ = common.Marshal(thoughtSignatureBypassValue)
			}
			appendResponsesGeminiPart(converted, "model", part)
			callNames[id] = name
		case "function_call_output", "custom_tool_call_output":
			id := responsesCallID(item)
			name := responsesString(item["name"])
			if name == "" {
				name = callNames[id]
			} else {
				name, err = toolNames.register(responsesString(item["namespace"]), name)
				if err != nil {
					return nil, err
				}
			}
			if id == "" || name == "" {
				return nil, fmt.Errorf("%s requires call_id and a matching call or name", kind)
			}
			output, media, err := plan.toolOutput(item["output"], mediaInResponse)
			if err != nil {
				return nil, err
			}
			fr := &dto.GeminiFunctionResponse{Name: name, Response: output}
			fr.ID, _ = common.Marshal(id)
			// Gemini 3 supports images/documents in functionResponse.parts.
			// Older models receive media after the function responses; Gemini 3
			// toolOutput has already diagnosed unrepresentable audio/video parts.
			var nested, separate []dto.GeminiPart
			for _, part := range media {
				mime := part.InlineData.MimeType
				if mediaInResponse && !strings.HasPrefix(mime, "audio/") && !strings.HasPrefix(mime, "video/") {
					nested = append(nested, part)
				} else {
					separate = append(separate, part)
				}
			}
			if len(nested) > 0 {
				fr.Parts, _ = common.Marshal(nested)
			}
			appendResponsesGeminiPart(converted, "user", dto.GeminiPart{FunctionResponse: fr})
			for _, part := range separate {
				appendResponsesGeminiPart(converted, "user", part)
			}
		case "reasoning":
			// Provider-encrypted reasoning is not portable. Visible summaries are
			// not instructions or assistant answers and must not be replayed as such.
			continue
		case "", "message":
			role := responsesString(item["role"])
			if role == "" {
				role = "user"
			}
			parts, err := plan.content(item["content"])
			if err != nil {
				return nil, err
			}
			switch role {
			case "system", "developer":
				for _, part := range parts {
					if part.InlineData != nil {
						return nil, fmt.Errorf("Gemini system instructions must be text")
					}
					system = append(system, part.Text)
				}
			case "user", "assistant", "model":
				if role == "assistant" {
					role = "model"
				}
				if len(parts) > 0 {
					converted.Contents = append(converted.Contents, dto.GeminiChatContent{Role: role, Parts: parts})
				}
			default:
				return nil, fmt.Errorf("unsupported input role %q", role)
			}
		default:
			return nil, fmt.Errorf("unsupported Gemini Responses input item %q", kind)
		}
	}
	plan.omitTrailingAssistant(converted)
	if len(system) > 0 {
		converted.SystemInstructions = &dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: strings.Join(system, "\n")}}}
	}
	if len(converted.Contents) == 0 {
		return nil, fmt.Errorf("input contains no Gemini content")
	}
	if plan.rejected != nil {
		return nil, plan.rejected
	}
	c.Set(geminiResponsesCustomToolsKey, custom)
	c.Set(geminiResponsesToolNamesKey, toolNames)
	return converted, nil
}

func appendResponsesGeminiPart(req *dto.GeminiChatRequest, role string, part dto.GeminiPart) {
	if len(req.Contents) == 0 || req.Contents[len(req.Contents)-1].Role != role {
		req.Contents = append(req.Contents, dto.GeminiChatContent{Role: role})
	}
	turn := &req.Contents[len(req.Contents)-1]
	index := len(turn.Parts)
	if part.FunctionCall != nil {
		index = 0
		for index < len(turn.Parts) && turn.Parts[index].FunctionCall != nil {
			index++
		}
	} else if part.FunctionResponse != nil {
		// Keep all function responses before the turn's standalone media.
		for i, p := range turn.Parts {
			if p.FunctionResponse != nil {
				index = i + 1
			}
		}
	}
	turn.Parts = append(turn.Parts, dto.GeminiPart{})
	copy(turn.Parts[index+1:], turn.Parts[index:])
	turn.Parts[index] = part
}
