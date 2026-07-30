package helper

import (
	"bytes"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ShouldScaleResponseUsage defines the single egress boundary for client-facing
// usage scaling. Request pass-through only controls the upstream request body;
// the explicit response-usage setting still controls the client response.
func ShouldScaleResponseUsage(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil || !model_setting.GetGlobalSettings().ResponseUsageScaleEnabled {
		return false
	}
	return true
}

func ResponseUsageRatio(info *relaycommon.RelayInfo) float64 {
	if !ShouldScaleResponseUsage(info) {
		return 1
	}
	return info.PriceData.GlobalModelRatio
}

func responseUsageRatioForInput(info *relaycommon.RelayInfo, inputTokens int64) float64 {
	if !ShouldScaleResponseUsage(info) {
		return 1
	}
	ReevaluateGlobalModelRatioForActualInput(info, inputTokens)
	return info.PriceData.GlobalModelRatio
}

func openAIResponseInputTokens(raw *dto.Usage) int64 {
	if raw == nil {
		return 0
	}
	inputTokens := raw.InputTokens
	if inputTokens == 0 {
		inputTokens = raw.PromptTokens
	}
	if raw.UsageSemantic == dto.BillingUsageSemanticAnthropic {
		inputTokens += raw.PromptTokensDetails.CachedTokens + raw.PromptTokensDetails.CacheCreationTokensTotal()
	}
	return int64(inputTokens)
}

func ScaleOpenAIUsageForRelayResponse(raw *dto.Usage, info *relaycommon.RelayInfo) *dto.Usage {
	return ScaleOpenAIUsageForResponse(raw, responseUsageRatioForInput(info, openAIResponseInputTokens(raw)))
}

func ScaleClaudeUsageForRelayResponse(raw *dto.ClaudeUsage, info *relaycommon.RelayInfo) *dto.ClaudeUsage {
	inputTokens := int64(0)
	if raw != nil {
		inputTokens = int64(raw.InputTokens + raw.CacheReadInputTokens + raw.GetCacheCreationTotalTokens())
	}
	return ScaleClaudeUsageForResponse(raw, responseUsageRatioForInput(info, inputTokens))
}

func ScaleGeminiUsageForRelayResponse(raw *dto.GeminiUsageMetadata, info *relaycommon.RelayInfo) *dto.GeminiUsageMetadata {
	inputTokens := int64(0)
	if raw != nil {
		inputTokens = int64(raw.PromptTokenCount)
	}
	return ScaleGeminiUsageForResponse(raw, responseUsageRatioForInput(info, inputTokens))
}

func ScaleRealtimeUsageForRelayResponse(raw *dto.RealtimeUsage, info *relaycommon.RelayInfo) *dto.RealtimeUsage {
	inputTokens := int64(0)
	if raw != nil {
		inputTokens = int64(raw.InputTokens)
	}
	return ScaleRealtimeUsageForResponse(raw, responseUsageRatioForInput(info, inputTokens))
}

// ScaleOpenAIUsageForResponse returns a detached client view. The raw usage and
// its BillingUsage sidecar remain untouched for settlement.
func ScaleOpenAIUsageForResponse(raw *dto.Usage, ratio float64) *dto.Usage {
	if raw == nil {
		return nil
	}
	client := *raw
	client.BillingUsage = nil
	client.PromptTokensDetails = scaleInputTokenDetails(raw.PromptTokensDetails, ratio)
	client.CompletionTokenDetails = scaleOutputTokenDetails(raw.CompletionTokenDetails, ratio)
	if raw.InputTokensDetails != nil {
		details := scaleInputTokenDetails(*raw.InputTokensDetails, ratio)
		client.InputTokensDetails = &details
	}
	client.PromptTokens = ScaleTokensByGlobalModelRatio(raw.PromptTokens, ratio)
	client.CompletionTokens = ScaleTokensByGlobalModelRatio(raw.CompletionTokens, ratio)
	client.InputTokens = ScaleTokensByGlobalModelRatio(raw.InputTokens, ratio)
	client.OutputTokens = ScaleTokensByGlobalModelRatio(raw.OutputTokens, ratio)
	client.PromptCacheHitTokens = ScaleTokensByGlobalModelRatio(raw.PromptCacheHitTokens, ratio)
	client.ClaudeCacheCreation5mTokens = ScaleTokensByGlobalModelRatio(raw.ClaudeCacheCreation5mTokens, ratio)
	client.ClaudeCacheCreation1hTokens = ScaleTokensByGlobalModelRatio(raw.ClaudeCacheCreation1hTokens, ratio)
	client.TotalTokens = client.PromptTokens + client.CompletionTokens
	return &client
}

func ScaleClaudeUsageForResponse(raw *dto.ClaudeUsage, ratio float64) *dto.ClaudeUsage {
	if raw == nil {
		return nil
	}
	client := *raw
	client.BillingUsage = nil
	client.InputTokens = ScaleTokensByGlobalModelRatio(raw.InputTokens, ratio)
	client.OutputTokens = ScaleTokensByGlobalModelRatio(raw.OutputTokens, ratio)
	client.CacheReadInputTokens = ScaleTokensByGlobalModelRatio(raw.CacheReadInputTokens, ratio)
	client.CacheCreationInputTokens = ScaleTokensByGlobalModelRatio(raw.CacheCreationInputTokens, ratio)
	client.ClaudeCacheCreation5mTokens = ScaleTokensByGlobalModelRatio(raw.ClaudeCacheCreation5mTokens, ratio)
	client.ClaudeCacheCreation1hTokens = ScaleTokensByGlobalModelRatio(raw.ClaudeCacheCreation1hTokens, ratio)
	if raw.CacheCreation != nil {
		cache := *raw.CacheCreation
		cache.Ephemeral5mInputTokens = ScaleTokensByGlobalModelRatio(cache.Ephemeral5mInputTokens, ratio)
		cache.Ephemeral1hInputTokens = ScaleTokensByGlobalModelRatio(cache.Ephemeral1hInputTokens, ratio)
		client.CacheCreation = &cache
	}
	return &client
}

func ScaleGeminiUsageForResponse(raw *dto.GeminiUsageMetadata, ratio float64) *dto.GeminiUsageMetadata {
	if raw == nil {
		return nil
	}
	client := *raw
	client.BillingUsage = nil
	client.PromptTokenCount = ScaleTokensByGlobalModelRatio(raw.PromptTokenCount, ratio)
	client.ToolUsePromptTokenCount = ScaleTokensByGlobalModelRatio(raw.ToolUsePromptTokenCount, ratio)
	client.CandidatesTokenCount = ScaleTokensByGlobalModelRatio(raw.CandidatesTokenCount, ratio)
	client.ThoughtsTokenCount = ScaleTokensByGlobalModelRatio(raw.ThoughtsTokenCount, ratio)
	client.CachedContentTokenCount = ScaleTokensByGlobalModelRatio(raw.CachedContentTokenCount, ratio)
	client.PromptTokensDetails = scaleGeminiDetails(raw.PromptTokensDetails, ratio)
	client.ToolUsePromptTokensDetails = scaleGeminiDetails(raw.ToolUsePromptTokensDetails, ratio)
	client.CandidatesTokensDetails = scaleGeminiDetails(raw.CandidatesTokensDetails, ratio)
	client.TotalTokenCount = ScaleTokensByGlobalModelRatio(raw.TotalTokenCount, ratio)
	return &client
}

func ScaleRealtimeUsageForResponse(raw *dto.RealtimeUsage, ratio float64) *dto.RealtimeUsage {
	if raw == nil {
		return nil
	}
	client := *raw
	client.InputTokens = ScaleTokensByGlobalModelRatio(raw.InputTokens, ratio)
	client.OutputTokens = ScaleTokensByGlobalModelRatio(raw.OutputTokens, ratio)
	client.InputTokenDetails = scaleInputTokenDetails(raw.InputTokenDetails, ratio)
	client.OutputTokenDetails = scaleOutputTokenDetails(raw.OutputTokenDetails, ratio)
	client.TotalTokens = client.InputTokens + client.OutputTokens
	return &client
}

func scaleInputTokenDetails(raw dto.InputTokenDetails, ratio float64) dto.InputTokenDetails {
	raw.CachedTokens = ScaleTokensByGlobalModelRatio(raw.CachedTokens, ratio)
	raw.CacheWriteTokens = ScaleTokensByGlobalModelRatio(raw.CacheWriteTokens, ratio)
	raw.TextTokens = ScaleTokensByGlobalModelRatio(raw.TextTokens, ratio)
	raw.AudioTokens = ScaleTokensByGlobalModelRatio(raw.AudioTokens, ratio)
	raw.ImageTokens = ScaleTokensByGlobalModelRatio(raw.ImageTokens, ratio)
	return raw
}

func scaleOutputTokenDetails(raw dto.OutputTokenDetails, ratio float64) dto.OutputTokenDetails {
	raw.TextTokens = ScaleTokensByGlobalModelRatio(raw.TextTokens, ratio)
	raw.AudioTokens = ScaleTokensByGlobalModelRatio(raw.AudioTokens, ratio)
	raw.ImageTokens = ScaleTokensByGlobalModelRatio(raw.ImageTokens, ratio)
	raw.ReasoningTokens = ScaleTokensByGlobalModelRatio(raw.ReasoningTokens, ratio)
	return raw
}

func scaleGeminiDetails(raw []dto.GeminiPromptTokensDetails, ratio float64) []dto.GeminiPromptTokensDetails {
	if raw == nil {
		return nil
	}
	client := make([]dto.GeminiPromptTokensDetails, len(raw))
	copy(client, raw)
	for i := range client {
		client[i].TokenCount = ScaleTokensByGlobalModelRatio(client[i].TokenCount, ratio)
	}
	return client
}

var openAIUsageTokenPaths = []string{
	"prompt_tokens", "completion_tokens", "input_tokens", "output_tokens", "prompt_cache_hit_tokens",
	"prompt_tokens_details.cached_tokens", "prompt_tokens_details.cache_write_tokens", "prompt_tokens_details.text_tokens",
	"prompt_tokens_details.audio_tokens", "prompt_tokens_details.image_tokens",
	"completion_tokens_details.text_tokens", "completion_tokens_details.audio_tokens", "completion_tokens_details.image_tokens",
	"completion_tokens_details.reasoning_tokens", "input_tokens_details.cached_tokens", "input_tokens_details.cache_write_tokens",
	"input_tokens_details.text_tokens", "input_tokens_details.audio_tokens", "input_tokens_details.image_tokens",
	"claude_cache_creation_5_m_tokens", "claude_cache_creation_1_h_tokens",
}

var claudeUsageTokenPaths = []string{
	"input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens",
	"cache_creation.ephemeral_5m_input_tokens", "cache_creation.ephemeral_1h_input_tokens",
	"claude_cache_creation_5_m_tokens", "claude_cache_creation_1_h_tokens",
}

var geminiUsageTokenPaths = []string{
	"promptTokenCount", "toolUsePromptTokenCount", "candidatesTokenCount", "thoughtsTokenCount", "cachedContentTokenCount", "totalTokenCount",
}

// PatchResponseUsageJSON changes only known token counters, preserving unknown
// provider fields. format is the client-facing protocol, never the upstream one.
func PatchResponseUsageJSON(data []byte, format types.RelayFormat, ratio float64) ([]byte, error) {
	if len(data) == 0 || !gjson.ValidBytes(data) {
		return data, nil
	}
	var err error
	switch format {
	case types.RelayFormatOpenAI, types.RelayFormatEmbedding, types.RelayFormatRerank, types.RelayFormatOpenAIAudio:
		data, err = patchUsageAt(data, "usage", openAIUsageTokenPaths, ratio)
		if err == nil {
			data, err = recomputeTotal(data, "usage", "prompt_tokens", "completion_tokens", "total_tokens")
		}
		if err == nil {
			data, err = recomputeTotal(data, "usage", "input_tokens", "output_tokens", "total_tokens")
		}
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		data, err = patchUsageAt(data, "usage", openAIUsageTokenPaths, ratio)
		if err == nil {
			data, err = recomputeTotal(data, "usage", "input_tokens", "output_tokens", "total_tokens")
		}
		if err == nil {
			data, err = patchUsageAt(data, "response.usage", openAIUsageTokenPaths, ratio)
		}
		if err == nil {
			data, err = recomputeTotal(data, "response.usage", "input_tokens", "output_tokens", "total_tokens")
		}
	case types.RelayFormatClaude:
		data, err = patchUsageAt(data, "usage", claudeUsageTokenPaths, ratio)
		if err == nil {
			data, err = patchUsageAt(data, "message.usage", claudeUsageTokenPaths, ratio)
		}
	case types.RelayFormatGemini:
		data, err = patchUsageAt(data, "usageMetadata", geminiUsageTokenPaths, ratio)
		if err == nil {
			data, err = patchGeminiDetailArrays(data, "usageMetadata", ratio)
		}
	case types.RelayFormatOpenAIRealtime:
		data, err = patchUsageAt(data, "response.usage", append(openAIUsageTokenPaths, "input_token_details.cached_tokens", "input_token_details.cache_write_tokens", "input_token_details.text_tokens", "input_token_details.audio_tokens", "output_token_details.text_tokens", "output_token_details.audio_tokens", "output_token_details.reasoning_tokens"), ratio)
		if err == nil {
			data, err = recomputeTotal(data, "response.usage", "input_tokens", "output_tokens", "total_tokens")
		}
	}
	if err == nil {
		data, err = removeBillingUsageSidecars(data)
	}
	return data, err
}

// PatchResponseUsageJSONForRelay resolves thresholded global ratios from the
// actual upstream usage before changing the client-facing response.
func PatchResponseUsageJSONForRelay(data []byte, format types.RelayFormat, info *relaycommon.RelayInfo) ([]byte, error) {
	if !ShouldScaleResponseUsage(info) {
		return data, nil
	}
	inputTokens, ok := responseInputTokensFromJSON(data, format)
	ratio := ResponseUsageRatio(info)
	if ok {
		ratio = responseUsageRatioForInput(info, inputTokens)
	}
	return PatchResponseUsageJSON(data, format, ratio)
}

func responseInputTokensFromJSON(data []byte, format types.RelayFormat) (int64, bool) {
	var usagePath string
	switch format {
	case types.RelayFormatOpenAI, types.RelayFormatEmbedding, types.RelayFormatRerank, types.RelayFormatOpenAIAudio:
		usagePath = "usage"
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction, types.RelayFormatOpenAIRealtime:
		usagePath = "response.usage"
		if !gjson.GetBytes(data, usagePath).Exists() {
			usagePath = "usage"
		}
	case types.RelayFormatClaude:
		usagePath = "usage"
		if !gjson.GetBytes(data, usagePath).Exists() {
			usagePath = "message.usage"
		}
	case types.RelayFormatGemini:
		usagePath = "usageMetadata"
	default:
		return 0, false
	}
	usage := gjson.GetBytes(data, usagePath)
	if !usage.Exists() {
		return 0, false
	}

	switch format {
	case types.RelayFormatClaude:
		input := usage.Get("input_tokens")
		if !input.Exists() {
			return 0, false
		}
		cacheCreation := usage.Get("cache_creation_input_tokens").Int()
		if cacheCreation == 0 {
			cacheCreation = usage.Get("cache_creation.ephemeral_5m_input_tokens").Int() + usage.Get("cache_creation.ephemeral_1h_input_tokens").Int()
		}
		return input.Int() + usage.Get("cache_read_input_tokens").Int() + cacheCreation, true
	case types.RelayFormatGemini:
		input := usage.Get("promptTokenCount")
		return input.Int(), input.Exists()
	default:
		input := usage.Get("input_tokens")
		if input.Exists() {
			return input.Int(), true
		}
		prompt := usage.Get("prompt_tokens")
		return prompt.Int(), prompt.Exists()
	}
}

func PatchSSEUsageLine(line []byte, format types.RelayFormat, ratio float64) ([]byte, error) {
	trimmed := bytes.TrimRight(line, "\r\n")
	ending := line[len(trimmed):]
	prefixIndex := bytes.Index(trimmed, []byte("data:"))
	if prefixIndex < 0 {
		return line, nil
	}
	payloadStart := prefixIndex + len("data:")
	for payloadStart < len(trimmed) && (trimmed[payloadStart] == ' ' || trimmed[payloadStart] == '\t') {
		payloadStart++
	}
	payload := trimmed[payloadStart:]
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return line, nil
	}
	patched, err := PatchResponseUsageJSON(payload, format, ratio)
	if err != nil || bytes.Equal(patched, payload) {
		return line, err
	}
	out := make([]byte, 0, payloadStart+len(patched)+len(ending))
	out = append(out, trimmed[:payloadStart]...)
	out = append(out, patched...)
	out = append(out, ending...)
	return out, nil
}

func ObjectDataWithScaledUsage(c *gin.Context, info *relaycommon.RelayInfo, format types.RelayFormat, object interface{}) error {
	data, err := common.Marshal(object)
	if err != nil {
		return err
	}
	if ShouldScaleResponseUsage(info) {
		data, err = PatchResponseUsageJSONForRelay(data, format, info)
		if err != nil {
			return err
		}
	}
	return StringData(c, string(data))
}

func patchUsageAt(data []byte, prefix string, paths []string, ratio float64) ([]byte, error) {
	if !gjson.GetBytes(data, prefix).Exists() {
		return data, nil
	}
	var err error
	for _, suffix := range paths {
		path := prefix + "." + suffix
		value := gjson.GetBytes(data, path)
		if !value.Exists() || value.Type != gjson.Number {
			continue
		}
		data, err = sjson.SetBytes(data, path, ScaleTokensByGlobalModelRatio(int(value.Int()), ratio))
		if err != nil {
			return nil, fmt.Errorf("patch response usage %s: %w", path, err)
		}
	}
	return data, nil
}

func recomputeTotal(data []byte, prefix, inputKey, outputKey, totalKey string) ([]byte, error) {
	input := gjson.GetBytes(data, prefix+"."+inputKey)
	output := gjson.GetBytes(data, prefix+"."+outputKey)
	if !input.Exists() || !output.Exists() {
		return data, nil
	}
	return sjson.SetBytes(data, prefix+"."+totalKey, input.Int()+output.Int())
}

func patchGeminiDetailArrays(data []byte, prefix string, ratio float64) ([]byte, error) {
	for _, name := range []string{"promptTokensDetails", "toolUsePromptTokensDetails", "candidatesTokensDetails"} {
		items := gjson.GetBytes(data, prefix+"."+name).Array()
		for i, item := range items {
			value := item.Get("tokenCount")
			if !value.Exists() {
				continue
			}
			path := fmt.Sprintf("%s.%s.%d.tokenCount", prefix, name, i)
			var err error
			data, err = sjson.SetBytes(data, path, ScaleTokensByGlobalModelRatio(int(value.Int()), ratio))
			if err != nil {
				return nil, err
			}
		}
	}
	return data, nil
}

func removeBillingUsageSidecars(data []byte) ([]byte, error) {
	var err error
	for _, path := range []string{"usage.billing_usage", "response.usage.billing_usage", "message.usage.billing_usage", "usageMetadata.billing_usage"} {
		if !gjson.GetBytes(data, path).Exists() {
			continue
		}
		data, err = sjson.DeleteBytes(data, path)
		if err != nil {
			return nil, err
		}
	}
	return data, nil
}
