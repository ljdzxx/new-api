package xiaomi

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func ChatCompletionsToResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	var chatResp dto.OpenAITextResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	if err = common.Unmarshal(responseBody, &chatResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := chatResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	responsesResp, usage, err := service.ChatCompletionsResponseToResponsesResponse(&chatResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	encoded, err := common.Marshal(responsesResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	service.IOCopyBytesGracefully(c, resp, encoded)
	return usage, nil
}

func ChatCompletionsToResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	responseID := "resp_" + strings.TrimPrefix(relayhelper.GetResponseID(c), "chatcmpl-")
	outputID := "msg_" + strings.TrimPrefix(responseID, "resp_")
	createdAt := int(time.Now().Unix())
	model := info.UpstreamModelName
	usage := &dto.Usage{}
	var outputText strings.Builder

	relayhelper.StreamScannerHandler(c, resp, info, func(data string) bool {
		var chunk dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
			logger.LogError(c, "failed to unmarshal xiaomi chat stream chunk: "+err.Error())
			return true
		}

		if chunk.Id != "" {
			responseID = "resp_" + strings.TrimPrefix(chunk.Id, "chatcmpl-")
			outputID = "msg_" + strings.TrimPrefix(responseID, "resp_")
		}
		if chunk.Created != 0 {
			createdAt = int(chunk.Created)
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if service.ValidUsage(chunk.Usage) {
			*usage = *chunk.Usage
			normalizeUsageForResponses(usage)
		}

		for _, choice := range chunk.Choices {
			delta := choice.Delta.GetContentString()
			if delta == "" {
				delta = choice.Delta.GetReasoningContent()
			}
			if delta == "" {
				continue
			}

			outputText.WriteString(delta)
			event := dto.ResponsesStreamResponse{
				Type:  "response.output_text.delta",
				Delta: delta,
			}
			eventData, err := common.Marshal(event)
			if err != nil {
				logger.LogError(c, "failed to marshal responses delta chunk: "+err.Error())
				return false
			}
			relayhelper.ResponseChunkData(c, event, string(eventData))
		}
		return true
	})

	if !service.ValidUsage(usage) {
		usage = service.ResponseText2Usage(c, outputText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	normalizeUsageForResponses(usage)

	finalResponse := &dto.OpenAIResponsesResponse{
		ID:        responseID,
		Object:    "response",
		CreatedAt: createdAt,
		Model:     model,
		Usage:     usage,
		Output: []dto.ResponsesOutput{{
			Type:   "message",
			ID:     outputID,
			Status: "completed",
			Role:   "assistant",
			Content: []dto.ResponsesOutputContent{{
				Type:        "output_text",
				Text:        outputText.String(),
				Annotations: []interface{}{},
			}},
		}},
	}
	finalResponse.Status, _ = common.Marshal("completed")

	completedEvent := dto.ResponsesStreamResponse{
		Type:     "response.completed",
		Response: finalResponse,
	}
	completedData, err := common.Marshal(completedEvent)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	relayhelper.ResponseChunkData(c, completedEvent, string(completedData))
	return usage, nil
}

func normalizeUsageForResponses(usage *dto.Usage) {
	if usage == nil {
		return
	}
	if usage.InputTokens == 0 && usage.PromptTokens != 0 {
		usage.InputTokens = usage.PromptTokens
	}
	if usage.OutputTokens == 0 && usage.CompletionTokens != 0 {
		usage.OutputTokens = usage.CompletionTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if usage.InputTokensDetails == nil {
		usage.InputTokensDetails = &dto.InputTokenDetails{
			CachedTokens: usage.PromptTokensDetails.CachedTokens,
			ImageTokens:  usage.PromptTokensDetails.ImageTokens,
			AudioTokens:  usage.PromptTokensDetails.AudioTokens,
		}
	}
}
