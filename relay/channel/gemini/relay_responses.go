package gemini

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func decodeGeminiResponses(data []byte) (*dto.GeminiChatResponse, *types.NewAPIError) {
	var envelope struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := common.Unmarshal(data, &envelope); err != nil {
		return nil, geminiResponsesError(err)
	}
	if envelope.Error != nil {
		return nil, geminiResponsesError(fmt.Errorf("Gemini upstream error: %s", envelope.Error.Message))
	}
	var response dto.GeminiChatResponse
	if err := common.Unmarshal(data, &response); err != nil {
		return nil, geminiResponsesError(err)
	}
	return &response, nil
}

func geminiResponsesError(err error) *types.NewAPIError {
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
}

func geminiResponsesBlocked(c *gin.Context, response *dto.GeminiChatResponse) *types.NewAPIError {
	if response.PromptFeedback != nil && response.PromptFeedback.BlockReason != nil && *response.PromptFeedback.BlockReason != "" {
		reason := *response.PromptFeedback.BlockReason
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_block_reason="+reason)
		return types.NewOpenAIError(fmt.Errorf("request blocked by Gemini API: %s", reason), types.ErrorCodePromptBlocked, http.StatusBadRequest)
	}
	return nil
}

func GeminiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, geminiResponsesError(fmt.Errorf("empty Gemini response"))
	}
	defer service.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, geminiResponsesError(err)
	}
	response, apiErr := decodeGeminiResponses(body)
	if apiErr != nil {
		return nil, apiErr
	}
	usage := buildUsageFromGeminiResponse(c, info, response)
	var grounding geminiGroundingCounter
	grounding.observe(response)
	grounding.commit(info)
	if apiErr = geminiResponsesBlocked(c, response); apiErr != nil {
		return &usage, apiErr
	}
	if len(response.Candidates) == 0 {
		return &usage, geminiResponsesError(fmt.Errorf("empty Gemini candidates"))
	}
	state := newGeminiResponsesState(c, info, nil)
	partial := geminiPartialStreamState{partialToolByCandidate: make(map[int64]*geminiPartialToolCall)}
	prepared, err := partial.preparePartialFunctionCalls(response)
	if err != nil {
		return &usage, geminiResponsesError(err)
	}
	if len(partial.partialToolByCandidate) > 0 {
		return &usage, geminiResponsesError(fmt.Errorf("incomplete Gemini function call"))
	}
	if err = state.consume(prepared); err != nil {
		return &usage, geminiResponsesError(err)
	}
	if !state.started {
		return &usage, geminiResponsesError(fmt.Errorf("Gemini response is missing candidate zero"))
	}
	converted, err := state.complete(&usage, &grounding)
	if err != nil {
		return &usage, geminiResponsesError(err)
	}
	body, err = common.Marshal(converted)
	if err != nil {
		return &usage, geminiResponsesError(err)
	}
	if helper.ShouldScaleResponseUsage(info) {
		body, err = helper.PatchResponseUsageJSONForRelay(body, types.RelayFormatOpenAIResponses, info)
		if err != nil {
			return &usage, geminiResponsesError(err)
		}
	}
	c.Header("Content-Type", "application/json")
	service.IOCopyBytesGracefully(c, resp, body)
	return &usage, nil
}

func GeminiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, geminiResponsesError(fmt.Errorf("empty Gemini stream"))
	}
	defer service.CloseResponseBodyGracefully(resp)
	var streamErr *types.NewAPIError
	send := func(kind string, payload map[string]any) error {
		data, err := common.Marshal(payload)
		if err != nil {
			return err
		}
		if helper.ShouldScaleResponseUsage(info) {
			data, err = helper.PatchResponseUsageJSONForRelay(data, types.RelayFormatOpenAIResponses, info)
			if err != nil {
				return err
			}
		}
		helper.SetEventStreamHeaders(c)
		if err = helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: kind}, string(data)); err != nil {
			streamErr = types.NewClientDisconnectedError(err)
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
			return err
		}
		switch kind {
		case "response.output_text.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
			if responsesString(payload["delta"]) != "" {
				common.SetContextKey(c, constant.ContextKeyResponsesBillableStreamOutput, true)
			}
		}
		return nil
	}
	state := newGeminiResponsesState(c, info, send)
	partial := geminiPartialStreamState{partialToolByCandidate: make(map[int64]*geminiPartialToolCall)}
	var metadata *dto.GeminiUsageMetadata
	var grounding geminiGroundingCounter
	var outputText strings.Builder
	imageCount := 0
	sawFrame := false
	scan := helper.StreamScannerHandlerWithOptions(c, resp, info, func(data string, sr *helper.StreamResult) {
		response, apiErr := decodeGeminiResponses([]byte(data))
		if apiErr != nil {
			streamErr = apiErr
			sr.Stop(apiErr)
			return
		}
		sawFrame = true
		// Observe raw usage before conversion: a malformed tool call must not
		// erase the charge already incurred upstream.
		if incoming := response.GetUsageMetadata(); incoming != nil {
			metadata = dto.MergeGeminiUsageMetadataNonZero(metadata, incoming)
		}
		grounding.observe(response)
		grounding.commit(info)
		imageCount += geminiResponseInlineImageCount(response)
		if apiErr = geminiResponsesBlocked(c, response); apiErr != nil {
			streamErr = apiErr
			sr.Stop(apiErr)
			return
		}
		prepared, err := partial.preparePartialFunctionCalls(response)
		if err == nil {
			// Count the reconstructed call once, not each partialArgs frame
			// (whose args may be nil or an overlapping cumulative snapshot).
			outputText.WriteString(geminiResponseUsageText(prepared))
			err = state.consume(prepared)
		}
		if err != nil {
			if streamErr == nil {
				streamErr = geminiResponsesError(err)
			}
			sr.Stop(err)
		}
		// Keep reading after finishReason; Gemini may send usage/grounding
		// snapshots in trailing frames. EOF is successful only with a finish.
	}, helper.StreamScannerOptions{PingDataFunc: func(c *gin.Context) error {
		return helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.keepalive"}, `{"type":"response.keepalive"}`)
	}})
	usage := &dto.Usage{}
	if dto.HasGeminiUsageMetadataTokens(metadata) {
		mapped := buildUsageFromGeminiMetadata(*metadata, info.GetEstimatePromptTokens())
		usage = &mapped
		patchGeminiZeroCompletionUsage(c, info, usage, outputText.String(), imageCount)
	} else if sawFrame {
		usage = service.ResponseText2Usage(c, outputText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		patchGeminiZeroCompletionUsage(c, info, usage, "", imageCount)
		attachEstimatedGeminiBillingUsage(usage)
	}
	if streamErr == nil {
		switch {
		case scan.Reason == helper.StreamScannerClientDisconnected:
			streamErr = types.NewClientDisconnectedError(fmt.Errorf("Gemini Responses client disconnected: %w", scan.Err))
		case scan.Reason != helper.StreamScannerFinished:
			streamErr = geminiResponsesError(fmt.Errorf("Gemini Responses stream interrupted (%s): %v", scan.Reason, scan.Err))
		case len(partial.partialToolByCandidate) > 0:
			streamErr = geminiResponsesError(fmt.Errorf("Gemini stream ended with an incomplete function call"))
		case !state.started || state.finish == "":
			streamErr = geminiResponsesError(fmt.Errorf("Gemini stream ended without a finish reason"))
		}
	}
	if streamErr == nil {
		if _, err := state.complete(usage, &grounding); err != nil && streamErr == nil {
			streamErr = geminiResponsesError(err)
		}
	}
	if streamErr != nil {
		// Scanner EOF can arrive before its queued frames finish writing. All
		// scanner/keepalive goroutines have joined now, so record the final
		// downstream result for billing logs instead of leaving a stale EOF.
		if streamErr.GetErrorCode() == types.ErrorCodeClientDisconnected && info.StreamStatus != nil {
			info.StreamStatus.EndReason = relaycommon.StreamEndReasonClientGone
			info.StreamStatus.EndError = streamErr
		}
		if helper.ResponsesStreamStarted(c) {
			types.ErrOptionWithSkipRetry()(streamErr)
			if streamErr.GetErrorCode() != types.ErrorCodeClientDisconnected {
				failed := state.response("failed", usage)
				failed["error"] = streamErr.ToOpenAIError()
				_ = state.event("response.failed", map[string]any{"response": failed})
			}
		}
		return usage, streamErr
	}
	return usage, nil
}
