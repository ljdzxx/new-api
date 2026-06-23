package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
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
	if info.ChannelMeta == nil {
		info.InitChannelMeta(c)
	}
	responseText, err := resolveMockTestResponseText(c, info)
	if err != nil {
		return types.NewError(fmt.Errorf("mock js handler failed: %w", err), types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
	}
	info.SetFirstResponseTime()
	info.ShouldIncludeUsage = true
	usage := mockTestUsage(c, info, responseText)

	var apiErr *types.NewAPIError
	if info.IsStream {
		if info.RelayFormat == types.RelayFormatOpenAI && info.RelayMode == relayconstant.RelayModeCompletions {
			apiErr = mockTestCompletionsStream(c, info, usage, responseText)
		} else {
			switch info.RelayFormat {
			case types.RelayFormatOpenAIResponses:
				apiErr = mockTestResponsesStream(c, info, usage, responseText)
			case types.RelayFormatOpenAIResponsesCompaction:
				apiErr = mockTestResponsesCompaction(c, info, usage, responseText)
			default:
				apiErr = mockTestChatStream(c, info, usage, responseText)
			}
		}
		if apiErr == nil {
			recordMockTestConsumeLog(c, info, usage)
		}
		return apiErr
	}

	switch info.RelayFormat {
	case types.RelayFormatClaude:
		c.JSON(http.StatusOK, service.ResponseOpenAI2Claude(mockTestOpenAIResponse(c, info, usage, responseText), info))
	case types.RelayFormatGemini:
		c.JSON(http.StatusOK, service.ResponseOpenAI2Gemini(mockTestOpenAIResponse(c, info, usage, responseText), info))
	case types.RelayFormatOpenAIResponses:
		c.JSON(http.StatusOK, mockTestResponsesResponse(c, info, usage, responseText))
	case types.RelayFormatOpenAIResponsesCompaction:
		apiErr = mockTestResponsesCompaction(c, info, usage, responseText)
	default:
		if info.RelayMode == relayconstant.RelayModeCompletions {
			c.JSON(http.StatusOK, mockTestCompletionsResponse(c, info, usage, responseText))
		} else {
			c.JSON(http.StatusOK, mockTestOpenAIResponse(c, info, usage, responseText))
		}
	}
	if apiErr == nil {
		recordMockTestConsumeLog(c, info, usage)
	}
	return apiErr
}

func resolveMockTestResponseText(c *gin.Context, info *relaycommon.RelayInfo) (string, error) {
	if c == nil || info == nil || info.ChannelMeta == nil {
		return mockTestResponseText, nil
	}
	script := getMockJSHandlerScript(c, info.ChannelSetting)
	if script == "" {
		return mockTestResponseText, nil
	}
	body, err := getMockJSBodyText(c)
	if err != nil {
		return "", err
	}
	return runMockJSProcess(script, body)
}

func mockTestUsage(c *gin.Context, info *relaycommon.RelayInfo, responseText string) dto.Usage {
	promptTokens := 0
	if info.Request != nil {
		if meta := info.Request.GetTokenCountMeta(); meta != nil && meta.CombineText != "" {
			promptTokens = service.CountTextToken(meta.CombineText, info.OriginModelName)
		}
	}
	completionTokens := service.CountTextToken(responseText, info.OriginModelName)
	if completionTokens <= 0 {
		completionTokens = service.EstimateTokenByModel(info.OriginModelName, responseText)
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

func recordMockTestConsumeLog(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage) {
	if c == nil || info == nil {
		return
	}
	userID := info.UserId
	if userID == 0 {
		userID = common.GetContextKeyInt(c, constant.ContextKeyUserId)
	}
	if userID == 0 && info.IsChannelTest {
		userID = 1
	}
	if userID == 0 {
		return
	}

	tokenName := c.GetString("token_name")
	if tokenName == "" {
		tokenName = "模型测试"
	}
	content := "Mock测试"
	if info.IsChannelTest {
		content = "模型测试"
	}

	requestPath := info.RequestURLPath
	if c.Request != nil && c.Request.URL != nil && c.Request.URL.Path != "" {
		requestPath = c.Request.URL.Path
	} else if idx := strings.Index(requestPath, "?"); idx != -1 {
		requestPath = requestPath[:idx]
	}
	other := map[string]interface{}{
		"mock_test":        true,
		"usage_source":     "mock_test",
		"model_ratio":      0,
		"model_price":      -1,
		"group_ratio":      1,
		"completion_ratio": 0,
		"cache_ratio":      1,
	}
	if !info.FirstResponseTime.IsZero() && !info.StartTime.IsZero() {
		other["frt"] = float64(info.FirstResponseTime.UnixMilli() - info.StartTime.UnixMilli())
	}
	if requestPath != "" {
		other["request_path"] = requestPath
	}
	if len(info.RequestConversionChain) > 0 {
		chain := make([]string, 0, len(info.RequestConversionChain))
		for _, format := range info.RequestConversionChain {
			chain = append(chain, string(format))
		}
		other["request_conversion"] = chain
	}
	if info.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		other["claude"] = true
	}

	adminInfo := map[string]interface{}{
		"use_channel": c.GetStringSlice("use_channel"),
	}
	if common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
		adminInfo["is_multi_key"] = true
		adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	}
	if common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens) {
		adminInfo["local_count_tokens"] = true
	}
	service.AppendChannelAffinityAdminInfo(c, adminInfo)
	other["admin_info"] = adminInfo

	useTimeSeconds := 0
	if !info.StartTime.IsZero() {
		useTimeSeconds = int(time.Since(info.StartTime).Seconds())
	}
	model.RecordConsumeLog(c, userID, model.RecordConsumeLogParams{
		ChannelId:        info.ChannelId,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		ModelName:        info.OriginModelName,
		TokenName:        tokenName,
		Quota:            0,
		Content:          content,
		TokenId:          info.TokenId,
		UseTimeSeconds:   useTimeSeconds,
		IsStream:         info.IsStream,
		Group:            info.UsingGroup,
		Other:            other,
	})
}

func mockTestOpenAIResponse(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage, responseText string) *dto.OpenAITextResponse {
	message := dto.Message{Role: "assistant"}
	message.SetStringContent(responseText)
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

func mockTestCompletionsResponse(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage, responseText string) gin.H {
	return gin.H{
		"id":      mockTestID(c, "cmpl-mock-"),
		"object":  "text_completion",
		"created": common.GetTimestamp(),
		"model":   mockTestModel(info),
		"choices": []gin.H{
			{
				"index":         0,
				"text":          responseText,
				"finish_reason": constant.FinishReasonStop,
			},
		},
		"usage": usage,
	}
}

func mockTestCompletionsStream(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage, responseText string) *types.NewAPIError {
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
				{"index": 0, "text": responseText, "finish_reason": nil},
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

func mockTestChatStream(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage, responseText string) *types.NewAPIError {
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
						Content: common.GetPointer(responseText),
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

func mockTestResponsesResponse(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage, responseText string) *dto.OpenAIResponsesResponse {
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
						Text:        responseText,
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

func mockTestResponsesStream(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage, responseText string) *types.NewAPIError {
	helper.SetEventStreamHeaders(c)
	response := mockTestResponsesResponse(c, info, usage, responseText)
	outputIndex := 0
	contentIndex := 0
	events := []dto.ResponsesStreamResponse{
		{Type: "response.created", Response: response},
		{Type: dto.ResponsesOutputTypeItemAdded, OutputIndex: &outputIndex, Item: &response.Output[0]},
		{Type: "response.output_text.delta", OutputIndex: &outputIndex, ContentIndex: &contentIndex, ItemID: response.Output[0].ID, Delta: responseText},
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

func mockTestResponsesCompaction(c *gin.Context, info *relaycommon.RelayInfo, usage dto.Usage, responseText string) *types.NewAPIError {
	output, err := common.Marshal(responseText)
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
