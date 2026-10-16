package gemini

import (
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// A request owns one state. Output indices and item IDs remain stable from the
// first delta through the terminal response. Usage is attached only at the end.
type geminiResponsesState struct {
	id             string
	model          string
	created        int64
	sequence       int
	output         []map[string]any
	textIndex      int
	reasoningIndex int
	text           string
	reasoning      string
	annotations    []any
	grounding      geminiGroundingStreamState
	custom         map[string]bool
	toolNames      responsesToolNames
	finish         string
	started        bool
	emit           func(string, map[string]any) error
	c              *gin.Context
	info           *relaycommon.RelayInfo
}

func newGeminiResponsesState(c *gin.Context, info *relaycommon.RelayInfo, emit func(string, map[string]any) error) *geminiResponsesState {
	s := &geminiResponsesState{id: "resp_" + common.GetUUID(), model: info.UpstreamModelName, created: common.GetTimestamp(), output: make([]map[string]any, 0), textIndex: -1, reasoningIndex: -1, annotations: make([]any, 0), emit: emit}
	s.c, s.info = c, info
	s.grounding.seen = make(map[string]struct{})
	if value, ok := c.Get(geminiResponsesCustomToolsKey); ok {
		s.custom, _ = value.(map[string]bool)
	}
	if value, ok := c.Get(geminiResponsesToolNamesKey); ok {
		s.toolNames, _ = value.(responsesToolNames)
	}
	return s
}

func (s *geminiResponsesState) event(kind string, payload map[string]any) error {
	if s.emit == nil {
		return nil
	}
	payload["type"] = kind
	payload["sequence_number"] = s.sequence
	s.sequence++
	return s.emit(kind, payload)
}

func responsesGeminiUsage(usage *dto.Usage) map[string]any {
	return map[string]any{
		"input_tokens": usage.PromptTokens, "output_tokens": usage.CompletionTokens, "total_tokens": usage.TotalTokens,
		"input_tokens_details":  map[string]any{"cached_tokens": usage.PromptTokensDetails.CachedTokens, "text_tokens": usage.PromptTokensDetails.TextTokens, "image_tokens": usage.PromptTokensDetails.ImageTokens, "audio_tokens": usage.PromptTokensDetails.AudioTokens},
		"output_tokens_details": map[string]any{"reasoning_tokens": usage.CompletionTokenDetails.ReasoningTokens, "text_tokens": usage.CompletionTokenDetails.TextTokens, "image_tokens": usage.CompletionTokenDetails.ImageTokens, "audio_tokens": usage.CompletionTokenDetails.AudioTokens},
	}
}

func (s *geminiResponsesState) response(status string, usage *dto.Usage) map[string]any {
	// Failure responses also carry the partial output already sent as deltas.
	if s.textIndex >= 0 {
		item := s.output[s.textIndex]
		item["content"] = []any{map[string]any{"type": "output_text", "text": s.text, "annotations": s.annotations}}
		if status == "failed" {
			item["status"] = "incomplete"
		}
	}
	if s.reasoningIndex >= 0 {
		s.output[s.reasoningIndex]["summary"] = []any{map[string]any{"type": "summary_text", "text": s.reasoning}}
	}
	resp := map[string]any{"id": s.id, "object": "response", "created_at": s.created, "model": s.model, "status": status, "output": s.output, "error": nil, "incomplete_details": nil, "usage": nil, "store": false}
	if usage != nil {
		resp["usage"] = responsesGeminiUsage(usage)
	}
	if status == "incomplete" {
		reason := "content_filter"
		if s.finish == "MAX_TOKENS" {
			reason = "max_output_tokens"
		}
		resp["incomplete_details"] = map[string]any{"reason": reason}
	}
	return resp
}

func (s *geminiResponsesState) start() error {
	if s.started {
		return nil
	}
	s.started = true
	if err := s.event("response.created", map[string]any{"response": s.response("in_progress", nil)}); err != nil {
		return err
	}
	return s.event("response.in_progress", map[string]any{"response": s.response("in_progress", nil)})
}

func (s *geminiResponsesState) add(item map[string]any) (int, error) {
	index := len(s.output)
	s.output = append(s.output, item)
	return index, s.event("response.output_item.added", map[string]any{"output_index": index, "item": item})
}

func (s *geminiResponsesState) textDelta(text string, thought bool) error {
	index := s.textIndex
	prefix, partKind, indexKey := "response.output_text", "output_text", "content_index"
	if thought {
		index = s.reasoningIndex
		prefix = "response.reasoning_summary_text"
		partKind = "summary_text"
		indexKey = "summary_index"
	}
	if index < 0 {
		item := map[string]any{"type": "message", "id": "msg_" + common.GetUUID(), "role": "assistant", "status": "in_progress", "content": []any{}}
		if thought {
			item = map[string]any{"type": "reasoning", "id": "rs_" + common.GetUUID(), "summary": []any{}}
		}
		var err error
		index, err = s.add(item)
		if err != nil {
			return err
		}
		part := map[string]any{"type": partKind, "text": ""}
		kind := "response.content_part.added"
		if thought {
			s.reasoningIndex = index
			kind = "response.reasoning_summary_part.added"
		} else {
			s.textIndex = index
			part["annotations"] = []any{}
		}
		if err = s.event(kind, map[string]any{"item_id": item["id"], "output_index": index, indexKey: 0, "part": part}); err != nil {
			return err
		}
	}
	if thought {
		s.reasoning += text
	} else {
		s.text += text
		s.grounding.rendered.WriteString(text)
	}
	return s.event(prefix+".delta", map[string]any{"item_id": s.output[index]["id"], "output_index": index, indexKey: 0, "delta": text})
}

func (s *geminiResponsesState) functionCall(call *dto.FunctionCall) error {
	if call.FunctionName == "" {
		return fmt.Errorf("Gemini function call is missing name")
	}
	id := geminiCallID(call.ID)
	if id == "" {
		id = "call_" + common.GetUUID()
	}
	args := call.Arguments
	if args == nil {
		args = map[string]any{}
	}
	if _, ok := args.(map[string]any); !ok {
		return fmt.Errorf("Gemini function arguments must be an object")
	}
	encoded, err := common.Marshal(args)
	if err != nil {
		return err
	}
	kind, field, prefix, value := "function_call", "arguments", "response.function_call_arguments", string(encoded)
	if s.custom[call.FunctionName] {
		var obj map[string]any
		if common.Unmarshal(encoded, &obj) != nil {
			return fmt.Errorf("invalid custom tool arguments")
		}
		var ok bool
		value, ok = obj["input"].(string)
		if !ok {
			return fmt.Errorf("custom tool arguments require string input")
		}
		kind, field, prefix = "custom_tool_call", "input", "response.custom_tool_call_input"
	}
	item := map[string]any{"type": kind, "id": "fc_" + common.GetUUID(), "call_id": id, "name": call.FunctionName, "status": "in_progress", field: ""}
	if original, ok := s.toolNames[call.FunctionName]; ok {
		item["name"] = original.Name
		if original.Namespace != "" {
			item["namespace"] = original.Namespace
		}
	}
	if kind == "custom_tool_call" {
		item["id"] = "ctc_" + common.GetUUID()
	}
	index, err := s.add(item)
	if err != nil {
		return err
	}
	if err = s.event(prefix+".delta", map[string]any{"item_id": item["id"], "output_index": index, "delta": value}); err != nil {
		return err
	}
	item[field] = value
	if err = s.event(prefix+".done", map[string]any{"item_id": item["id"], "output_index": index, field: value}); err != nil {
		return err
	}
	item["status"] = "completed"
	return s.event("response.output_item.done", map[string]any{"output_index": index, "item": item})
}

func (s *geminiResponsesState) consume(response *dto.GeminiChatResponse) error {
	for _, candidate := range response.Candidates {
		// Responses has one output sequence; candidateCount is never requested.
		if candidate.Index != 0 {
			continue
		}
		if err := s.start(); err != nil {
			return err
		}
		s.grounding.observeCandidate(candidate)
		textParts := 0
		writePart := func(text string) error {
			if text == "" {
				return nil
			}
			if textParts > 0 {
				text = "\n" + text
			}
			textParts++
			return s.textDelta(text, false)
		}
		for _, part := range candidate.Content.Parts {
			if part.FileData != nil {
				s.info.RecordConversionDiagnostics(s.c, []types.ConversionDiagnostic{{Code: "unsupported_output_media", Path: "output", Message: "Gemini fileData output cannot be represented and was omitted", Severity: types.ConversionDiagnosticError, From: types.RelayFormatGemini, To: types.RelayFormatOpenAIResponses}})
			}
			if part.Text != "" {
				var err error
				if part.Thought {
					err = s.textDelta(part.Text, true)
				} else {
					err = writePart(part.Text)
				}
				if err != nil {
					return err
				}
			}
			if part.FunctionCall != nil {
				if err := s.functionCall(part.FunctionCall); err != nil {
					return err
				}
			}
			if part.InlineData != nil {
				// Match upstream's Gemini -> Chat -> Responses media representation.
				label := "[media]"
				if strings.HasPrefix(part.InlineData.MimeType, "image/") {
					label = "![image]"
				}
				if err := writePart(label + "(data:" + part.InlineData.MimeType + ";base64," + part.InlineData.Data + ")"); err != nil {
					return err
				}
			}
			if part.ExecutableCode != nil || part.CodeExecutionResult != nil {
				// Preserve hosted execution output in visible text; it is not a
				// client function call and must never request a tool result.
				text := ""
				if part.ExecutableCode != nil {
					text = "```" + strings.ToLower(part.ExecutableCode.Language) + "\n" + part.ExecutableCode.Code + "\n```\n"
				}
				if part.CodeExecutionResult != nil {
					text += "```output\n" + part.CodeExecutionResult.Output + "\n```\n"
				}
				if err := writePart(text); err != nil {
					return err
				}
			}
		}
		for _, annotation := range s.grounding.annotations(candidate.GroundingMetadata) {
			if s.textIndex < 0 {
				continue
			}
			wrapped := annotation.(map[string]any)
			citation := wrapped["url_citation"].(map[string]any)
			citation["type"] = "url_citation"
			index := len(s.annotations)
			s.annotations = append(s.annotations, citation)
			if err := s.event("response.output_text.annotation.added", map[string]any{"item_id": s.output[s.textIndex]["id"], "output_index": s.textIndex, "content_index": 0, "annotation_index": index, "annotation": citation}); err != nil {
				return err
			}
		}
		if candidate.FinishReason != nil && *candidate.FinishReason != "" {
			s.finish = *candidate.FinishReason
		}
	}
	return nil
}

func (s *geminiResponsesState) complete(usage *dto.Usage, grounding *geminiGroundingCounter) (map[string]any, error) {
	status := "completed"
	switch s.finish {
	case "", "STOP":
	case "MAX_TOKENS", "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY":
		status = "incomplete"
	default:
		// Upstream maps OTHER and unrecognized finish reasons to content_filter.
		status = "incomplete"
	}
	if s.reasoningIndex >= 0 {
		index := s.reasoningIndex
		item := s.output[index]
		part := map[string]any{"type": "summary_text", "text": s.reasoning}
		item["summary"] = []any{part}
		if err := s.event("response.reasoning_summary_text.done", map[string]any{"item_id": item["id"], "output_index": index, "summary_index": 0, "text": s.reasoning}); err != nil {
			return nil, err
		}
		if err := s.event("response.reasoning_summary_part.done", map[string]any{"item_id": item["id"], "output_index": index, "summary_index": 0, "part": part}); err != nil {
			return nil, err
		}
		if err := s.event("response.output_item.done", map[string]any{"output_index": index, "item": item}); err != nil {
			return nil, err
		}
	}
	if s.textIndex >= 0 {
		index := s.textIndex
		item := s.output[index]
		part := map[string]any{"type": "output_text", "text": s.text, "annotations": s.annotations}
		item["content"] = []any{part}
		item["status"] = status
		if err := s.event("response.output_text.done", map[string]any{"item_id": item["id"], "output_index": index, "content_index": 0, "text": s.text}); err != nil {
			return nil, err
		}
		if err := s.event("response.content_part.done", map[string]any{"item_id": item["id"], "output_index": index, "content_index": 0, "part": part}); err != nil {
			return nil, err
		}
		if err := s.event("response.output_item.done", map[string]any{"output_index": index, "item": item}); err != nil {
			return nil, err
		}
	}
	if len(grounding.queries) > 0 {
		queries := make([]string, 0, len(grounding.queries))
		for q := range grounding.queries {
			queries = append(queries, q)
		}
		sort.Strings(queries)
		item := map[string]any{"type": "web_search_call", "id": "ws_" + common.GetUUID(), "status": "in_progress", "action": map[string]any{"type": "search", "queries": queries}}
		index, err := s.add(item)
		if err != nil {
			return nil, err
		}
		item["status"] = "completed"
		if err = s.event("response.output_item.done", map[string]any{"output_index": index, "item": item}); err != nil {
			return nil, err
		}
	}
	response := s.response(status, usage)
	return response, s.event("response."+status, map[string]any{"response": response})
}
