package gemini

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func geminiSyncContext() (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:streamGenerateContent", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-2.5-flash",
		RelayFormat:     types.RelayFormatGemini,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-flash"},
		PriceData:       types.PriceData{GlobalModelRatio: 2, GlobalRatioConfigSnapshot: true},
	}
	return c, recorder, info
}

func TestGeminiNativeUsageScalingLeavesBillingSnapshotUnchanged(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	original := settings.ResponseUsageScaleEnabled
	settings.ResponseUsageScaleEnabled = true
	t.Cleanup(func() { settings.ResponseUsageScaleEnabled = original })
	payload := "{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}],\"usageMetadata\":{\"promptTokenCount\":100,\"candidatesTokenCount\":10,\"totalTokenCount\":110,\"cachedContentTokenCount\":30,\"promptTokensDetails\":[{\"modality\":\"IMAGE\",\"tokenCount\":40}]},\"providerExtension\":true}"
	for _, stream := range []bool{false, true} {
		name := "nonstream"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			c, recorder, info := geminiSyncContext()
			body := payload
			if stream {
				body = "data: " + payload + "\n\n"
			}
			resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
			var usage *dto.Usage
			var apiErr *types.NewAPIError
			if stream {
				usage, apiErr = GeminiTextGenerationStreamHandler(c, info, resp)
			} else {
				usage, apiErr = GeminiTextGenerationHandler(c, info, resp)
			}
			require.Nil(t, apiErr)
			require.Equal(t, 100, usage.PromptTokens)
			require.Equal(t, 10, usage.CompletionTokens)
			require.Equal(t, 30, usage.BillingUsage.GeminiUsageMetadata.CachedContentTokenCount)
			response := recorder.Body.String()
			if stream {
				response = strings.TrimSpace(strings.TrimPrefix(response, "data: "))
			}
			require.EqualValues(t, 200, gjson.Get(response, "usageMetadata.promptTokenCount").Int())
			require.EqualValues(t, 20, gjson.Get(response, "usageMetadata.candidatesTokenCount").Int())
			require.EqualValues(t, 80, gjson.Get(response, "usageMetadata.promptTokensDetails.0.tokenCount").Int())
			require.True(t, gjson.Get(response, "providerExtension").Bool())
		})
	}
}

func TestGeminiStreamMergesPartialSnapshotsAndKeepsUsageOnStop(t *testing.T) {
	first := "{\"usageMetadata\":{\"promptTokenCount\":100,\"cachedContentTokenCount\":30,\"promptTokensDetails\":[{\"modality\":\"IMAGE\",\"tokenCount\":40}]}}"
	second := "{\"usageMetadata\":{\"candidatesTokenCount\":10,\"totalTokenCount\":110,\"promptTokensDetails\":[{\"modality\":\"IMAGE\",\"tokenCount\":40}]}}"
	for _, stopAfter := range []int{0, 2} {
		c, _, info := geminiSyncContext()
		resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: " + first + "\n\ndata: " + second + "\n\ndata: " + second + "\n\n"))}
		seen := 0
		usage, apiErr := geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool {
			seen++
			return seen != stopAfter
		})
		require.Nil(t, apiErr)
		require.Equal(t, 100, usage.PromptTokens)
		require.Equal(t, 10, usage.CompletionTokens)
		require.Equal(t, 40, usage.PromptTokensDetails.ImageTokens)
		require.Equal(t, 30, usage.BillingUsage.GeminiUsageMetadata.CachedContentTokenCount)
		require.Equal(t, 110, usage.TotalTokens)
	}
}

func TestGeminiMissingUsageEstimatesFunctionOutput(t *testing.T) {
	payload := "{\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"lookup_weather\",\"args\":{\"city\":\"Shanghai\"}}}]}}]}"
	for _, stream := range []bool{false, true} {
		c, _, info := geminiSyncContext()
		info.SetEstimatePromptTokens(25)
		body := payload
		if stream {
			body = "data: " + body + "\n\n"
		}
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
		var usage *dto.Usage
		var apiErr *types.NewAPIError
		if stream {
			usage, apiErr = geminiStreamHandler(c, info, resp, func(_ string, _ *dto.GeminiChatResponse) bool { return true })
		} else {
			usage, apiErr = GeminiTextGenerationHandler(c, info, resp)
		}
		require.Nil(t, apiErr)
		require.Equal(t, 25, usage.PromptTokens)
		require.Positive(t, usage.CompletionTokens)
		require.True(t, usage.BillingUsage.Estimated)
	}
}

func TestGeminiPromptOnlyUsageRetainsCacheWhenEstimatingOutput(t *testing.T) {
	c, _, info := geminiSyncContext()
	usage := buildUsageFromGeminiMetadata(dto.GeminiUsageMetadata{
		PromptTokenCount: 100, ToolUsePromptTokenCount: 5, TotalTokenCount: 105,
		CachedContentTokenCount: 30,
		PromptTokensDetails:     []dto.GeminiPromptTokensDetails{{Modality: "IMAGE", TokenCount: 40}},
	}, 0)
	original := usage.BillingUsage
	patchGeminiZeroCompletionUsage(c, info, &usage, "The requested weather forecast is sunny.", 0)
	require.Positive(t, usage.CompletionTokens)
	require.Equal(t, 30, usage.BillingUsage.GeminiUsageMetadata.CachedContentTokenCount)
	require.Equal(t, 5, usage.BillingUsage.GeminiUsageMetadata.ToolUsePromptTokenCount)
	require.Equal(t, original.GeminiUsageMetadata.PromptTokensDetails, usage.BillingUsage.GeminiUsageMetadata.PromptTokensDetails)
	require.Zero(t, original.GeminiUsageMetadata.CandidatesTokenCount)
}

func TestGeminiNativeAdaptorPreservesToolIdentityAndExplicitFalse(t *testing.T) {
	_, _, info := geminiSyncContext()
	request := &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{{Parts: []dto.GeminiPart{
		{FunctionCall: &dto.FunctionCall{ID: common.GetPointer("call_1"), FunctionName: "lookup"}},
	}}}, GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{IncludeThoughts: common.GetPointer(false)}}}
	_, err := (&Adaptor{}).ConvertGeminiRequest(nil, info, request)
	require.NoError(t, err)
	require.Equal(t, "call_1", *request.Contents[0].Parts[0].FunctionCall.ID)
	require.False(t, *request.GenerationConfig.ThinkingConfig.IncludeThoughts)
	for _, model := range []string{"gemini-3-pro-image", "gemini-3.1-flash-image"} {
		require.Contains(t, ModelList, model)
		require.True(t, model_setting.IsGeminiModelSupportImagine(model))
	}
}
