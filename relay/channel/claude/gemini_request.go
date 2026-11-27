package claude

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/claudethinking"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func ConvertGeminiToClaude(c *gin.Context, info *relaycommon.RelayInfo, req *dto.GeminiChatRequest) (*dto.ClaudeRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	p, err := newClaudeConversion(c, info)
	if err != nil {
		return nil, err
	}
	if req.CachedContent != "" {
		return nil, fmt.Errorf("cachedContent requires a native Gemini upstream")
	}
	if len(req.Requests) > 0 {
		return nil, fmt.Errorf("Gemini batch requests cannot be converted to Claude Messages")
	}
	config := req.GenerationConfig
	if err = validateClaudeMaxTokens(config.MaxOutputTokens); err != nil {
		return nil, err
	}
	out := &dto.ClaudeRequest{Model: info.UpstreamModelName, Stream: common.GetPointer(info.IsStream), Temperature: config.Temperature, TopP: config.TopP, MaxTokens: config.MaxOutputTokens, StopSequences: config.StopSequences}
	if out.Model == "" {
		return nil, fmt.Errorf("upstream model is required")
	}
	if config.TopK != nil {
		v := *config.TopK
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > math.MaxInt32 || math.Trunc(v) != v {
			return nil, fmt.Errorf("topK must be a non-negative bounded integer")
		}
		out.TopK = common.GetPointer(int(v))
	}
	if config.CandidateCount != nil && *config.CandidateCount != 1 {
		p.loss("generationConfig.candidateCount", "Claude returns one candidate", true)
	}
	if len(req.SafetySettings) > 0 {
		p.loss("safetySettings", "Gemini safety settings cannot be enforced by Claude", true)
	}
	if config.ResponseMimeType != "" || config.ResponseSchema != nil || rawPresent(config.ResponseJsonSchema) {
		p.loss("generationConfig.responseSchema", "Gemini output format constraints cannot be preserved by this Claude conversion", true)
	}
	if len(config.ResponseModalities) > 0 || rawPresent(config.SpeechConfig) || rawPresent(config.ImageConfig) {
		p.loss("generationConfig", "Gemini output modality controls cannot be represented in Claude", true)
	}
	if config.Seed != nil || config.PresencePenalty != nil || config.FrequencyPenalty != nil || config.Logprobs != nil || config.ResponseLogprobs != nil {
		p.loss("generationConfig", "Gemini seed, penalties and log probabilities are unsupported by Claude", false)
	}
	if out.MaxTokens != nil && *out.MaxTokens == 0 {
		out.MaxTokens = nil
	}
	if err = claudethinking.ApplyGemini(c, out, req, info); err != nil {
		return nil, err
	}
	if req.SystemInstructions != nil {
		for _, part := range req.SystemInstructions.Parts {
			if part.Text != "" {
				appendClaudeBlocks(out, "system", dto.ClaudeMediaMessage{Type: "text", Text: common.GetPointer(part.Text)})
			}
			if part.InlineData != nil || part.FileData != nil || part.FunctionCall != nil || part.FunctionResponse != nil {
				p.loss("systemInstruction", "Claude system instructions only accept text", true)
			}
		}
	}
	// Match ID-less results against pending calls by name in encounter order.
	type pendingCall struct {
		id, name string
		consumed bool
	}
	var pending []pendingCall
	seenIDs := map[string]bool{}
	for ci, content := range req.Contents {
		role := "user"
		if content.Role == "model" {
			role = "assistant"
		}
		for pi, part := range content.Parts {
			path := fmt.Sprintf("contents[%d].parts[%d]", ci, pi)
			switch {
			case part.FunctionCall != nil:
				call := part.FunctionCall
				if call.FunctionName == "" {
					return nil, fmt.Errorf("%s function call requires name", path)
				}
				id := ""
				if call.ID != nil {
					id = *call.ID
				}
				if id == "" {
					id = "call_" + common.GetUUID()
				}
				if seenIDs[id] {
					return nil, fmt.Errorf("duplicate Gemini function call id %q", id)
				}
				seenIDs[id] = true
				pending = append(pending, pendingCall{id: id, name: call.FunctionName})
				input := call.Arguments
				if input == nil {
					input = map[string]any{}
				}
				raw, err := common.Marshal(input)
				if err != nil {
					return nil, err
				}
				if common.GetJsonType(raw) != "object" {
					return nil, fmt.Errorf("function arguments must be an object")
				}
				if len(call.PartialArgs) > 0 || call.WillContinue != nil && *call.WillContinue {
					return nil, fmt.Errorf("partial Gemini function calls cannot be replayed")
				}
				appendClaudeBlocks(out, "assistant", dto.ClaudeMediaMessage{Type: "tool_use", Id: id, Name: call.FunctionName, Input: json.RawMessage(raw)})
			case part.FunctionResponse != nil:
				result := part.FunctionResponse
				id := rawString(result.ID)
				match := -1
				for i, call := range pending {
					if !call.consumed && (id != "" && call.id == id || id == "" && call.name == result.Name) {
						match = i
						break
					}
				}
				if match < 0 {
					return nil, fmt.Errorf("%s has no matching function call", path)
				}
				if result.Name != "" && result.Name != pending[match].name {
					return nil, fmt.Errorf("%s function name does not match call id", path)
				}
				pending[match].consumed = true
				id = pending[match].id
				text, err := common.Marshal(result.Response)
				if err != nil {
					return nil, err
				}
				blocks := []dto.ClaudeMediaMessage{{Type: "text", Text: common.GetPointer(string(text))}}
				if rawPresent(result.Parts) {
					var media []dto.GeminiPart
					if err = common.Unmarshal(result.Parts, &media); err != nil {
						return nil, err
					}
					for _, m := range media {
						block, err := p.geminiMedia(m, path)
						if err != nil {
							return nil, err
						}
						if block != nil {
							blocks = append(blocks, *block)
						}
					}
				}
				if rawPresent(result.Scheduling) || rawPresent(result.WillContinue) {
					p.loss(path, "Gemini asynchronous function response controls cannot be preserved", true)
				}
				appendClaudeBlocks(out, "user", dto.ClaudeMediaMessage{Type: "tool_result", ToolUseId: id, Content: blocks})
			case part.Thought:
				p.loss(path, "Gemini thoughts cannot be replayed as signed Claude thinking; the thought was omitted", false)
			case part.Text != "":
				appendClaudeBlocks(out, role, dto.ClaudeMediaMessage{Type: "text", Text: common.GetPointer(part.Text)})
			case part.InlineData != nil || part.FileData != nil:
				block, err := p.geminiMedia(part, path)
				if err != nil {
					return nil, err
				}
				if block != nil {
					appendClaudeBlocks(out, role, *block)
				}
			case part.ExecutableCode != nil || part.CodeExecutionResult != nil:
				p.loss(path, "Gemini hosted execution history cannot be replayed in Claude", true)
			}
			if rawPresent(part.ThoughtSignature) {
				p.loss(path+".thoughtSignature", "Gemini thought signatures cannot be reused by Claude", false)
			}
		}
	}
	tools, err := p.geminiTools(req.Tools)
	if err != nil {
		return nil, err
	}
	if len(tools) > 0 {
		out.Tools = tools
	}
	if req.ToolConfig != nil && req.ToolConfig.FunctionCallingConfig != nil {
		fc := req.ToolConfig.FunctionCallingConfig
		declared := make(map[string]bool, len(tools))
		for _, tool := range tools {
			declared[tool.(dto.Tool).Name] = true
		}
		for _, name := range fc.AllowedFunctionNames {
			if !declared[name] {
				return nil, fmt.Errorf("allowedFunctionNames references undeclared function %q", name)
			}
		}
		switch strings.ToUpper(string(fc.Mode)) {
		case "", "AUTO":
			out.ToolChoice = &dto.ClaudeToolChoice{Type: "auto"}
		case "NONE":
			out.ToolChoice = &dto.ClaudeToolChoice{Type: "none"}
		case "ANY", "VALIDATED":
			if strings.EqualFold(string(fc.Mode), "VALIDATED") {
				p.loss("toolConfig", "Gemini VALIDATED function calling has no exact Claude equivalent", true)
			}
			out.ToolChoice = &dto.ClaudeToolChoice{Type: "any"}
			if len(fc.AllowedFunctionNames) == 1 {
				out.ToolChoice = &dto.ClaudeToolChoice{Type: "tool", Name: fc.AllowedFunctionNames[0]}
			}
			if len(fc.AllowedFunctionNames) > 1 {
				allowed := map[string]bool{}
				for _, n := range fc.AllowedFunctionNames {
					allowed[n] = true
				}
				filtered := make([]any, 0)
				for _, t := range tools {
					if allowed[t.(dto.Tool).Name] {
						filtered = append(filtered, t)
					}
				}
				if len(filtered) == 0 {
					return nil, fmt.Errorf("allowedFunctionNames does not match any declared function")
				}
				out.Tools = filtered
			}
		default:
			return nil, fmt.Errorf("unsupported function calling mode %q", fc.Mode)
		}
	}
	if req.ToolConfig != nil && (req.ToolConfig.RetrievalConfig != nil || req.ToolConfig.IncludeServerSideToolInvocations != nil) {
		p.loss("toolConfig", "Gemini retrieval and server-side invocation controls cannot be represented in Claude", true)
	}
	if p.err != nil {
		return nil, p.err
	}
	if err = finishClaudeRequest(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *claudeRequestConversion) geminiMedia(part dto.GeminiPart, path string) (*dto.ClaudeMediaMessage, error) {
	if part.Text != "" {
		return &dto.ClaudeMediaMessage{Type: "text", Text: common.GetPointer(part.Text)}, nil
	}
	var source types.FileSource
	if part.InlineData != nil {
		source = part.InlineData.ToFileSource()
	} else if part.FileData != nil {
		source = types.NewFileSourceFromData(part.FileData.FileUri, part.FileData.MimeType)
	}
	return p.media(source, path)
}

func (p *claudeRequestConversion) geminiTools(raw json.RawMessage) ([]any, error) {
	if !rawPresent(raw) {
		return nil, nil
	}
	if common.GetJsonType(raw) == "object" {
		raw = append(append([]byte{'['}, raw...), ']')
	}
	var tools []map[string]json.RawMessage
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil, err
	}
	var result []any
	seenNames := map[string]bool{}
	for _, tool := range tools {
		for k := range tool {
			if k != "functionDeclarations" {
				p.loss("tools."+k, "Gemini hosted tool cannot be represented in Claude; it was omitted", true)
			}
		}
		var functions []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
			Schema      json.RawMessage `json:"parametersJsonSchema"`
		}
		if !rawPresent(tool["functionDeclarations"]) {
			continue
		}
		if err := common.Unmarshal(tool["functionDeclarations"], &functions); err != nil {
			return nil, err
		}
		for _, fn := range functions {
			if fn.Name == "" {
				return nil, fmt.Errorf("function declaration requires name")
			}
			if seenNames[fn.Name] {
				return nil, fmt.Errorf("duplicate function declaration %q", fn.Name)
			}
			seenNames[fn.Name] = true
			schema := fn.Schema
			if !rawPresent(schema) {
				var err error
				schema, err = geminiSchemaToClaude(fn.Parameters)
				if err != nil {
					return nil, err
				}
			}
			if !rawPresent(schema) {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			schema, err := claudeFunctionSchema(schema)
			if err != nil {
				return nil, err
			}
			result = append(result, dto.Tool{Name: fn.Name, Description: fn.Description, InputSchema: schema})
		}
	}
	return result, nil
}

// Patch schema keywords in place so property order and numeric tokens survive.
func geminiSchemaToClaude(raw json.RawMessage) (json.RawMessage, error) {
	if !rawPresent(raw) {
		return raw, nil
	}
	var schema map[string]json.RawMessage
	if err := common.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	var err error
	if kind := rawString(schema["type"]); kind != "" {
		var renderedType any = strings.ToLower(kind)
		if gjson.GetBytes(raw, "nullable").Bool() {
			renderedType = []string{strings.ToLower(kind), "null"}
		}
		raw, err = sjson.SetBytes(raw, "type", renderedType)
		if err != nil {
			return nil, err
		}
		raw, err = sjson.DeleteBytes(raw, "nullable")
		if err != nil {
			return nil, err
		}
	}
	for _, key := range []string{"properties", "$defs", "definitions"} {
		obj := gjson.GetBytes(raw, key)
		obj.ForEach(func(k, v gjson.Result) bool {
			child, e := geminiSchemaToClaude(json.RawMessage(v.Raw))
			if e != nil {
				err = e
				return false
			}
			path := key + "." + strings.NewReplacer("\\", "\\\\", ".", "\\.", "*", "\\*", "?", "\\?").Replace(k.String())
			raw, err = sjson.SetRawBytes(raw, path, child)
			return err == nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, key := range []string{"items", "additionalProperties"} {
		if common.GetJsonType(schema[key]) == "object" {
			child, e := geminiSchemaToClaude(schema[key])
			if e != nil {
				return nil, e
			}
			raw, err = sjson.SetRawBytes(raw, key, child)
			if err != nil {
				return nil, err
			}
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		var children []json.RawMessage
		if !rawPresent(schema[key]) {
			continue
		}
		if err = common.Unmarshal(schema[key], &children); err != nil {
			return nil, err
		}
		for i, child := range children {
			children[i], err = geminiSchemaToClaude(child)
			if err != nil {
				return nil, err
			}
		}
		encoded, e := common.Marshal(children)
		if e != nil {
			return nil, e
		}
		raw, err = sjson.SetRawBytes(raw, key, encoded)
		if err != nil {
			return nil, err
		}
	}
	return raw, nil
}
