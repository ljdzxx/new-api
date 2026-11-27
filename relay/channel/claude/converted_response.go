package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type claudeOutputBlock struct {
	kind          string
	index         int
	item          map[string]any
	text          strings.Builder
	arguments     strings.Builder
	initial       json.RawMessage
	closed        bool
	truncatedArgs bool
	itemDone      bool
	custom        bool
}

// One state per attempt. Native block indices are not Responses output indices
// (thinking and hosted/unsupported blocks can make the native indices sparse).
type claudeConvertedResponse struct {
	c                *gin.Context
	info             *relaycommon.RelayInfo
	id, model, stop  string
	created          int64
	sequence         int
	started, stopped bool
	blocks           map[int]*claudeOutputBlock
	ordered          []*claudeOutputBlock
	output           []map[string]any
	tools            map[string]dto.ResponsesToolIdentity
	emit             func(string, any) error
}

func newClaudeConvertedResponse(c *gin.Context, info *relaycommon.RelayInfo, emit func(string, any) error) *claudeConvertedResponse {
	s := &claudeConvertedResponse{c: c, info: info, id: "resp_" + common.GetUUID(), model: info.UpstreamModelName, created: common.GetTimestamp(), blocks: map[int]*claudeOutputBlock{}, output: make([]map[string]any, 0), emit: emit}
	if c != nil {
		if value, ok := c.Get(claudeResponsesToolsKey); ok {
			s.tools, _ = value.(map[string]dto.ResponsesToolIdentity)
		}
	}
	return s
}

func (s *claudeConvertedResponse) event(kind string, payload map[string]any) error {
	if s.emit == nil || s.info.RelayFormat != types.RelayFormatOpenAIResponses {
		return nil
	}
	payload["type"], payload["sequence_number"] = kind, s.sequence
	s.sequence++
	return s.emit(kind, payload)
}

func (s *claudeConvertedResponse) response(status string, usage *dto.Usage) map[string]any {
	for _, b := range s.ordered {
		s.updateItem(b)
		if status == "failed" && !b.itemDone {
			b.item["status"] = "incomplete"
		}
	}
	response := map[string]any{"id": s.id, "object": "response", "created_at": s.created, "model": s.model, "status": status, "output": s.output, "error": nil, "incomplete_details": nil, "usage": nil, "store": false}
	if status == "incomplete" {
		reason := "content_filter"
		if s.stop == "max_tokens" || s.stop == "model_context_window_exceeded" {
			reason = "max_output_tokens"
		}
		response["incomplete_details"] = map[string]any{"reason": reason}
	}
	if usage != nil {
		response["usage"] = map[string]any{"input_tokens": usage.PromptTokens, "output_tokens": usage.CompletionTokens, "total_tokens": usage.TotalTokens, "input_tokens_details": map[string]any{"cached_tokens": usage.PromptTokensDetails.CachedTokens, "cache_write_tokens": usage.PromptTokensDetails.CacheWriteTokens}, "output_tokens_details": map[string]any{"reasoning_tokens": usage.CompletionTokenDetails.ReasoningTokens}}
	}
	return response
}

func (s *claudeConvertedResponse) start(message *dto.ClaudeResponse) error {
	if s.started {
		return fmt.Errorf("duplicate Claude message_start")
	}
	s.started = true
	if message != nil && message.Model != "" {
		s.model = message.Model
	}
	if err := s.event("response.created", map[string]any{"response": s.response("in_progress", nil)}); err != nil {
		return err
	}
	return s.event("response.in_progress", map[string]any{"response": s.response("in_progress", nil)})
}

func (s *claudeConvertedResponse) add(index int, block *dto.ClaudeMediaMessage) error {
	if !s.started || block == nil || index < 0 {
		return fmt.Errorf("invalid Claude content_block_start")
	}
	if _, exists := s.blocks[index]; exists {
		return fmt.Errorf("duplicate Claude block index %d", index)
	}
	b := &claudeOutputBlock{kind: block.Type, index: len(s.output)}
	s.blocks[index] = b
	switch block.Type {
	case "text":
		b.item = map[string]any{"type": "message", "id": "msg_" + common.GetUUID(), "role": "assistant", "status": "in_progress", "content": []any{}}
	case "thinking":
		b.item = map[string]any{"type": "reasoning", "id": "rs_" + common.GetUUID(), "summary": []any{}}
	case "tool_use":
		if block.Id == "" || block.Name == "" {
			return fmt.Errorf("Claude tool_use requires id and name")
		}
		b.item = map[string]any{"type": "function_call", "id": "fc_" + common.GetUUID(), "call_id": block.Id, "name": block.Name, "arguments": "", "status": "in_progress"}
		if original, ok := s.tools[block.Name]; ok {
			b.item["name"] = original.Name
			if original.Namespace != "" {
				b.item["namespace"] = original.Namespace
			}
			if original.Custom {
				b.custom = true
				b.item["type"] = "custom_tool_call"
				b.item["id"] = "ctc_" + common.GetUUID()
				delete(b.item, "arguments")
				b.item["input"] = ""
			}
		}
		if block.Input != nil {
			raw, err := common.Marshal(block.Input)
			if err != nil {
				return err
			}
			b.initial = raw
		}
	default:
		s.info.RecordConversionDiagnostics(s.c, []types.ConversionDiagnostic{{Code: "unsupported_claude_output", Path: "content", Message: "Claude block " + block.Type + " cannot be represented and was omitted", Severity: types.ConversionDiagnosticWarning, From: types.RelayFormatClaude, To: s.info.RelayFormat}})
		return nil
	}
	s.ordered = append(s.ordered, b)
	s.output = append(s.output, b.item)
	if err := s.event("response.output_item.added", map[string]any{"output_index": b.index, "item": b.item}); err != nil {
		return err
	}
	if block.Type == "text" || block.Type == "thinking" {
		kind, key, partType := "response.content_part.added", "content_index", "output_text"
		if block.Type == "thinking" {
			kind, key, partType = "response.reasoning_summary_part.added", "summary_index", "summary_text"
		}
		part := map[string]any{"type": partType, "text": ""}
		if block.Type == "text" {
			part["annotations"] = []any{}
		}
		if err := s.event(kind, map[string]any{"output_index": b.index, "item_id": b.item["id"], key: 0, "part": part}); err != nil {
			return err
		}
		text := block.GetText()
		if block.Type == "thinking" && block.Thinking != nil {
			text = *block.Thinking
		}
		if text != "" {
			return s.textDelta(b, text)
		}
	}
	return nil
}

func (s *claudeConvertedResponse) textDelta(b *claudeOutputBlock, text string) error {
	b.text.WriteString(text)
	if s.info.RelayFormat == types.RelayFormatGemini {
		return s.geminiChunk([]dto.GeminiPart{{Text: text, Thought: b.kind == "thinking"}}, nil, "")
	}
	prefix, key := "response.output_text", "content_index"
	if b.kind == "thinking" {
		prefix, key = "response.reasoning_summary_text", "summary_index"
	}
	return s.event(prefix+".delta", map[string]any{"output_index": b.index, "item_id": b.item["id"], key: 0, "delta": text})
}

func (s *claudeConvertedResponse) delta(index int, delta *dto.ClaudeMediaMessage) error {
	b, exists := s.blocks[index]
	if !exists || b.closed || delta == nil {
		return fmt.Errorf("Claude delta has no open block %d", index)
	}
	if b.item == nil {
		return nil
	}
	switch delta.Type {
	case "text_delta":
		if b.kind != "text" || delta.Text == nil {
			return fmt.Errorf("invalid text delta")
		}
		return s.textDelta(b, *delta.Text)
	case "thinking_delta":
		if b.kind != "thinking" || delta.Thinking == nil {
			return fmt.Errorf("invalid thinking delta")
		}
		return s.textDelta(b, *delta.Thinking)
	case "input_json_delta":
		if b.kind != "tool_use" || delta.PartialJson == nil {
			return fmt.Errorf("invalid tool delta")
		}
		b.arguments.WriteString(*delta.PartialJson)
		if b.custom {
			return nil
		}
		return s.event("response.function_call_arguments.delta", map[string]any{"output_index": b.index, "item_id": b.item["id"], "delta": *delta.PartialJson})
	case "signature_delta":
		return nil
	default:
		return fmt.Errorf("unsupported Claude delta %q", delta.Type)
	}
}

func (s *claudeConvertedResponse) updateItem(b *claudeOutputBlock) {
	if b.item == nil {
		return
	}
	switch b.kind {
	case "text":
		b.item["content"] = []any{map[string]any{"type": "output_text", "text": b.text.String(), "annotations": []any{}}}
	case "thinking":
		b.item["summary"] = []any{map[string]any{"type": "summary_text", "text": b.text.String()}}
	case "tool_use":
		if !b.custom {
			b.item["arguments"] = b.arguments.String()
		}
	}
}

func (s *claudeConvertedResponse) close(index int) error {
	b, exists := s.blocks[index]
	if !exists || b.closed {
		return fmt.Errorf("Claude stop has no open block %d", index)
	}
	if b.item == nil {
		b.closed = true
		return nil
	}
	if b.kind == "tool_use" {
		initial := b.arguments.Len() == 0
		if initial {
			if len(b.initial) > 0 {
				b.arguments.Write(b.initial)
			} else {
				b.arguments.WriteString("{}")
			}
		}
		var args map[string]json.RawMessage
		if err := common.UnmarshalUseNumber([]byte(b.arguments.String()), &args); err != nil || args == nil {
			// A length stop can split a tool argument mid-object. Its stop reason follows block_stop.
			if !initial && args == nil && errors.Is(err, io.ErrUnexpectedEOF) && strings.HasPrefix(strings.TrimSpace(b.arguments.String()), "{") {
				b.truncatedArgs = true
				b.closed = true
				s.updateItem(b)
				return nil
			}
			return fmt.Errorf("Claude tool arguments are incomplete or invalid")
		}
		field, prefix, value := "arguments", "response.function_call_arguments", b.arguments.String()
		if b.custom {
			var input string
			if err := common.Unmarshal(args["input"], &input); err != nil {
				return fmt.Errorf("custom tool arguments require string input")
			}
			field, prefix, value = "input", "response.custom_tool_call_input", input
		}
		b.item[field] = value
		if initial || b.custom {
			if err := s.event(prefix+".delta", map[string]any{"output_index": b.index, "item_id": b.item["id"], "delta": value}); err != nil {
				return err
			}
		}
		if err := s.event(prefix+".done", map[string]any{"output_index": b.index, "item_id": b.item["id"], field: value}); err != nil {
			return err
		}
		if s.info.RelayFormat == types.RelayFormatGemini {
			if err := s.geminiChunk([]dto.GeminiPart{{FunctionCall: &dto.FunctionCall{ID: common.GetPointer(b.item["call_id"].(string)), FunctionName: b.item["name"].(string), Arguments: json.RawMessage(b.arguments.String())}}}, nil, ""); err != nil {
				return err
			}
		}
	} else {
		prefix, key, partKind, partEvent := "response.output_text", "content_index", "output_text", "response.content_part.done"
		if b.kind == "thinking" {
			prefix, key, partKind, partEvent = "response.reasoning_summary_text", "summary_index", "summary_text", "response.reasoning_summary_part.done"
		}
		if err := s.event(prefix+".done", map[string]any{"output_index": b.index, "item_id": b.item["id"], key: 0, "text": b.text.String()}); err != nil {
			return err
		}
		part := map[string]any{"type": partKind, "text": b.text.String()}
		if b.kind == "text" {
			part["annotations"] = []any{}
		}
		if err := s.event(partEvent, map[string]any{"output_index": b.index, "item_id": b.item["id"], key: 0, "part": part}); err != nil {
			return err
		}
	}
	s.updateItem(b)
	b.closed = true
	if b.kind == "tool_use" {
		b.item["status"] = "completed"
		b.itemDone = true
		return s.event("response.output_item.done", map[string]any{"output_index": b.index, "item": b.item})
	}
	// The stop reason arrives after block_stop. Defer item completion so a
	// max_tokens result never advertises a completed message prematurely.
	return nil
}

func (s *claudeConvertedResponse) consume(response *dto.ClaudeResponse) error {
	if s.stopped {
		return fmt.Errorf("Claude event after message_stop")
	}
	switch response.Type {
	case "ping":
		return nil
	case "message_start":
		if response.Message == nil {
			return fmt.Errorf("empty message_start")
		}
		return s.start(&dto.ClaudeResponse{Model: response.Message.Model})
	case "content_block_start":
		if response.Index == nil {
			return fmt.Errorf("missing Claude block index")
		}
		return s.add(*response.Index, response.ContentBlock)
	case "content_block_delta":
		if response.Index == nil {
			return fmt.Errorf("missing Claude block index")
		}
		return s.delta(*response.Index, response.Delta)
	case "content_block_stop":
		if response.Index == nil {
			return fmt.Errorf("missing Claude block index")
		}
		return s.close(*response.Index)
	case "message_delta":
		if response.Delta != nil && response.Delta.StopReason != nil {
			s.stop = *response.Delta.StopReason
			maybeMarkClaudeRefusal(s.c, s.stop)
		}
	case "message_stop":
		if !s.started || s.stop == "" {
			return fmt.Errorf("Claude stream ended without a stop reason")
		}
		for _, b := range s.blocks {
			if !b.closed {
				return fmt.Errorf("Claude stream ended with an open block")
			}
		}
		s.stopped = true
	default:
		return fmt.Errorf("unsupported Claude event %q", response.Type)
	}
	return nil
}

func (s *claudeConvertedResponse) complete(usage *dto.Usage) (any, error) {
	if !s.stopped {
		return nil, fmt.Errorf("Claude stream ended before message_stop")
	}
	finishReason := stopReasonClaude2OpenAI(s.stop)
	for _, b := range s.ordered {
		if b.truncatedArgs && finishReason != "length" {
			return nil, fmt.Errorf("Claude tool arguments are incomplete")
		}
	}
	if s.info.RelayFormat == types.RelayFormatGemini {
		if s.emit != nil {
			return nil, s.geminiChunk(nil, usage, s.stop)
		}
		parts := make([]dto.GeminiPart, 0, len(s.ordered))
		for _, b := range s.ordered {
			if b.truncatedArgs {
				continue
			}
			if b.kind == "tool_use" {
				parts = append(parts, dto.GeminiPart{FunctionCall: &dto.FunctionCall{ID: common.GetPointer(b.item["call_id"].(string)), FunctionName: b.item["name"].(string), Arguments: json.RawMessage(b.arguments.String())}})
			} else {
				parts = append(parts, dto.GeminiPart{Text: b.text.String(), Thought: b.kind == "thinking"})
			}
		}
		return s.geminiResponse(parts, usage, s.stop), nil
	}
	status := "completed"
	if finishReason == "length" || finishReason == "content_filter" {
		status = "incomplete"
	}
	for _, b := range s.ordered {
		if !b.itemDone {
			if b.kind == "text" || b.truncatedArgs {
				b.item["status"] = status
			}
			b.itemDone = true
			if err := s.event("response.output_item.done", map[string]any{"output_index": b.index, "item": b.item}); err != nil {
				return nil, err
			}
		}
	}
	response := s.response(status, usage)
	return response, s.event("response."+status, map[string]any{"response": response})
}

func (s *claudeConvertedResponse) geminiResponse(parts []dto.GeminiPart, usage *dto.Usage, stop string) map[string]any {
	if parts == nil {
		parts = []dto.GeminiPart{}
	}
	candidate := map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": parts}}
	if stop != "" {
		finish := "STOP"
		switch stopReasonClaude2OpenAI(stop) {
		case "length":
			finish = "MAX_TOKENS"
		case "content_filter":
			finish = "SAFETY"
		}
		candidate["finishReason"] = finish
	}
	response := map[string]any{"candidates": []any{candidate}, "modelVersion": s.model, "responseId": s.id}
	if usage != nil {
		response["usageMetadata"] = map[string]any{"promptTokenCount": usage.PromptTokens, "candidatesTokenCount": usage.CompletionTokens, "totalTokenCount": usage.TotalTokens, "cachedContentTokenCount": usage.PromptTokensDetails.CachedTokens}
	}
	return response
}

func (s *claudeConvertedResponse) geminiChunk(parts []dto.GeminiPart, usage *dto.Usage, stop string) error {
	if s.emit == nil {
		return nil
	}
	return s.emit("", s.geminiResponse(parts, usage, stop))
}

func convertClaudeResponse(c *gin.Context, info *relaycommon.RelayInfo, response *dto.ClaudeResponse, usage *dto.Usage) (any, error) {
	s := newClaudeConvertedResponse(c, info, nil)
	if response.Type != "message" || response.StopReason == "" {
		return nil, fmt.Errorf("invalid Claude message response")
	}
	if err := s.start(response); err != nil {
		return nil, err
	}
	for i := range response.Content {
		if err := s.add(i, &response.Content[i]); err != nil {
			return nil, err
		}
		if err := s.close(i); err != nil {
			return nil, err
		}
	}
	s.stop = response.StopReason
	s.stopped = true
	return s.complete(usage)
}
