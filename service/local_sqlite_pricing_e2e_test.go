package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const localPricingSQLitePathEnv = "NEW_API_PRICING_TEST_SQLITE"

type localPricingOption struct {
	Key   string
	Value string
}

func loadLocalPricingOptions(t *testing.T) map[string]string {
	t.Helper()
	path := os.Getenv(localPricingSQLitePathEnv)
	if path == "" {
		path = filepath.Join("..", "one-api.db")
	}
	absPath, err := filepath.Abs(path)
	require.NoError(t, err)
	if _, err = os.Stat(absPath); err != nil {
		t.Skipf("local pricing SQLite is unavailable at %s; set %s to run this integration test", absPath, localPricingSQLitePathEnv)
	}
	db, err := gorm.Open(sqlite.Open("file:"+filepath.ToSlash(absPath)+"?mode=ro"), &gorm.Config{})
	require.NoError(t, err)
	var rows []localPricingOption
	require.NoError(t, db.Table("options").Select("key", "value").Where("key IN ?", []string{
		"ModelBillingPolicy", "GlobalModelRatio", "GroupRatio",
	}).Scan(&rows).Error)
	options := make(map[string]string, len(rows))
	for _, row := range rows {
		options[row.Key] = row.Value
	}
	require.NotEmpty(t, options["ModelBillingPolicy"])
	return options
}

func installLocalPricingOptions(t *testing.T, options map[string]string) billing_policy.Config {
	t.Helper()
	policyBackup := billing_policy.GetConfig()
	globalBackup := ratio_setting.GetGlobalModelRatio()
	groupBackup := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(policyBackup)))
		ratio_setting.SetGlobalModelRatio(globalBackup)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(groupBackup))
	})
	require.NoError(t, billing_policy.UpdateFromJSON(options["ModelBillingPolicy"]))
	if options["GlobalModelRatio"] != "" {
		var ratio float64
		require.NoError(t, common.UnmarshalJsonStr(options["GlobalModelRatio"], &ratio))
		ratio_setting.SetGlobalModelRatio(ratio)
	}
	if options["GroupRatio"] != "" {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(options["GroupRatio"]))
	}
	config := billing_policy.GetConfig()
	require.Equal(t, billing_policy.StateActive, config.State)
	for _, modelName := range []string{"gpt-5.6-sol", "claude-fable-5", "gemini-2.5-pro"} {
		_, ok := config.Policies[modelName]
		require.Truef(t, ok, "local SQLite policy must define %s", modelName)
	}
	return config
}

func newLocalPricingContext(path string) *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = mustPricingRequest(path)
	return ctx
}

func mustPricingRequest(path string) *http.Request {
	return httptest.NewRequest("POST", path, nil)
}

func prepareLocalPriceData(t *testing.T, ctx *gin.Context, modelName string, promptTokens int) *relaycommon.RelayInfo {
	t.Helper()
	info := &relaycommon.RelayInfo{
		OriginModelName: modelName,
		UserGroup:       "default",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelModelRatio: 1},
	}
	priceData, err := relayhelper.ModelPriceHelper(ctx, info, promptTokens, &types.TokenCountMeta{MaxTokens: 128})
	require.NoError(t, err)
	require.Greater(t, priceData.QuotaToPreConsume, 0)
	require.NotNil(t, info.BillingPolicySnapshot)
	info.PriceData = priceData
	info.FinalPreConsumedQuota = priceData.QuotaToPreConsume
	return info
}

func assertEveryConfiguredPolicyFieldIsBillable(t *testing.T, policy billing_policy.Policy) {
	t.Helper()
	usage := billing_policy.BillingUsage{
		TierInputTotalTokens: 200, TierOutputTotalTokens: 10,
		InputTokens: 1, OutputTokens: 1, CacheReadTokens: 1, CacheWriteTokens: 1,
		CacheWrite5mTokens: 1, CacheWrite1hTokens: 1, ImageInputTokens: 1,
		AudioInputTokens: 1, AudioOutputTokens: 1, ToolUsage: map[string]int64{},
	}
	for name := range policy.Tools {
		usage.ToolUsage[name] = 1
	}
	calculation, err := billing_policy.CalculateBilling(policy, usage, billing_policy.RequestContext{Now: time.Date(2026, 7, 13, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))})
	require.NoError(t, err)
	fields := make(map[string]bool, len(calculation.LineItems))
	for _, item := range calculation.LineItems {
		fields[item.Field] = true
		require.NotEqual(t, "0", item.CostUSD)
	}
	prices := calculation.Prices
	for name, price := range map[string]string{
		"input": prices.Input, "output": prices.Output, "cache_read": prices.CacheRead,
		"cache_write": prices.CacheWrite, "cache_write_5m": prices.CacheWrite5m,
		"cache_write_1h": prices.CacheWrite1h, "image_input": prices.ImageInput,
		"audio_input": prices.AudioInput, "audio_output": prices.AudioOutput,
	} {
		if price != "" {
			require.Truef(t, fields[name], "configured price field %s was not reached", name)
		}
	}
	for name := range policy.Tools {
		require.Truef(t, fields[name], "configured tool price %s was not reached", name)
	}
	if len(policy.Adjustments) > 0 {
		require.NotEmpty(t, calculation.AppliedAdjustments)
		require.NotEqual(t, "1", calculation.AdjustmentMultiplier)
	}
}

func TestLocalSQLiteModelPricingEndToEnd(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config := installLocalPricingOptions(t, loadLocalPricingOptions(t))

	for _, modelName := range []string{"gpt-5.6-sol", "claude-fable-5", "gemini-2.5-pro"} {
		t.Run(modelName+"_all_policy_fields", func(t *testing.T) {
			assertEveryConfiguredPolicyFieldIsBillable(t, config.Policies[modelName])
		})
	}

	t.Run("openai_native_usage_and_settlement", func(t *testing.T) {
		ctx := newLocalPricingContext("/v1/chat/completions")
		info := prepareLocalPriceData(t, ctx, "gpt-5.6-sol", 200)
		var response dto.OpenAITextResponse
		require.NoError(t, common.UnmarshalJsonStr(`{"id":"chatcmpl_local","model":"gpt-5.6-sol","object":"chat.completion","choices":[],"usage":{"prompt_tokens":200,"completion_tokens":50,"total_tokens":250,"prompt_tokens_details":{"cached_tokens":20,"cache_write_tokens":30,"text_tokens":145,"audio_tokens":0,"image_tokens":5},"completion_tokens_details":{"text_tokens":40,"audio_tokens":3,"image_tokens":7,"reasoning_tokens":0},"input_tokens":0,"output_tokens":0,"input_tokens_details":null,"claude_cache_creation_5_m_tokens":0,"claude_cache_creation_1_h_tokens":0}}`, &response))
		summary := calculateTextQuotaSummary(ctx, info, &response.Usage)
		require.NoError(t, summary.PolicyError)
		require.NotNil(t, summary.PolicyCalculation)
		require.Equal(t, "长报文", summary.PolicyCalculation.TierID)
		require.Greater(t, summary.Quota, 0)
		wire, err := common.Marshal(response)
		require.NoError(t, err)
		var returned map[string]any
		require.NoError(t, common.Unmarshal(wire, &returned))
		usage := returned["usage"].(map[string]any)
		completionDetails := usage["completion_tokens_details"].(map[string]any)
		require.Equal(t, float64(7), completionDetails["image_tokens"])
	})

	t.Run("anthropic_native_usage_round_trip_and_settlement", func(t *testing.T) {
		ctx := newLocalPricingContext("/v1/messages")
		info := prepareLocalPriceData(t, ctx, "claude-fable-5", 100)
		info.RelayFormat = types.RelayFormatClaude
		info.FinalRequestRelayFormat = types.RelayFormatClaude
		var response dto.ClaudeResponse
		require.NoError(t, common.UnmarshalJsonStr(`{"id":"msg_local","type":"message","model":"claude-fable-5","content":[],"usage":{"input_tokens":100,"cache_creation_input_tokens":30,"cache_read_input_tokens":20,"output_tokens":40,"cache_creation":{"ephemeral_5m_input_tokens":10,"ephemeral_1h_input_tokens":20},"server_tool_use":{"web_search_requests":2}}}`, &response))
		require.NotNil(t, response.Usage)
		canonical := &dto.Usage{
			PromptTokens: 100, CompletionTokens: 40, TotalTokens: 140,
			UsageSemantic:               dto.BillingUsageSemanticAnthropic,
			UsageSource:                 dto.BillingUsageSourceClaudeMessages,
			BillingUsage:                dto.NewClaudeMessagesBillingUsage(response.Usage),
			PromptTokensDetails:         dto.InputTokenDetails{CachedTokens: 20, CacheWriteTokens: 30},
			ClaudeCacheCreation5mTokens: 10, ClaudeCacheCreation1hTokens: 20,
		}
		summary := calculateTextQuotaSummary(ctx, info, canonical)
		require.NoError(t, summary.PolicyError)
		require.NotNil(t, summary.PolicyCalculation)
		require.Greater(t, summary.Quota, 0)
		restored := buildClaudeUsageFromOpenAIUsage(canonical)
		require.Equal(t, response.Usage, restored)
		require.Equal(t, 2, restored.ServerToolUse.WebSearchRequests)
		wire, err := common.Marshal(dto.ClaudeResponse{Type: "message", Usage: restored})
		require.NoError(t, err)
		var returned map[string]any
		require.NoError(t, common.Unmarshal(wire, &returned))
		returnedUsage := returned["usage"].(map[string]any)
		require.Equal(t, float64(30), returnedUsage["cache_creation_input_tokens"])
		cacheCreation := returnedUsage["cache_creation"].(map[string]any)
		require.Equal(t, float64(10), cacheCreation["ephemeral_5m_input_tokens"])
		require.Equal(t, float64(20), cacheCreation["ephemeral_1h_input_tokens"])
		serverToolUse := returnedUsage["server_tool_use"].(map[string]any)
		require.Equal(t, float64(2), serverToolUse["web_search_requests"])
	})

	t.Run("gemini_native_usage_round_trip_and_settlement", func(t *testing.T) {
		ctx := newLocalPricingContext("/v1beta/models/gemini-2.5-pro:generateContent")
		info := prepareLocalPriceData(t, ctx, "gemini-2.5-pro", 135)
		info.RelayFormat = types.RelayFormatGemini
		var response dto.GeminiChatResponse
		require.NoError(t, common.UnmarshalJsonStr(`{"candidates":[],"usageMetadata":{"promptTokenCount":135,"toolUsePromptTokenCount":3,"candidatesTokenCount":50,"totalTokenCount":188,"thoughtsTokenCount":5,"cachedContentTokenCount":20,"promptTokensDetails":[{"modality":"TEXT","tokenCount":100},{"modality":"IMAGE","tokenCount":10},{"modality":"AUDIO","tokenCount":5}],"toolUsePromptTokensDetails":[{"modality":"TEXT","tokenCount":3}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":43},{"modality":"AUDIO","tokenCount":7}]}}`, &response))
		metadata := response.UsageMetadata
		canonical := &dto.Usage{
			PromptTokens: 135, CompletionTokens: 50, TotalTokens: 188,
			UsageSemantic:          dto.BillingUsageSemanticGemini,
			UsageSource:            dto.BillingUsageSourceGeminiChat,
			BillingUsage:           dto.NewGeminiChatBillingUsage(&metadata),
			PromptTokensDetails:    dto.InputTokenDetails{CachedTokens: 20, TextTokens: 100, ImageTokens: 10, AudioTokens: 5},
			CompletionTokenDetails: dto.OutputTokenDetails{TextTokens: 43, AudioTokens: 7, ReasoningTokens: 5},
		}
		summary := calculateTextQuotaSummary(ctx, info, canonical)
		require.NoError(t, summary.PolicyError)
		require.NotNil(t, summary.PolicyCalculation)
		require.Greater(t, summary.Quota, 0)
		openAIResponse := &dto.OpenAITextResponse{Usage: *canonical}
		restored := ResponseOpenAI2Gemini(openAIResponse, info)
		require.Equal(t, metadata, restored.UsageMetadata)
		require.Equal(t, response.UsageMetadata.CandidatesTokensDetails, restored.UsageMetadata.CandidatesTokensDetails)
		wire, err := common.Marshal(restored)
		require.NoError(t, err)
		var returned map[string]any
		require.NoError(t, common.Unmarshal(wire, &returned))
		returnedMetadata := returned["usageMetadata"].(map[string]any)
		candidateDetails := returnedMetadata["candidatesTokensDetails"].([]any)
		require.Len(t, candidateDetails, 2)
		require.Equal(t, "AUDIO", candidateDetails[1].(map[string]any)["modality"])
		require.Equal(t, float64(7), candidateDetails[1].(map[string]any)["tokenCount"])
	})
}
