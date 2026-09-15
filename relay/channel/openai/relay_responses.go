package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// Only the client copy is scaled. responsesResponse remains raw for billing.
	if helper.ShouldScaleResponseUsage(info) {
		responseBody, err = helper.PatchResponseUsageJSONForRelay(responseBody, types.RelayFormatOpenAIResponses, info)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	}
	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CacheWriteTokens = responsesResponse.Usage.InputTokensDetails.CacheWriteTokens
		}
		usage.BillingUsage = dto.NewOpenAIResponsesBillingUsage(responsesResponse.Usage)
	}
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// 解析 Tools 用量
	for _, tool := range responsesResponse.Tools {
		buildToolinfo, ok := info.ResponsesUsageInfo.BuiltInTools[common.Interface2String(tool["type"])]
		if !ok || buildToolinfo == nil {
			logger.LogError(c, fmt.Sprintf("BuiltInTools not found for tool type: %v", tool["type"]))
			continue
		}
		buildToolinfo.CallCount++
	}
	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	completed := false
	var streamErr *types.NewAPIError
	lastEventType := ""
	forwardedEvents := 0
	lastForwardedAt := time.Now()

	scanResult := helper.StreamScannerHandlerWithOptions(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err == nil {
			lastEventType = streamResponse.Type
			if streamResponse.Type == "" {
				streamErr = types.NewOpenAIError(fmt.Errorf("upstream sent a Responses event without a type"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				sr.Stop(streamErr)
				return
			}
			failed := streamResponse.Type == "response.failed" || streamResponse.Type == "response.incomplete" || streamResponse.Type == "error"
			if failed {
				streamErr = types.NewOpenAIError(fmt.Errorf("upstream returned %s", streamResponse.Type), types.ErrorCodeBadResponse, http.StatusBadGateway)
				if streamResponse.Response != nil {
					if upstreamErr := streamResponse.Response.GetOpenAIError(); upstreamErr != nil {
						streamErr = types.WithOpenAIError(*upstreamErr, http.StatusBadGateway)
					}
				} else if streamResponse.Type == "error" {
					var upstreamErr types.OpenAIError
					if common.UnmarshalJsonStr(data, &upstreamErr) == nil && upstreamErr.Message != "" {
						streamErr = types.WithOpenAIError(upstreamErr, http.StatusBadGateway)
					}
				}
				sr.Stop(streamErr)
			}
			clientData := data
			if !failed && helper.ShouldScaleResponseUsage(info) {
				patched, patchErr := helper.PatchResponseUsageJSONForRelay(common.StringToByteSlice(data), types.RelayFormatOpenAIResponses, info)
				if patchErr != nil {
					logger.LogError(c, "failed to scale responses stream usage: "+patchErr.Error())
					streamErr = types.NewOpenAIError(patchErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
					sr.Stop(patchErr)
					return
				}
				clientData = string(patched)
			}
			if !failed {
				if err := sendResponsesStreamData(c, streamResponse, clientData); err != nil {
					logger.LogWarn(c, "failed to write responses stream data: "+err.Error())
					streamErr = types.NewClientDisconnectedError(err)
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
					sr.Stop(err)
					return
				}
				forwardedEvents++
				lastForwardedAt = time.Now()
			}
			switch streamResponse.Type {
			case "response.completed", "response.failed", "response.incomplete":
				completed = streamResponse.Type == "response.completed"
				if completed {
					sr.Done()
				}
				if streamResponse.Response != nil {
					if streamResponse.Response.Usage != nil {
						if streamResponse.Response.Usage.InputTokens != 0 {
							usage.PromptTokens = streamResponse.Response.Usage.InputTokens
						}
						if streamResponse.Response.Usage.OutputTokens != 0 {
							usage.CompletionTokens = streamResponse.Response.Usage.OutputTokens
						}
						if streamResponse.Response.Usage.TotalTokens != 0 {
							usage.TotalTokens = streamResponse.Response.Usage.TotalTokens
						}
						if streamResponse.Response.Usage.InputTokensDetails != nil {
							usage.PromptTokensDetails.CachedTokens = streamResponse.Response.Usage.InputTokensDetails.CachedTokens
							usage.PromptTokensDetails.CacheWriteTokens = streamResponse.Response.Usage.InputTokensDetails.CacheWriteTokens
						}
						usage.BillingUsage = dto.NewOpenAIResponsesBillingUsage(streamResponse.Response.Usage)
					}
					if streamResponse.Response.HasImageGenerationCall() {
						c.Set("image_generation_call", true)
						c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
						c.Set("image_generation_call_size", streamResponse.Response.GetSize())
					}
				}
			case "response.output_text.delta":
				// 处理输出文本
				if streamResponse.Delta != "" {
					common.SetContextKey(c, constant.ContextKeyResponsesBillableStreamOutput, true)
					responseTextBuilder.WriteString(streamResponse.Delta)
				}
			case dto.ResponsesOutputTypeItemDone:
				// 函数调用处理
				if streamResponse.Item != nil {
					switch streamResponse.Item.Type {
					case dto.BuildInCallWebSearchCall:
						if info != nil && info.ResponsesUsageInfo != nil && info.ResponsesUsageInfo.BuiltInTools != nil {
							if webSearchTool, exists := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool != nil {
								common.SetContextKey(c, constant.ContextKeyResponsesBillableStreamOutput, true)
								webSearchTool.CallCount++
							}
						}
					case dto.BuildInCallFileSearchCall:
						if info != nil && info.ResponsesUsageInfo != nil && info.ResponsesUsageInfo.BuiltInTools != nil {
							if fileSearchTool, exists := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch]; exists && fileSearchTool != nil {
								common.SetContextKey(c, constant.ContextKeyResponsesBillableStreamOutput, true)
								fileSearchTool.CallCount++
							}
						}
					}
				}
			}
		} else {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			streamErr = types.NewOpenAIError(fmt.Errorf("invalid upstream Responses event: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			sr.Stop(err)
		}
	}, helper.StreamScannerOptions{
		PingDataFunc: sendResponsesKeepAlive,
	})

	if !completed {
		if streamErr == nil {
			if scanResult.Reason == helper.StreamScannerClientDisconnected {
				streamErr = types.NewClientDisconnectedError(fmt.Errorf("downstream disconnected before response.completed: %w", scanResult.Err))
			} else {
				streamErr = types.NewOpenAIError(fmt.Errorf("upstream Responses stream ended before response.completed (%s)", scanResult.Reason), types.ErrorCodeBadResponse, http.StatusBadGateway)
			}
		}
		if helper.ResponsesStreamStarted(c) || scanResult.Reason == helper.StreamScannerClientDisconnected {
			types.ErrOptionWithSkipRetry()(streamErr)
		}
		logEnd := logger.LogError
		if streamErr.GetErrorCode() == types.ErrorCodeClientDisconnected {
			logEnd = logger.LogWarn
		}
		logEnd(c, fmt.Sprintf(
			"responses stream ended before response.completed: reason=%s err=%v received_response_count=%d prompt_tokens=%d completion_tokens=%d total_tokens=%d output_text_bytes=%d last_event=%q forwarded_events=%d last_forwarded_ago_ms=%d downstream_context_error=%v downstream_remote=%q downstream_user_agent=%q",
			scanResult.Reason,
			scanResult.Err,
			info.ReceivedResponseCount,
			usage.PromptTokens,
			usage.CompletionTokens,
			usage.TotalTokens,
			responseTextBuilder.Len(),
			lastEventType,
			forwardedEvents,
			time.Since(lastForwardedAt).Milliseconds(),
			c.Request.Context().Err(),
			c.Request.RemoteAddr,
			c.Request.UserAgent(),
		))
	}

	if usage.CompletionTokens == 0 {
		// 计算输出文本的 token 数量
		tempStr := responseTextBuilder.String()
		if len(tempStr) > 0 {
			// 非正常结束，使用输出文本的 token 数量
			completionTokens := service.CountTextToken(tempStr, info.UpstreamModelName)
			usage.CompletionTokens = completionTokens
		}
	}

	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}

	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	return usage, streamErr
}

func sendResponsesKeepAlive(c *gin.Context) error {
	event := dto.ResponsesStreamResponse{
		Type: "response.keepalive",
	}
	data, err := common.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal responses keepalive failed: %w", err)
	}
	return helper.ResponseChunkData(c, event, string(data))
}
