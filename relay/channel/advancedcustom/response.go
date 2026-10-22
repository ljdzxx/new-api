package advancedcustom

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type responseState struct {
	c           *gin.Context
	info        *relaycommon.RelayInfo
	stream      bool
	id, model   string
	parts       []any
	text        map[string]string
	calls       map[string]*responseCall
	searches    map[string]bool
	grounding   []any
	supports    []any
	citations   map[string]bool
	textOffsets map[string]int
	answerBytes int
	usage       *dto.Usage
	usageText   strings.Builder
	terminal    bool
}
type responseCall struct {
	name, id, args string
	emitted        bool
}

func responseError(err error) *types.NewAPIError {
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
}
func (s *responseState) frame(parts []any, finish string) map[string]any {
	candidate := map[string]any{"index": 0}
	if len(parts) > 0 {
		candidate["content"] = map[string]any{"role": "model", "parts": parts}
	}
	if finish != "" {
		candidate["finishReason"] = finish
	}
	if len(s.grounding) > 0 {
		candidate["groundingMetadata"] = map[string]any{"groundingChunks": s.grounding, "groundingSupports": s.supports}
	}
	frame := map[string]any{"candidates": []any{candidate}, "modelVersion": s.model, "responseId": s.id}
	if finish != "" && s.usage != nil {
		u := s.usage
		frame["usageMetadata"] = map[string]any{"promptTokenCount": u.PromptTokens, "candidatesTokenCount": max(0, u.CompletionTokens-u.CompletionTokenDetails.ReasoningTokens), "thoughtsTokenCount": u.CompletionTokenDetails.ReasoningTokens, "totalTokenCount": u.TotalTokens, "cachedContentTokenCount": u.PromptTokensDetails.CachedTokens}
	}
	return frame
}
func (s *responseState) emit(parts []any, finish string) error {
	if !s.stream {
		s.parts = append(s.parts, parts...)
		return nil
	}
	helper.SetEventStreamHeaders(s.c)
	before := s.c.Writer.Size()
	err := helper.ObjectDataWithScaledUsage(s.c, s.info, types.RelayFormatGemini, s.frame(parts, finish))
	if s.c.Writer.Size() > before {
		s.c.Set(StreamStartedKey, true)
	}
	if err != nil {
		return err
	}
	if len(parts) > 0 {
		common.SetContextKey(s.c, constant.ContextKeyResponsesBillableStreamOutput, true)
	}
	return nil
}
func (s *responseState) addText(key, text string, thought, delta bool) error {
	if !delta {
		previous := s.text[key]
		if strings.HasPrefix(text, previous) {
			text = text[len(previous):]
		} else if previous != "" {
			return fmt.Errorf("Responses text snapshot changed after delivery")
		}
	}
	if text == "" {
		return nil
	}
	if !thought {
		if s.textOffsets == nil {
			s.textOffsets = map[string]int{}
		}
		if _, ok := s.textOffsets[key]; !ok {
			s.textOffsets[key] = s.answerBytes
		}
		s.answerBytes += len(text)
	}
	s.text[key] += text
	s.usageText.WriteString(text)
	part := map[string]any{"text": text}
	if thought {
		part["thought"] = true
	}
	return s.emit([]any{part}, "")
}
func (s *responseState) call(key string) *responseCall {
	if s.calls[key] == nil {
		s.calls[key] = &responseCall{}
	}
	return s.calls[key]
}
func (s *responseState) item(item gjson.Result, index int, done bool) error {
	id := item.Get("id").String()
	if id == "" {
		id = "output_" + strconv.Itoa(index)
	}
	switch item.Get("type").String() {
	case "message":
		if !done {
			return nil
		}
		for i, part := range item.Get("content").Array() {
			kind := part.Get("type").String()
			if kind == "output_text" || kind == "refusal" {
				text := part.Get("text").String()
				if kind == "refusal" {
					text = part.Get("refusal").String()
				}
				if err := s.addText(id+":text:"+strconv.Itoa(i), text, false, false); err != nil {
					return err
				}
				for _, a := range part.Get("annotations").Array() {
					if a.Get("type").String() == "url_citation" {
						key := id + ":text:" + strconv.Itoa(i)
						citationKey := key + ":" + a.Get("url").String() + ":" + a.Get("start_index").String() + ":" + a.Get("end_index").String()
						if s.citations == nil {
							s.citations = map[string]bool{}
						}
						if s.citations[citationKey] {
							continue
						}
						s.citations[citationKey] = true
						index := len(s.grounding)
						s.grounding = append(s.grounding, map[string]any{"web": map[string]any{"uri": a.Get("url").String(), "title": a.Get("title").String()}})
						start, end := a.Get("start_index").Int(), a.Get("end_index").Int()
						if start >= 0 && end >= start && end <= int64(utf8.RuneCountInString(text)) {
							runes := []rune(text)
							offset := s.textOffsets[key]
							s.supports = append(s.supports, map[string]any{"segment": map[string]any{"startIndex": offset + len(string(runes[:start])), "endIndex": offset + len(string(runes[:end])), "text": string(runes[start:end])}, "groundingChunkIndices": []int{index}})
						}
					}
				}
			}
		}
	case "reasoning":
		if !done {
			return nil
		}
		for i, part := range item.Get("summary").Array() {
			if err := s.addText(id+":thought:"+strconv.Itoa(i), part.Get("text").String(), true, false); err != nil {
				return err
			}
		}
	case "function_call":
		call := s.call(id)
		if name := item.Get("name").String(); name != "" {
			call.name = name
		}
		if callID := item.Get("call_id").String(); callID != "" {
			call.id = callID
		}
		if args := item.Get("arguments"); args.Exists() {
			call.args = args.String()
		}
		if done && !call.emitted {
			if call.name == "" {
				return fmt.Errorf("Responses function call has no name")
			}
			var args map[string]any
			if call.args == "" {
				call.args = "{}"
			}
			if err := common.UnmarshalJsonStr(call.args, &args); err != nil || args == nil {
				return fmt.Errorf("Responses function arguments must be a JSON object")
			}
			if call.id == "" {
				call.id = id
			}
			s.usageText.WriteString(call.name + call.args)
			if err := s.emit([]any{map[string]any{"functionCall": map[string]any{"id": call.id, "name": call.name, "args": args}}}, ""); err != nil {
				return err
			}
			call.emitted = true
		}
	case "web_search_call":
		if done && item.Get("status").String() == "completed" && !s.searches[id] {
			s.searches[id] = true
			s.info.SetBillableToolCount(dto.BuildInToolWebSearchPreview, len(s.searches))
		}
	case "":
		return fmt.Errorf("Responses output item has no type")
	default:
		s.info.RecordConversionDiagnostics(s.c, []types.ConversionDiagnostic{{Code: "unsupported_response_item", Path: "output", Message: "Responses output item cannot be represented by this Gemini converter", Severity: types.ConversionDiagnosticWarning, From: types.RelayFormatOpenAIResponses, To: types.RelayFormatGemini}})
	}
	return nil
}
func (s *responseState) observeUsage(response gjson.Result) error {
	if id := response.Get("id").String(); id != "" {
		s.id = id
	}
	if model := response.Get("model").String(); model != "" {
		s.model = model
	}
	value := response.Get("usage")
	if !value.Exists() || value.Type == gjson.Null {
		return nil
	}
	var native dto.Usage
	if err := common.UnmarshalJsonStr(value.Raw, &native); err != nil {
		return err
	}
	// The local shared Usage DTO names this field completion_tokens_details;
	// Responses uses output_tokens_details. Normalize at this protocol boundary.
	if details := value.Get("output_tokens_details"); details.Exists() && details.Type != gjson.Null {
		if err := common.UnmarshalJsonStr(details.Raw, &native.CompletionTokenDetails); err != nil {
			return err
		}
	}
	// Native usage remains the billing source; clamp hostile counts before any
	// arithmetic or client conversion and preserve the audit marker.
	clampCount := func(n int) int {
		v, clamp := common.QuotaFromFloatChecked(float64(max(0, n)))
		if clamp != nil && s.info.QuotaClamp == nil {
			s.info.QuotaClamp = clamp
		}
		return v
	}
	native.InputTokens = clampCount(native.InputTokens)
	native.OutputTokens = clampCount(native.OutputTokens)
	native.CompletionTokenDetails.ReasoningTokens = min(native.OutputTokens, clampCount(native.CompletionTokenDetails.ReasoningTokens))
	native.CompletionTokenDetails.TextTokens = min(native.OutputTokens, clampCount(native.CompletionTokenDetails.TextTokens))
	native.CompletionTokenDetails.ImageTokens = min(native.OutputTokens, clampCount(native.CompletionTokenDetails.ImageTokens))
	native.CompletionTokenDetails.AudioTokens = min(native.OutputTokens, clampCount(native.CompletionTokenDetails.AudioTokens))
	if details := native.InputTokensDetails; details != nil {
		details.CachedTokens = min(native.InputTokens, clampCount(details.CachedTokens))
		details.CacheWriteTokens = min(native.InputTokens, clampCount(details.CacheWriteTokens))
		details.TextTokens = min(native.InputTokens, clampCount(details.TextTokens))
		details.ImageTokens = min(native.InputTokens, clampCount(details.ImageTokens))
		details.AudioTokens = min(native.InputTokens, clampCount(details.AudioTokens))
	}
	usage := &dto.Usage{PromptTokens: native.InputTokens, InputTokens: native.InputTokens, CompletionTokens: native.OutputTokens, OutputTokens: native.OutputTokens, CompletionTokenDetails: native.CompletionTokenDetails}
	if native.InputTokensDetails != nil {
		usage.PromptTokensDetails = *native.InputTokensDetails
	}
	total, clamp := common.QuotaFromFloatChecked(float64(usage.PromptTokens) + float64(usage.CompletionTokens))
	usage.TotalTokens = total
	if clamp != nil && s.info.QuotaClamp == nil {
		s.info.QuotaClamp = clamp
	}
	// Build the billing snapshot solely from Responses fields. Do not trust
	// compatibility/internal fields injected into an upstream usage object.
	native = dto.Usage{InputTokens: native.InputTokens, OutputTokens: native.OutputTokens, PromptTokens: native.InputTokens, CompletionTokens: native.OutputTokens, TotalTokens: total, InputTokensDetails: native.InputTokensDetails, PromptTokensDetails: usage.PromptTokensDetails, CompletionTokenDetails: native.CompletionTokenDetails}
	usage.BillingUsage = dto.NewOpenAIResponsesBillingUsage(&native)
	if usage.BillingUsage == nil {
		// Presence is authoritative even when every upstream counter is zero.
		usage.BillingUsage = &dto.BillingUsage{Source: dto.BillingUsageSourceOAIResponses, Semantic: dto.BillingUsageSemanticOpenAI, OpenAIUsage: &native}
	}
	s.usage = usage
	s.info.UpstreamUsageReported = true
	return nil
}
func (s *responseState) finalUsage() *dto.Usage {
	if s.usage == nil {
		s.usage = service.ResponseText2Usage(s.c, s.usageText.String(), s.info.UpstreamModelName, s.info.GetEstimatePromptTokens())
	}
	return s.usage
}
func (s *responseState) complete(response gjson.Result) error {
	if err := s.observeUsage(response); err != nil {
		return err
	}
	status := response.Get("status").String()
	if status != "completed" && status != "incomplete" {
		return fmt.Errorf("Responses upstream ended with status %q", status)
	}
	for i, item := range response.Get("output").Array() {
		if err := s.item(item, i, true); err != nil {
			return err
		}
	}
	for _, call := range s.calls {
		if !call.emitted {
			return fmt.Errorf("Responses stream ended with unfinished function arguments")
		}
	}
	finish := "STOP"
	if status == "incomplete" {
		finish = "MAX_TOKENS"
		if response.Get("incomplete_details.reason").String() != "max_output_tokens" {
			finish = "SAFETY"
		}
	}
	s.finalUsage()
	s.terminal = true
	return s.emit(nil, finish)
}

func responsesToGemini(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, responseError(fmt.Errorf("empty Responses body"))
	}
	defer service.CloseResponseBodyGracefully(resp)
	s := &responseState{c: c, info: info, stream: info.IsStream, id: helper.GetResponseID(c), model: info.UpstreamModelName, text: map[string]string{}, calls: map[string]*responseCall{}, searches: map[string]bool{}}
	if !info.IsStream {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, responseError(err)
		}
		if !gjson.ValidBytes(body) {
			return nil, responseError(fmt.Errorf("invalid Responses JSON"))
		}
		result := gjson.ParseBytes(body)
		if err = s.complete(result); err != nil {
			return nil, responseError(err)
		}
		finish := "STOP"
		if result.Get("status").String() == "incomplete" {
			finish = "MAX_TOKENS"
			if result.Get("incomplete_details.reason").String() != "max_output_tokens" {
				finish = "SAFETY"
			}
		}
		body = raw(s.frame(s.parts, finish))
		if helper.ShouldScaleResponseUsage(info) {
			body, err = helper.PatchResponseUsageJSONForRelay(body, types.RelayFormatGemini, info)
			if err != nil {
				return nil, responseError(err)
			}
		}
		c.Header("Content-Type", "application/json")
		service.IOCopyBytesGracefully(c, resp, body)
		return s.usage, nil
	}
	var streamErr error
	scan := helper.StreamScannerHandlerWithOptions(c, resp, info, func(data string, sr *helper.StreamResult) {
		if s.terminal {
			return
		}
		if !gjson.Valid(data) {
			streamErr = fmt.Errorf("invalid Responses stream frame")
			sr.Stop(streamErr)
			return
		}
		event := gjson.Parse(data)
		kind := event.Get("type").String()
		var err error
		id := event.Get("item_id").String()
		if id == "" {
			id = "output_" + strconv.FormatInt(event.Get("output_index").Int(), 10)
		}
		switch kind {
		case "response.created", "response.in_progress":
			err = s.observeUsage(event.Get("response"))
		case "response.output_text.delta":
			err = s.addText(id+":text:"+strconv.FormatInt(event.Get("content_index").Int(), 10), event.Get("delta").String(), false, true)
		case "response.reasoning_summary_text.delta":
			err = s.addText(id+":thought:"+strconv.FormatInt(event.Get("summary_index").Int(), 10), event.Get("delta").String(), true, true)
		case "response.output_item.added":
			err = s.item(event.Get("item"), int(event.Get("output_index").Int()), false)
		case "response.function_call_arguments.delta":
			s.call(id).args += event.Get("delta").String()
		case "response.function_call_arguments.done":
			s.call(id).args = event.Get("arguments").String()
			call := s.call(id)
			if call.name != "" {
				err = s.item(gjson.ParseBytes(raw(map[string]any{"id": id, "type": "function_call", "call_id": call.id, "name": call.name, "arguments": call.args})), int(event.Get("output_index").Int()), true)
			}
		case "response.output_item.done":
			err = s.item(event.Get("item"), int(event.Get("output_index").Int()), true)
		case "response.completed", "response.incomplete":
			err = s.complete(event.Get("response"))
			sr.Stop(err)
		case "response.failed", "response.cancelled", "error":
			_ = s.observeUsage(event.Get("response"))
			err = fmt.Errorf("Responses upstream stream failed")
			sr.Stop(err)
		}
		if err != nil {
			streamErr = err
			sr.Stop(err)
		}
	}, helper.StreamScannerOptions{})
	if c.Request.Context().Err() != nil || scan.Reason == helper.StreamScannerClientDisconnected {
		err := types.NewClientDisconnectedError(c.Request.Context().Err())
		if info.StreamStatus != nil {
			// The scanner and writer have joined; replace an earlier EOF that
			// raced with the downstream cancellation, as the native handler does.
			info.StreamStatus.EndReason = relaycommon.StreamEndReasonClientGone
			info.StreamStatus.EndError = err
		}
		return s.finalUsage(), err
	}
	if streamErr == nil && !s.terminal {
		streamErr = fmt.Errorf("Responses stream ended without a terminal response (%s)", scan.Reason)
	}
	if streamErr != nil {
		err := responseError(streamErr)
		if c.GetBool(StreamStartedKey) {
			types.ErrOptionWithSkipRetry()(err)
		}
		return s.finalUsage(), err
	}
	return s.finalUsage(), nil
}
