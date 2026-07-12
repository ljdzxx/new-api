package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func installActiveBillingPolicyForTest(t *testing.T, modelName string, policy billing_policy.Policy) {
	t.Helper()
	backup := billing_policy.GetConfig()
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(backup)))
	})
	config := billing_policy.NewConfig()
	config.State = billing_policy.StateActive
	config.Revision = 99
	config.Policies = map[string]billing_policy.Policy{modelName: policy}
	require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(config)))
}

func fullTextBillingPolicy() billing_policy.Policy {
	return billing_policy.Policy{
		Version:  billing_policy.SchemaVersion,
		Mode:     "per_token",
		Currency: "USD",
		Unit:     "per_million_tokens",
		Prices: billing_policy.Prices{
			Input: "1", Output: "2", CacheRead: "3",
			CacheWrite: "5", CacheWrite5m: "6", CacheWrite1h: "7",
			ImageInput: "8", AudioInput: "9", AudioOutput: "10",
		},
	}
}

func TestCalculateTextQuotaSummaryFixedPriceAppliesImageCountOnceAndAllowsOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	priceData := types.PriceData{
		ModelPrice:       0.12,
		UsePrice:         true,
		GlobalModelRatio: 1,
		GroupRatioInfo: types.GroupRatioInfo{
			GroupRatio: 1,
		},
	}
	priceData.AddOtherRatio("n", 3)
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "dall-e-3",
		PriceData:       priceData,
		StartTime:       time.Now(),
	}
	usage := &dto.Usage{PromptTokens: 1, TotalTokens: 1}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.Equal(t, 180000, summary.Quota)

	// The adaptor-reported actual count replaces the requested count instead
	// of multiplying a second count into the charge.
	relayInfo.PriceData.AddOtherRatio("n", 2)
	summary = calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.Equal(t, 120000, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesActivePolicyFieldsForOpenAIUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Request.Header.Set("x-price-adjustment", "double")

	policy := fullTextBillingPolicy()
	policy.Adjustments = []billing_policy.Adjustment{{
		ID: "double", Multiplier: "2",
		Conditions: []billing_policy.AdjustmentCondition{{Source: "header", Path: "x-price-adjustment", Operator: "eq", Value: "double"}},
	}}
	installActiveBillingPolicyForTest(t, "complete-openai", policy)

	priceData := types.PriceData{
		ModelRatio: 0.5, CompletionRatio: 2, CacheRatio: 3,
		CacheCreationRatio: 5, ImageRatio: 8, AudioRatio: 9,
		AudioCompletionRatio: 10.0 / 9.0, GlobalModelRatio: 1,
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	priceData.SetPolicyAdjustmentMultiplier(2)
	priceData.AddOtherRatio("batch", 3)
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "complete-openai", RelayFormat: types.RelayFormatOpenAI,
		PriceData: priceData, StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens: 137, CompletionTokens: 53,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 10, CacheWriteTokens: 20, ImageTokens: 5, AudioTokens: 2,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{AudioTokens: 3},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	// (100*1 + 50*2 + 10*3 + 20*5 + 5*8 + 2*9 + 3*10) / 1M
	// * 500000 quota/USD * adjustment 2 * batch 3 = 1254 quota.
	require.Equal(t, 1254, summary.Quota)
	require.NotNil(t, summary.PolicyCalculation)
	require.Equal(t, "0.000836", summary.PolicyCalculation.TotalUSD)
	require.Len(t, summary.PolicyCalculation.LineItems, 7)
	fields := make([]string, 0, len(summary.PolicyCalculation.LineItems))
	for _, item := range summary.PolicyCalculation.LineItems {
		fields = append(fields, item.Field)
	}
	require.Equal(t, []string{"input", "output", "cache_read", "cache_write", "image_input", "audio_input", "audio_output"}, fields)

	other := map[string]interface{}{}
	attachBillingPolicySnapshot(ctx, relayInfo, summary, other)
	snapshot, ok := other["billing_policy"].(billingPolicyLogSnapshot)
	require.True(t, ok)
	require.Equal(t, 1254, snapshot.ActualQuota)
	require.Equal(t, int64(99), snapshot.Revision)
	require.Equal(t, map[string]float64{"batch": 3}, snapshot.OtherRatios)
	require.Equal(t, float64(3), snapshot.OtherRatioMultiplier)
	require.Equal(t, float64(2), snapshot.PolicyAdjustmentMultiplier)
}

func TestBuildBillingPolicyAdditionalCharges(t *testing.T) {
	charges, total := buildBillingPolicyAdditionalCharges(textQuotaSummary{
		WebSearchCallCount:       2,
		WebSearchPrice:           10,
		FileSearchCallCount:      4,
		FileSearchPrice:          2.5,
		ImageGenerationCallPrice: 0.011,
	})

	require.Len(t, charges, 3)
	require.Equal(t, billingPolicyAdditionalCharge{
		Field: "web_search", Units: 2, Unit: "per_thousand_calls", UnitPrice: "10", CostUSD: "0.02",
	}, charges[0])
	require.Equal(t, billingPolicyAdditionalCharge{
		Field: "file_search", Units: 4, Unit: "per_thousand_calls", UnitPrice: "2.5", CostUSD: "0.01",
	}, charges[1])
	require.Equal(t, "0.041", total.String())
}

func TestCalculateTextQuotaSummaryUsesAnthropicCachePriceFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	installActiveBillingPolicyForTest(t, "complete-anthropic", fullTextBillingPolicy())

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "complete-anthropic", RelayFormat: types.RelayFormatClaude,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		PriceData: types.PriceData{
			ModelRatio: 0.5, CompletionRatio: 2, CacheRatio: 4,
			CacheCreationRatio: 5, CacheCreation5mRatio: 6, CacheCreation1hRatio: 7,
			GlobalModelRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens: 100, CompletionTokens: 50, UsageSemantic: "anthropic",
		PromptTokensDetails:         dto.InputTokenDetails{CachedTokens: 10, CacheWriteTokens: 20},
		ClaudeCacheCreation5mTokens: 7, ClaudeCacheCreation1hTokens: 8,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	// (100*1 + 50*2 + 10*3 + remaining 5*5 + 7*6 + 8*7) / 1M
	// * 500000 quota/USD = 176.5, rounded half away from zero to 177.
	require.Equal(t, 177, summary.Quota)
	require.NotNil(t, summary.PolicyCalculation)
	fields := make([]string, 0, len(summary.PolicyCalculation.LineItems))
	for _, item := range summary.PolicyCalculation.LineItems {
		fields = append(fields, item.Field)
	}
	require.Equal(t, []string{"input", "output", "cache_read", "cache_write", "cache_write_5m", "cache_write_1h"}, fields)
}

func TestCalculateTextQuotaSummaryUnifiedForClaudeSemantic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:     100,
			CacheWriteTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	priceData := types.PriceData{
		ModelRatio:           1,
		CompletionRatio:      2,
		CacheRatio:           0.1,
		CacheCreationRatio:   1.25,
		CacheCreation5mRatio: 1.25,
		CacheCreation1hRatio: 2,
		GlobalModelRatio:     1,
		GroupRatioInfo: types.GroupRatioInfo{
			GroupRatio: 1,
		},
	}

	chatRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}
	messageRelayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatClaude,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData:               priceData,
		StartTime:               time.Now(),
	}

	chatSummary := calculateTextQuotaSummary(ctx, chatRelayInfo, usage)
	messageSummary := calculateTextQuotaSummary(ctx, messageRelayInfo, usage)

	require.Equal(t, messageSummary.Quota, chatSummary.Quota)
	require.Equal(t, messageSummary.CacheCreationTokens5m, chatSummary.CacheCreationTokens5m)
	require.Equal(t, messageSummary.CacheCreationTokens1h, chatSummary.CacheCreationTokens1h)
	require.True(t, chatSummary.IsClaudeUsageSemantic)
	require.Equal(t, 1488, chatSummary.Quota)
}

func TestCalculateTextQuotaSummaryUsesSplitClaudeCacheCreationRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      1,
			CacheRatio:           0,
			CacheCreationRatio:   1,
			CacheCreation5mRatio: 2,
			CacheCreation1hRatio: 3,
			GlobalModelRatio:     1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 0,
		PromptTokensDetails: dto.InputTokenDetails{
			CacheWriteTokens: 10,
		},
		ClaudeCacheCreation5mTokens: 2,
		ClaudeCacheCreation1hTokens: 3,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 100 + remaining(5)*1 + 2*2 + 3*3 = 118
	require.Equal(t, 118, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesAnthropicUsageSemanticFromUpstreamUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      2,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GlobalModelRatio:     1,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1,
			},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 200,
		UsageSemantic:    "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:     100,
			CacheWriteTokens: 50,
		},
		ClaudeCacheCreation5mTokens: 10,
		ClaudeCacheCreation1hTokens: 20,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.True(t, summary.IsClaudeUsageSemantic)
	require.Equal(t, "anthropic", summary.UsageSemantic)
	require.Equal(t, 1488, summary.Quota)
}

func TestCacheWriteTokensTotal(t *testing.T) {
	t.Run("split cache creation", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens:   50,
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("legacy cache creation", func(t *testing.T) {
		summary := textQuotaSummary{CacheCreationTokens: 50}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("split cache creation without aggregate remainder", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 30, cacheWriteTokensTotal(summary))
	})
}

func TestCalculateTextQuotaSummaryBillsCacheOnlyUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	installActiveBillingPolicyForTest(t, "cache-only", fullTextBillingPolicy())
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "cache-only", RelayFormat: types.RelayFormatClaude,
		FinalRequestRelayFormat: types.RelayFormatClaude,
		PriceData: types.PriceData{
			ModelRatio: 0.5, CacheRatio: 4, GlobalModelRatio: 1,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		UsageSemantic:       "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 100},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
	require.True(t, hasBillableTokenUsage(summary))
	// 100 cache-read tokens * $3 / 1M * 500000 quota/USD = 150.
	require.Equal(t, 150, summary.Quota)
}

func TestCalculateTextQuotaSummaryHandlesLegacyClaudeDerivedOpenAIUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "claude-3-7-sonnet",
		PriceData: types.PriceData{
			ModelRatio:           1,
			CompletionRatio:      5,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			CacheCreation1hRatio: 2,
			GlobalModelRatio:     1,
			GroupRatioInfo:       types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     62,
		CompletionTokens: 95,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 3544,
		},
		ClaudeCacheCreation5mTokens: 586,
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	// 62 + 3544*0.1 + 586*1.25 + 95*5 = 1624.9 => 1624
	require.Equal(t, 1624, summary.Quota)
}

func TestCalculateTextQuotaSummaryUsesGlobalModelRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)

	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "gpt-5.4",
		PriceData: types.PriceData{
			ModelRatio:       1.25,
			CompletionRatio:  6,
			CacheRatio:       1,
			GlobalModelRatio: 2,
			GroupRatioInfo:   types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	usage := &dto.Usage{
		PromptTokens:     578,
		CompletionTokens: 56,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 28032,
		},
		UsageSemantic: "anthropic",
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.Equal(t, 72365, summary.Quota)
	require.Equal(t, 1156, summary.PromptTokens)
	require.Equal(t, 112, summary.CompletionTokens)
	require.Equal(t, 56064, summary.CacheTokens)
}
