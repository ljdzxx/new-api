package advancedcustom

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type conversion struct {
	c        *gin.Context
	info     *relaycommon.RelayInfo
	rejected error
}

func (p *conversion) loss(code, path, message string, semantic bool) {
	severity := types.ConversionDiagnosticWarning
	if semantic {
		severity = types.ConversionDiagnosticError
	}
	d := types.ConversionDiagnostic{Code: code, Path: path, Message: message, Severity: severity, From: types.RelayFormatGemini, To: types.RelayFormatOpenAIResponses}
	p.info.RecordConversionDiagnostics(p.c, []types.ConversionDiagnostic{d})
	policy := p.info.ChannelOtherSettings.ToolLossPolicy
	if p.rejected == nil && (policy == types.ConversionLossPolicyStrict || policy == types.ConversionLossPolicySafe && semantic) {
		p.rejected = &types.ConversionLossError{Diagnostics: []types.ConversionDiagnostic{d}}
	}
}

func convertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (*dto.OpenAIResponsesRequest, error) {
	info.ResetConversionDiagnostics()
	// Retry attempts must not inherit search counts or emitted-stream state.
	info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{}
	info.UpstreamUsageReported = false
	c.Set(StreamStartedKey, false)
	p := &conversion{c: c, info: info}
	config := request.GenerationConfig
	out := &dto.OpenAIResponsesRequest{Model: info.UpstreamModelName, Stream: common.GetPointer(info.IsStream), Temperature: config.Temperature, TopP: config.TopP, MaxOutputTokens: config.MaxOutputTokens}
	if request.CachedContent != "" {
		p.loss("unsupported_cached_content", "cachedContent", "Responses cannot resolve Gemini cachedContent", true)
	}
	if len(request.SafetySettings) > 0 {
		p.loss("unsupported_safety_settings", "safetySettings", "Responses does not expose Gemini safety thresholds", true)
	}
	if config.CandidateCount != nil && *config.CandidateCount != 1 {
		p.loss("unsupported_candidate_count", "generationConfig.candidateCount", "Responses generates one candidate", true)
	}
	if config.TopK != nil || config.Seed != nil || len(config.StopSequences) > 0 || config.PresencePenalty != nil || config.FrequencyPenalty != nil {
		p.loss("unsupported_generation_parameters", "generationConfig", "Responses cannot represent topK, seed, stop sequences or penalties", true)
	}
	if config.ResponseLogprobs != nil || config.Logprobs != nil || config.EnableEnhancedCivicAnswers != nil {
		p.loss("unsupported_generation_parameters", "generationConfig", "Gemini logprob and civic-answer controls are not portable", true)
	}
	nonTextOutput := false
	for _, modality := range config.ResponseModalities {
		nonTextOutput = nonTextOutput || !strings.EqualFold(modality, "TEXT")
	}
	if nonTextOutput || len(config.SpeechConfig) > 0 || len(config.ImageConfig) > 0 {
		p.loss("unsupported_output_modality", "generationConfig", "Gemini output modality controls cannot be forwarded to Responses", true)
	}
	if config.ThinkingConfig != nil {
		thinking := config.ThinkingConfig
		effort := strings.ToLower(thinking.ThinkingLevel)
		if effort == "minimal" {
			effort = "low"
		}
		if thinking.ThinkingBudget != nil {
			switch budget := *thinking.ThinkingBudget; {
			case budget == 0:
				effort = "none"
			case budget > 0 && budget <= 1024:
				effort = "low"
			case budget > 0 && budget <= 8192:
				effort = "medium"
			case budget > 8192:
				effort = "high"
			}
			p.loss("thinking_budget_approximated", "generationConfig.thinkingConfig", "Gemini thinking budget is approximated by Responses reasoning effort", false)
		}
		if effort != "" {
			out.Reasoning = &dto.Reasoning{Effort: effort}
		}
		if thinking.IncludeThoughts != nil && *thinking.IncludeThoughts {
			if out.Reasoning == nil {
				out.Reasoning = &dto.Reasoning{}
			}
			out.Reasoning.Summary = "auto"
		}
	}
	if len(config.ResponseJsonSchema) > 0 {
		var schema any
		if err := common.Unmarshal(config.ResponseJsonSchema, &schema); err != nil {
			return nil, err
		}
		out.Text = raw(map[string]any{"format": map[string]any{"type": "json_schema", "name": "response", "schema": schema}})
	} else if config.ResponseSchema != nil {
		out.Text = raw(map[string]any{"format": map[string]any{"type": "json_schema", "name": "response", "schema": p.schema(config.ResponseSchema, "generationConfig.responseSchema")}})
	} else if config.ResponseMimeType == "application/json" {
		out.Text = raw(map[string]any{"format": map[string]any{"type": "json_object"}})
	}
	var tools []any
	functions := map[string]bool{}
	for _, tool := range request.GetTools() {
		if tool.FunctionDeclarations != nil {
			var declarations []dto.GeminiFunctionDeclaration
			if err := common.Unmarshal(raw(tool.FunctionDeclarations), &declarations); err != nil {
				return nil, err
			}
			for _, fn := range declarations {
				if fn.Name == "" || functions[fn.Name] {
					return nil, fmt.Errorf("function names must be nonempty and unique")
				}
				functions[fn.Name] = true
				schema := fn.ParametersJsonSchema
				if schema == nil {
					schema = p.schema(fn.Parameters, "tools."+fn.Name+".parameters")
				}
				if schema == nil {
					schema = map[string]any{"type": "object", "properties": map[string]any{}}
				}
				tools = append(tools, map[string]any{"type": "function", "name": fn.Name, "description": fn.Description, "parameters": schema})
			}
		}
		if tool.GoogleSearch != nil || tool.GoogleSearchRetrieval != nil {
			if options, ok := tool.GoogleSearch.(map[string]any); ok && len(options) > 0 {
				p.loss("unsupported_search_constraints", "tools.googleSearch", "Gemini search options are not portable to Responses", true)
			}
			found := false
			for _, t := range tools {
				if m, ok := t.(map[string]any); ok && m["type"] == "web_search" {
					found = true
				}
			}
			if !found {
				tools = append(tools, map[string]any{"type": "web_search"})
			}
			if tool.GoogleSearchRetrieval != nil {
				p.loss("search_options_approximated", "tools.googleSearchRetrieval", "Gemini retrieval settings use Responses web search defaults", false)
			}
		}
		if tool.CodeExecution != nil || tool.URLContext != nil || len(tool.GoogleMaps) > 0 || len(tool.EnterpriseWebSearch) > 0 || len(tool.FileSearch) > 0 || len(tool.ComputerUse) > 0 || len(tool.Retrieval) > 0 {
			p.loss("unsupported_hosted_tool", "tools", "Gemini hosted tool has no Responses equivalent in this converter", true)
		}
	}
	if len(tools) > 0 {
		out.Tools = raw(tools)
	}
	if request.ToolConfig != nil && request.ToolConfig.FunctionCallingConfig != nil {
		choice := request.ToolConfig.FunctionCallingConfig
		switch strings.ToUpper(string(choice.Mode)) {
		case "", "AUTO":
			out.ToolChoice = raw("auto")
			if len(choice.AllowedFunctionNames) > 0 {
				p.loss("unsupported_tool_choice_subset", "toolConfig", "Allowed function names cannot constrain automatic Responses tool selection", true)
			}
		case "NONE":
			out.ToolChoice = raw("none")
		case "ANY", "VALIDATED":
			if string(choice.Mode) == "VALIDATED" {
				p.loss("tool_choice_approximated", "toolConfig", "VALIDATED mode is approximated by required tool choice", true)
			}
			if len(functions) == 0 {
				return nil, fmt.Errorf("function calling requires function declarations")
			}
			if len(choice.AllowedFunctionNames) == 1 {
				name := choice.AllowedFunctionNames[0]
				if !functions[name] {
					return nil, fmt.Errorf("tool choice names an undeclared function")
				}
				out.ToolChoice = raw(map[string]any{"type": "function", "name": name})
			} else {
				out.ToolChoice = raw("required")
				if len(choice.AllowedFunctionNames) > 1 {
					p.loss("unsupported_tool_choice_subset", "toolConfig", "Responses converter cannot force a subset of multiple functions", true)
				}
			}
		default:
			return nil, fmt.Errorf("unsupported Gemini function calling mode")
		}
	}
	if p.rejected != nil {
		return nil, p.rejected
	}
	var input []any
	if request.SystemInstructions != nil {
		var text []string
		for _, part := range request.SystemInstructions.Parts {
			if part.Text != "" {
				text = append(text, part.Text)
			} else {
				p.loss("unsupported_system_media", "systemInstruction", "Only text system instructions can be converted", true)
			}
		}
		if len(text) > 0 {
			out.Instructions = raw(strings.Join(text, "\n"))
		}
	}
	// Match result names to calls when older Gemini clients omit IDs. Queues
	// preserve parallel calls with the same function name.
	pending := map[string][]string{}
	callIDs := map[string]bool{}
	callIndex := 0
	for ci, content := range request.Contents {
		role := "user"
		if content.Role == "model" {
			role = "assistant"
		}
		var message []any
		flush := func() {
			if len(message) > 0 {
				input = append(input, map[string]any{"type": "message", "role": role, "content": message})
				message = nil
			}
		}
		for pi, part := range content.Parts {
			path := fmt.Sprintf("contents[%d].parts[%d]", ci, pi)
			if part.Thought || len(part.ThoughtSignature) > 0 {
				p.loss("unsupported_thought_signature", path, "Gemini thought/signature metadata is not portable to Responses", false)
			}
			if part.Thought {
				continue
			}
			if part.FunctionCall != nil {
				flush()
				fn := part.FunctionCall
				if len(fn.PartialArgs) > 0 || fn.WillContinue != nil && *fn.WillContinue {
					return nil, fmt.Errorf("request history requires complete function arguments")
				}
				id := ""
				if fn.ID != nil {
					id = *fn.ID
				}
				if id == "" {
					id = fmt.Sprintf("call_gemini_%d", callIndex)
				}
				callIndex++
				if callIDs[id] {
					return nil, fmt.Errorf("duplicate function call ID")
				}
				callIDs[id] = true
				if fn.FunctionName == "" {
					return nil, fmt.Errorf("function call has no name")
				}
				pending[fn.FunctionName] = append(pending[fn.FunctionName], id)
				args := fn.Arguments
				if args == nil {
					args = map[string]any{}
				}
				input = append(input, map[string]any{"type": "function_call", "name": fn.FunctionName, "call_id": id, "arguments": string(raw(args))})
				continue
			}
			if part.FunctionResponse != nil {
				flush()
				result := part.FunctionResponse
				if len(result.WillContinue) > 0 || len(result.Scheduling) > 0 {
					p.loss("unsupported_tool_scheduling", path, "Gemini asynchronous tool-result controls are not portable", true)
				}
				var id string
				_ = common.Unmarshal(result.ID, &id)
				queue := pending[result.Name]
				if id == "" && len(queue) > 0 {
					id = queue[0]
					pending[result.Name] = queue[1:]
				} else if id != "" {
					for j, q := range queue {
						if q == id {
							pending[result.Name] = append(queue[:j], queue[j+1:]...)
							break
						}
					}
				}
				if id == "" {
					return nil, fmt.Errorf("function response cannot be paired with a call")
				}
				var media []dto.GeminiPart
				if len(result.Parts) > 0 {
					if err := common.Unmarshal(result.Parts, &media); err != nil {
						return nil, err
					}
				}
				var output any = string(raw(result.Response))
				if len(media) > 0 {
					parts := []any{map[string]any{"type": "input_text", "text": output}}
					for _, m := range media {
						v, err := p.media(m, path)
						if err != nil {
							return nil, err
						}
						if v != nil {
							parts = append(parts, v)
						}
					}
					output = parts
				}
				input = append(input, map[string]any{"type": "function_call_output", "call_id": id, "output": output})
				continue
			}
			if part.Text != "" {
				kind := "input_text"
				if role == "assistant" {
					kind = "output_text"
				}
				message = append(message, map[string]any{"type": kind, "text": part.Text})
			}
			if part.InlineData != nil || part.FileData != nil {
				media, err := p.media(part, path)
				if err != nil {
					return nil, err
				}
				if media != nil {
					if role == "assistant" {
						p.loss("unsupported_assistant_media", path, "Responses assistant history supports text; media omitted", true)
					} else {
						message = append(message, media)
					}
				}
			}
			if part.ExecutableCode != nil || part.CodeExecutionResult != nil {
				p.loss("unsupported_code_execution_history", path, "Gemini code execution history cannot be replayed", true)
			}
		}
		flush()
	}
	if p.rejected != nil {
		return nil, p.rejected
	}
	if len(input) == 0 {
		return nil, fmt.Errorf("converted input is empty")
	}
	out.Input = raw(input)
	return out, nil
}

func (p *conversion) media(part dto.GeminiPart, path string) (any, error) {
	data, mime := "", ""
	if part.InlineData != nil {
		data, mime = part.InlineData.Data, part.InlineData.MimeType
	} else if part.FileData != nil {
		uri := part.FileData.FileUri
		mime = part.FileData.MimeType
		if !strings.HasPrefix(uri, "https://") && !strings.HasPrefix(uri, "http://") {
			p.loss("unsupported_media_type", path, "Provider-owned file URI is not portable", true)
			return nil, nil
		}
		var err error
		data, mime, err = service.GetBase64Data(p.c, types.NewFileSourceFromData(uri, mime), "gemini_to_responses")
		if err != nil {
			return nil, fmt.Errorf("load Gemini media: %w", err)
		}
	}
	if data == "" {
		return nil, nil
	}
	switch {
	case strings.HasPrefix(mime, "image/"):
		return map[string]any{"type": "input_image", "image_url": "data:" + mime + ";base64," + data, "detail": "auto"}, nil
	case mime == "application/pdf":
		return map[string]any{"type": "input_file", "filename": "document.pdf", "file_data": "data:" + mime + ";base64," + data}, nil
	case strings.HasPrefix(mime, "text/") || mime == "application/json":
		// Use the shared decoder so malformed base64 fails before transmission.
		bytes, err := base64.StdEncoding.DecodeString(data)
		if err != nil || !utf8.Valid(bytes) {
			return nil, fmt.Errorf("invalid UTF-8 text file")
		}
		return map[string]any{"type": "input_text", "text": string(bytes)}, nil
	default:
		p.loss("unsupported_media_type", path, "Responses converter cannot carry this media type", true)
		return nil, nil
	}
}

// Gemini typed Schema uses uppercase types and nullable. JSON Schema fields
// already use JSON Schema and are sent without this rewrite.
func (p *conversion) schema(value any, path string) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			switch key {
			case "propertyOrdering":
				p.loss("gemini_schema_keyword_dropped", path+"."+key, "JSON Schema has no propertyOrdering keyword", false)
			case "nullable":
			case "type":
				if name, ok := item.(string); ok {
					out[key] = strings.ToLower(name)
				} else {
					out[key] = item
				}
			case "properties", "$defs", "definitions":
				if entries, ok := item.(map[string]any); ok {
					properties := map[string]any{}
					for name, child := range entries {
						properties[name] = p.schema(child, path+"."+name)
					}
					out[key] = properties
				}
			case "items", "anyOf", "allOf", "oneOf", "not", "additionalProperties":
				out[key] = p.schema(item, path+"."+key)
			default:
				out[key] = item
			}
		}
		if nullable, _ := v["nullable"].(bool); nullable {
			if name, ok := out["type"].(string); ok {
				out["type"] = []string{name, "null"}
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = p.schema(item, path)
		}
		return out
	default:
		return value
	}
}
