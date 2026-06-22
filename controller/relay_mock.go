package controller

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	openairelay "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const mockTestResponseText = "Hi. What can I do for you?"

func shouldMockTestRelay(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if c == nil || info == nil {
		return false
	}
	channelSetting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	if !ok || !channelSetting.MockTest {
		return false
	}
	switch info.RelayFormat {
	case types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini,
		types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		if info.ChannelMeta == nil {
			info.InitChannelMeta(c)
		}
		return true
	default:
		return false
	}
}

func handleMockTestRelay(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if c == nil || info == nil {
		return types.NewError(fmt.Errorf("invalid mock relay context"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	info.SetFirstResponseTime()
	info.ShouldIncludeUsage = true
	usage := mockTestUsage(c, info)

	if info.IsStream {
		if info.RelayFormat == types.RelayFormatOpenAI && info.RelayMode == relayconstant.RelayModeCompletions {
			return mockTestCompletionsStream(c, info, usage)
		}
		switch info.RelayFormat {
		case types.RelayFormatOpenAIResponses:
			return mockTestResponsesStream(c, info, usage)
		case types.RelayFormatOpenAIResponsesCompaction:
			return mockTestResponsesCompaction(c, info, usage)
		default:
			return mockTestChatStream(c, info, usage)
		}
	}

	switch info.RelayFormat {
	case types.RelayFormatClaude:
		c.JSON(http.StatusOK, service.ResponseOpenAI2Claude(mockTestOpenAIResponse(c, info, usage), info))
	case types.RelayFormatGemini:
		c.JSON(http.StatusOK, service.ResponseOpenAI2Gemini(mockTestOpenAIResponse(c, info, usage), info))
	case types.RelayFormatOpenAIResponses:
		c.JSON(http.StatusOK, mockTestResponsesResponse(c, info, usage))
	case types.RelayFormatOpenAIResponsesCompaction:
		return mockTestResponsesCompaction(c, info, usage)
	default:
		if info.RelayMode == relayconstant.RelayModeCompletions {
			c.JSON(http.StatusOK, mockTestCompletionsResponse(c, info, usage))
		} else {
			c.JSON(http.StatusOK, mockTestOpenAIResponse(c, info, usage))
		}
	}
	return nil
}

func mockTestUsage(c *gin.Context, info *relaycommon.RelayInfo) dto.Usage {
	promptTokens := 0
	if info.Request != nil {
		if meta := info.Request.GetTokenCountMeta(); meta != nil && meta.CombineText != "" {
			promptTokens = service.CountTextToken(meta.CombineText, info.OriginModelName)
		}
	}
	completionTokens := service.CountTextToken(mockTestResponseText, info.OriginModelName)
	if completionTokens <= 0 {
		completionTokens = service.EstimateTokenByModel(info.OriginModelName, mockTestResponseText)
	}
	info.SetEstimatePromptTokens(promptTokens)
	common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	return dto.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
		InputTokens:      promptTokens,
		OutputTokens:     completionTokens,
		PromptTokensDetails: dto.InputTokenDetails{
			TextTokens: promptTokens,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{
			TextTokens: completionTokens,
		},
		InputTokensDetails: &dto.InputTokenDetails{
			TextTokens: promptTokens,
		},
		UsageSource: "mock_test",
	}
}

func mockTestID(c *gin.Context, prefix string) string {
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		requestID = common.GetUUID()
	}
	return prefix + requestID
}

func mockTestModel(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.UpstreamModelName != "" {
		return info.UpstreamModelName
	}
	return info.OriginModelName
}

func mockTestOpenAIResponse(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage) *dto.OpenAITextResponse {
	message := dto.Message{Role: "assistant"}
	message.SetStringContent(mockTestResponseText)
	return &dto.OpenAITextResponse{
		Id:      mockTestID(c, "chatcmpl-mock-"),
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Model:   mockTestModel(info),
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      message,
				FinishReason: constant.FinishReasonStop,
			},
		},
		Usage: usage,
	}
}

func mockTestCompletionsResponse(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage) gin.H {
	return gin.H{
		"id":      mockTestID(c, "cmpl-mock-"),
		"object":  "text_completion",
		"created": common.GetTimestamp(),
		"model":   mockTestModel(info),
		"choices": []gin.H{
			{
				"index":         0,
				"text":          mockTestResponseText,
				"finish_reason": constant.FinishReasonStop,
			},
		},
		"usage": usage,
	}
}

func mockTestCompletionsStream(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage) *types.NewAPIError {
	helper.SetEventStreamHeaders(c)
	id := mockTestID(c, "cmpl-mock-")
	created := common.GetTimestamp()
	chunks := []gin.H{
		{
			"id":      id,
			"object":  "text_completion",
			"created": created,
			"model":   mockTestModel(info),
			"choices": []gin.H{
				{"index": 0, "text": mockTestResponseText, "finish_reason": nil},
			},
		},
		{
			"id":      id,
			"object":  "text_completion",
			"created": created,
			"model":   mockTestModel(info),
			"choices": []gin.H{
				{"index": 0, "text": "", "finish_reason": constant.FinishReasonStop},
			},
		},
		{
			"id":      id,
			"object":  "text_completion",
			"created": created,
			"model":   mockTestModel(info),
			"choices": []gin.H{},
			"usage":   usage,
		},
	}
	for _, chunk := range chunks {
		data, err := common.Marshal(chunk)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
		}
		if err := helper.StringData(c, string(data)); err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
		}
	}
	helper.Done(c)
	return nil
}

func mockTestChatStream(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage) *types.NewAPIError {
	helper.SetEventStreamHeaders(c)
	id := mockTestID(c, "chatcmpl-mock-")
	created := common.GetTimestamp()
	modelName := mockTestModel(info)
	chunks := []*dto.ChatCompletionsStreamResponse{
		helper.GenerateStartEmptyResponse(id, created, modelName, nil),
		{
			Id:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   modelName,
			Choices: []dto.ChatCompletionsStreamResponseChoice{
				{
					Index: 0,
					Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
						Content: common.GetPointer(mockTestResponseText),
					},
				},
			},
		},
		helper.GenerateStopResponse(id, created, modelName, constant.FinishReasonStop),
	}

	lastData := ""
	for _, chunk := range chunks {
		data, err := common.Marshal(chunk)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
		}
		lastData = string(data)
		if err = openairelay.HandleStreamFormat(c, info, lastData, info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent); err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
		}
	}
	openairelay.HandleFinalResponse(c, info, lastData, id, created, modelName, "", &usage, false)
	return nil
}

func mockTestResponsesResponse(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage) *dto.OpenAIResponsesResponse {
	status := json.RawMessage(`"completed"`)
	return &dto.OpenAIResponsesResponse{
		ID:        mockTestID(c, "resp_mock_"),
		Object:    "response",
		CreatedAt: int(common.GetTimestamp()),
		Status:    status,
		Model:     mockTestModel(info),
		Output: []dto.ResponsesOutput{
			{
				Type:   "message",
				ID:     mockTestID(c, "msg_mock_"),
				Status: "completed",
				Role:   "assistant",
				Content: []dto.ResponsesOutputContent{
					{
						Type:        "output_text",
						Text:        mockTestResponseText,
						Annotations: []interface{}{},
					},
				},
			},
		},
		ParallelToolCalls: true,
		Reasoning:         &dto.Reasoning{},
		Store:             true,
		Usage:             &usage,
	}
}

func mockTestResponsesStream(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage) *types.NewAPIError {
	helper.SetEventStreamHeaders(c)
	response := mockTestResponsesResponse(c, info, usage)
	outputIndex := 0
	contentIndex := 0
	events := []dto.ResponsesStreamResponse{
		{Type: "response.created", Response: response},
		{Type: dto.ResponsesOutputTypeItemAdded, OutputIndex: &outputIndex, Item: &response.Output[0]},
		{Type: "response.output_text.delta", OutputIndex: &outputIndex, ContentIndex: &contentIndex, ItemID: response.Output[0].ID, Delta: mockTestResponseText},
		{Type: "response.output_text.done", OutputIndex: &outputIndex, ContentIndex: &contentIndex, ItemID: response.Output[0].ID},
		{Type: dto.ResponsesOutputTypeItemDone, OutputIndex: &outputIndex, Item: &response.Output[0]},
		{Type: "response.completed", Response: response},
	}
	for _, event := range events {
		data, err := common.Marshal(event)
		if err != nil {
			return types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
		}
		helper.ResponseChunkData(c, event, string(data))
	}
	helper.Done(c)
	return nil
}

func mockTestResponsesCompaction(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage) *types.NewAPIError {
	output, err := common.Marshal(mockTestResponseText)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	c.JSON(http.StatusOK, &dto.OpenAIResponsesCompactionResponse{
		ID:        mockTestID(c, "resp_compact_mock_"),
		Object:    "response.compaction",
		CreatedAt: int(common.GetTimestamp()),
		Output:    json.RawMessage(output),
		Usage:     &usage,
	})
	return nil
}
