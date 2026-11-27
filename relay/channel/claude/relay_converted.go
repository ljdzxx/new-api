package claude

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func claudeConversionResponseError(err error) *types.NewAPIError {
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
}

func ClaudeConvertedStreamHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, claudeConversionResponseError(fmt.Errorf("empty Claude stream"))
	}
	usageState := &ClaudeResponseInfo{Usage: &dto.Usage{}, Model: info.UpstreamModelName}
	var streamErr *types.NewAPIError
	send := func(kind string, payload any) error {
		body, err := common.Marshal(payload)
		if err != nil {
			return err
		}
		if helper.ShouldScaleResponseUsage(info) {
			body, err = helper.PatchResponseUsageJSONForRelay(body, info.RelayFormat, info)
			if err != nil {
				return err
			}
		}
		helper.ExtendWriteDeadline(c)
		if info.RelayFormat == types.RelayFormatOpenAIResponses {
			err = helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: kind}, string(body))
		} else {
			c.Set(helper.ConvertedGeminiStreamStartedKey, true)
			err = helper.StringData(c, string(body))
		}
		if err != nil {
			streamErr = types.NewClientDisconnectedError(err)
			return err
		}
		if kind == "response.output_text.delta" || kind == "response.reasoning_summary_text.delta" || kind == "response.function_call_arguments.delta" || kind == "response.custom_tool_call_input.delta" {
			if p, ok := payload.(map[string]any); ok && p["delta"] != "" {
				common.SetContextKey(c, constant.ContextKeyResponsesBillableStreamOutput, true)
			}
		}
		return nil
	}
	state := newClaudeConvertedResponse(c, info, send)
	sawMessage := false
	scan := helper.StreamScannerHandlerWithOptions(c, resp, info, func(data string, sr *helper.StreamResult) {
		var response dto.ClaudeResponse
		if err := common.UnmarshalJsonStr(data, &response); err != nil {
			streamErr = claudeConversionResponseError(err)
			sr.Stop(err)
			return
		}
		if apiErr := response.GetClaudeError(); apiErr != nil && apiErr.Type != "" {
			streamErr = types.WithClaudeError(*apiErr, http.StatusBadGateway)
			sr.Stop(streamErr)
			return
		}
		if response.Type == "message_start" {
			sawMessage = true
		}
		FormatClaudeResponseInfo(&response, nil, usageState)
		recordClaudeStreamUsage(info, usageState, &response)
		if response.Type == "content_block_start" && response.ContentBlock != nil {
			usageState.ResponseText.WriteString(response.ContentBlock.GetText())
			if response.ContentBlock.Thinking != nil {
				usageState.ResponseText.WriteString(*response.ContentBlock.Thinking)
			}
		}
		if response.Delta != nil && response.Delta.PartialJson != nil {
			usageState.ResponseText.WriteString(*response.Delta.PartialJson)
		}
		if response.Usage != nil && response.Usage.ServerToolUse != nil && response.Usage.ServerToolUse.WebSearchRequests > 0 {
			c.Set("claude_web_search_requests", response.Usage.ServerToolUse.WebSearchRequests)
		}
		if err := state.consume(&response); err != nil {
			if streamErr == nil {
				streamErr = claudeConversionResponseError(err)
			}
			sr.Stop(err)
			return
		}
		if state.stopped {
			sr.Done()
		}
	}, helper.StreamScannerOptions{PingDataFunc: helper.PingData})
	if sawMessage {
		usageState.Done = state.stopped
		FinalizeStreamUsage(c, info, usageState)
	}
	usage := buildOpenAIStyleUsageFromClaudeUsage(usageState.Usage)
	if streamErr == nil {
		switch {
		case scan.Reason == helper.StreamScannerClientDisconnected:
			streamErr = types.NewClientDisconnectedError(fmt.Errorf("Claude conversion client disconnected: %v", scan.Err))
		case !state.stopped:
			streamErr = claudeConversionResponseError(fmt.Errorf("Claude stream ended before message_stop (%s): %v", scan.Reason, scan.Err))
		case scan.Err != nil:
			streamErr = claudeConversionResponseError(scan.Err)
		}
	}
	if streamErr == nil {
		if _, err := state.complete(&usage); err != nil && streamErr == nil {
			streamErr = claudeConversionResponseError(err)
		}
	}
	if streamErr != nil {
		if info.StreamStatus != nil {
			info.StreamStatus.EndError = streamErr
			if info.StreamStatus.EndReason == relaycommon.StreamEndReasonEOF || info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone {
				info.StreamStatus.EndReason = relaycommon.StreamEndReasonHandlerStop
			}
		}
		if streamErr.GetErrorCode() == types.ErrorCodeClientDisconnected && info.StreamStatus != nil {
			info.StreamStatus.EndReason = relaycommon.StreamEndReasonClientGone
			info.StreamStatus.EndError = streamErr
		}
		if state.started && c.Writer.Written() {
			types.ErrOptionWithSkipRetry()(streamErr)
			if streamErr.GetErrorCode() != types.ErrorCodeClientDisconnected {
				if info.RelayFormat == types.RelayFormatOpenAIResponses {
					failed := state.response("failed", &usage)
					failed["error"] = streamErr.ToOpenAIError()
					_ = state.event("response.failed", map[string]any{"response": failed})
				}
			}
		}
		return usageState.Usage, streamErr
	}
	return usageState.Usage, nil
}
