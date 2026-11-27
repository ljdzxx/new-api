package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestGeminiConvertedCacheUsageScaling(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	original := settings.ResponseUsageScaleEnabled
	settings.ResponseUsageScaleEnabled = true
	t.Cleanup(func() { settings.ResponseUsageScaleEnabled = original })
	for _, details := range []bool{false, true} {
		usage := dto.Usage{PromptTokens: 1000, CompletionTokens: 5, TotalTokens: 1005}
		if details {
			usage.InputTokensDetails = &dto.InputTokenDetails{CachedTokens: 800}
		} else {
			usage.PromptTokensDetails.CachedTokens = 800
		}
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, PriceData: types.PriceData{GlobalRatioConfigSnapshot: true, GlobalModelRatio: 3}}
		full := ResponseOpenAI2Gemini(&dto.OpenAITextResponse{Usage: usage}, info)
		// The last SSE frame may consist entirely of usage.
		stream := StreamResponseOpenAI2Gemini(&dto.ChatCompletionsStreamResponse{Usage: &usage}, info)
		require.NotNil(t, stream)
		for _, response := range []*dto.GeminiChatResponse{full, stream} {
			require.Equal(t, 800, response.UsageMetadata.CachedContentTokenCount)
			require.Equal(t, 1000, response.UsageMetadata.PromptTokenCount)
			client := helper.ScaleGeminiUsageForRelayResponse(&response.UsageMetadata, info)
			require.Equal(t, 2400, client.CachedContentTokenCount)
			require.Equal(t, 3000, client.PromptTokenCount)
			require.Equal(t, 800, response.UsageMetadata.CachedContentTokenCount)
			require.NotNil(t, response.UsageMetadata.BillingUsage)
		}
	}
	for _, n := range []int{0, -1} {
		usage := dto.Usage{PromptTokensDetails: dto.InputTokenDetails{CachedTokens: n}, InputTokensDetails: &dto.InputTokenDetails{CachedTokens: n}}
		require.Zero(t, ResponseOpenAI2Gemini(&dto.OpenAITextResponse{Usage: usage}, nil).UsageMetadata.CachedContentTokenCount)
	}
}
