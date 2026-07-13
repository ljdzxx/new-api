package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestScaleOpenAIUsageForResponseDoesNotMutateRawUsage(t *testing.T) {
	raw := &dto.Usage{
		PromptTokens: 101, CompletionTokens: 9, TotalTokens: 110,
		InputTokens: 101, OutputTokens: 9,
		PromptTokensDetails:    dto.InputTokenDetails{CachedTokens: 11, TextTokens: 90},
		CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 3, TextTokens: 6},
		BillingUsage:           dto.NewOpenAIChatBillingUsage(&dto.Usage{PromptTokens: 101}),
	}

	client := ScaleOpenAIUsageForResponse(raw, 1.25)

	require.NotSame(t, raw, client)
	assert.Equal(t, 126, client.PromptTokens)
	assert.Equal(t, 11, client.CompletionTokens)
	assert.Equal(t, 137, client.TotalTokens)
	assert.Equal(t, 13, client.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 3, client.CompletionTokenDetails.ReasoningTokens)
	assert.Nil(t, client.BillingUsage)
	assert.Equal(t, 101, raw.PromptTokens)
	assert.Equal(t, 110, raw.TotalTokens)
	assert.NotNil(t, raw.BillingUsage)
}

func TestPatchResponseUsageJSONPreservesUnknownFields(t *testing.T) {
	body := []byte(`{"id":"x","provider_extension":{"keep":true},"usage":{"prompt_tokens":101,"completion_tokens":9,"total_tokens":110,"prompt_tokens_details":{"cached_tokens":11},"billing_usage":{"source":"internal"}}}`)

	patched, err := PatchResponseUsageJSON(body, types.RelayFormatOpenAI, 1.25)
	require.NoError(t, err)
	assert.True(t, gjson.GetBytes(patched, "provider_extension.keep").Bool())
	assert.EqualValues(t, 126, gjson.GetBytes(patched, "usage.prompt_tokens").Int())
	assert.EqualValues(t, 11, gjson.GetBytes(patched, "usage.completion_tokens").Int())
	assert.EqualValues(t, 137, gjson.GetBytes(patched, "usage.total_tokens").Int())
	assert.EqualValues(t, 13, gjson.GetBytes(patched, "usage.prompt_tokens_details.cached_tokens").Int())
	assert.False(t, gjson.GetBytes(patched, "usage.billing_usage").Exists())
}

func TestPatchResponseUsageJSONRecomputesInputOutputTotal(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":101,"output_tokens":9,"total_tokens":110}}`)
	patched, err := PatchResponseUsageJSON(body, types.RelayFormatOpenAIAudio, 1.25)
	require.NoError(t, err)
	assert.EqualValues(t, 126, gjson.GetBytes(patched, "usage.input_tokens").Int())
	assert.EqualValues(t, 11, gjson.GetBytes(patched, "usage.output_tokens").Int())
	assert.EqualValues(t, 137, gjson.GetBytes(patched, "usage.total_tokens").Int())
}

func TestPatchSSEUsageLinePreservesFrame(t *testing.T) {
	line := []byte("data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":10,\"output_tokens\":3}}\r\n")
	patched, err := PatchSSEUsageLine(line, types.RelayFormatClaude, 2)
	require.NoError(t, err)
	assert.Equal(t, "data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":20,\"output_tokens\":6}}\r\n", string(patched))
}

func TestShouldScaleResponseUsageSkipsPassThroughChannel(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	originalScale := settings.ResponseUsageScaleEnabled
	originalPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ResponseUsageScaleEnabled = originalScale
		settings.PassThroughRequestEnabled = originalPassThrough
	})
	settings.ResponseUsageScaleEnabled = true
	settings.PassThroughRequestEnabled = false

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	assert.True(t, ShouldScaleResponseUsage(info))
	info.ChannelSetting.PassThroughBodyEnabled = true
	assert.False(t, ShouldScaleResponseUsage(info))
}
