package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
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

func TestCalculateTextQuotaSummaryRechecksChannelRatioWithActualInputTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	originalSystemRatio := ratio_setting.GetGlobalModelRatio()
	originalSystemThreshold := ratio_setting.GetGlobalModelRatioInputTokenThreshold()
	t.Cleanup(func() {
		ratio_setting.SetGlobalModelRatio(originalSystemRatio)
		ratio_setting.SetGlobalModelRatioInputTokenThreshold(originalSystemThreshold)
	})
	ratio_setting.SetGlobalModelRatio(1)
	ratio_setting.SetGlobalModelRatioInputTokenThreshold(0)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "actual-input-channel-threshold",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelModelRatio: 1.1,
			RatioThreshold:    10_000,
		},
		PriceData: types.PriceData{
			ModelRatio:             1,
			CompletionRatio:        1,
			CacheRatio:             1,
			SystemGlobalModelRatio: 1,
			UserGlobalModelRatio:   1,
			ChannelModelRatio:      1,
			GlobalModelRatio:       1,
			GroupRatioInfo:         types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens:     40_936,
		CompletionTokens: 355,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 145_152,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.Equal(t, 1.1, relayInfo.PriceData.ChannelModelRatio)
	require.Equal(t, 1.1, relayInfo.PriceData.GlobalModelRatio)
	require.Equal(t, 45_029, summary.PromptTokens)
	require.Equal(t, 159_667, summary.CacheTokens)
}

func TestReevaluateGlobalModelRatioFreezesSystemAndUserThresholdConfig(t *testing.T) {
	const userID = 91001
	require.NoError(t, model.DB.Create(&model.User{
		Id:                   userID,
		Username:             "global-ratio-threshold-user",
		Status:               common.UserStatusEnabled,
		GlobalModelRatio:     1.6,
		GlobalRatioThreshold: 20_000,
	}).Error)
	t.Cleanup(func() { model.DB.Delete(&model.User{}, userID) })

	originalSystemRatio := ratio_setting.GetGlobalModelRatio()
	originalSystemThreshold := ratio_setting.GetGlobalModelRatioInputTokenThreshold()
	t.Cleanup(func() {
		ratio_setting.SetGlobalModelRatio(originalSystemRatio)
		ratio_setting.SetGlobalModelRatioInputTokenThreshold(originalSystemThreshold)
	})
	ratio_setting.SetGlobalModelRatio(1.25)
	ratio_setting.SetGlobalModelRatioInputTokenThreshold(10_000)

	info := &relaycommon.RelayInfo{
		UserId: userID,
		PriceData: types.PriceData{
			SystemGlobalModelRatio: 1,
			UserGlobalModelRatio:   1,
			ChannelModelRatio:      1,
			GlobalModelRatio:       1,
		},
	}

	require.Equal(t, 1.25, relayhelper.ReevaluateGlobalModelRatioForActualInput(info, 15_000))
	require.True(t, info.PriceData.GlobalRatioConfigSnapshot)

	// A request must settle with the configuration captured on its first actual
	// usage, even if an administrator changes settings while it is in flight.
	ratio_setting.SetGlobalModelRatio(3)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).
		Updates(map[string]interface{}{
			"global_model_ratio":                       4,
			"global_model_ratio_input_token_threshold": 1,
		}).Error)

	require.Equal(t, 2.0, relayhelper.ReevaluateGlobalModelRatioForActualInput(info, 25_000))
	require.Equal(t, 1.25, info.PriceData.SystemGlobalModelRatio)
	require.Equal(t, 1.6, info.PriceData.UserGlobalModelRatio)
}

func TestCalculateTextQuotaSummaryCountsAnthropicCacheInActualInputThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "anthropic-cache-channel-threshold",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelModelRatio: 1.2,
			RatioThreshold:    10_000,
		},
		PriceData: types.PriceData{
			ModelRatio:             1,
			CompletionRatio:        1,
			CacheRatio:             1,
			SystemGlobalModelRatio: 1,
			UserGlobalModelRatio:   1,
			ChannelModelRatio:      1,
			GlobalModelRatio:       1,
			GroupRatioInfo:         types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens:  100,
		UsageSemantic: "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 9_900,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.Equal(t, int64(10_000), summary.PolicyInputTotalTokens)
	require.Equal(t, 1.2, relayInfo.PriceData.ChannelModelRatio)
	require.Equal(t, 120, summary.PromptTokens)
	require.Equal(t, 11_880, summary.CacheTokens)
}

func TestRequestBillingSnapshotSurvivesPolicyAndTimeRuleChanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	ctx.Request.Header.Set("x-price", "discount")
	policy := fullTextBillingPolicy()
	policy.Prices.Input = "1"
	policy.Adjustments = []billing_policy.Adjustment{{
		ID: "discount", Multiplier: "0.5",
		Conditions: []billing_policy.AdjustmentCondition{{Source: "header", Path: "x-price", Operator: "eq", Value: "discount"}},
	}}
	installActiveBillingPolicyForTest(t, "snapshot-model", policy)
	relayInfo := &relaycommon.RelayInfo{OriginModelName: "snapshot-model", PriceData: types.PriceData{GlobalModelRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	require.True(t, ensureBillingPolicySnapshot(ctx, relayInfo))

	changed := fullTextBillingPolicy()
	changed.Prices.Input = "100"
	config := billing_policy.GetConfig()
	config.Revision++
	config.Policies["snapshot-model"] = changed
	require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(config)))
	ctx.Request.Header.Set("x-price", "changed")

	summary := calculateTextQuotaSummary(ctx, relayInfo, &dto.Usage{PromptTokens: 1_000, TotalTokens: 1_000})
	require.Equal(t, 250, summary.Quota)
	require.NotNil(t, relayInfo.BillingPolicySnapshot)
	require.Equal(t, int64(99), relayInfo.BillingPolicySnapshot.Revision)
	require.Equal(t, "0.5", relayInfo.BillingPolicySnapshot.AdjustmentMultiplier)
}

func TestActivePolicyFailureSettlesAtPreConsumeWithoutLegacyFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	installActiveBillingPolicyForTest(t, "broken-snapshot", fullTextBillingPolicy())
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "broken-snapshot", FinalPreConsumedQuota: 777,
		PriceData: types.PriceData{ModelRatio: 99, GlobalModelRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	require.True(t, ensureBillingPolicySnapshot(ctx, relayInfo))
	relayInfo.BillingPolicySnapshot.Policy.Prices.Input = "invalid"
	summary := calculateTextQuotaSummary(ctx, relayInfo, &dto.Usage{PromptTokens: 1_000, TotalTokens: 1_000})
	require.Error(t, summary.PolicyError)
	require.Equal(t, 777, summary.Quota)
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

func TestAttachBillingPolicyTokenScalingAdminInfo(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{PriceData: types.PriceData{
		SystemGlobalModelRatio: 1.25,
		ChannelModelRatio:      1.6,
		UserGlobalModelRatio:   1,
		GlobalModelRatio:       2,
	}}
	summary := textQuotaSummary{
		RawPromptTokens:        112573,
		RawCompletionTokens:    287,
		RawCacheTokens:         111141,
		RawCacheCreationTokens: 10,
		RawImageTokens:         5,
		RawAudioTokens:         2,
		RawAudioOutputTokens:   3,
		AudioInputPrice:        9,
		IsClaudeUsageSemantic:  false,
	}
	other := map[string]interface{}{}

	attachBillingPolicyTokenScalingAdminInfo(relayInfo, summary, billing_policy.Prices{
		AudioInput: "9", AudioOutput: "10",
	}, other)

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	tokenScaling, ok := adminInfo["billing_policy_token_scaling"].(billingPolicyTokenScaling)
	require.True(t, ok)
	require.Equal(t, 1.25, tokenScaling.SystemGlobalModelRatio)
	require.Equal(t, 1.6, tokenScaling.ChannelModelRatio)
	require.Equal(t, float64(1), tokenScaling.UserGlobalModelRatio)
	require.Equal(t, int64(1415), tokenScaling.RawLineItemTokens["input"])
	require.Equal(t, int64(284), tokenScaling.RawLineItemTokens["output"])
	require.Equal(t, int64(111141), tokenScaling.RawLineItemTokens["cache_read"])
	require.Equal(t, int64(10), tokenScaling.RawLineItemTokens["cache_write"])
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

func TestCalculateTextQuotaSummaryMissingResponsesStreamUsage(t *testing.T) {
	newContext := func(billable bool) *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		if billable {
			common.SetContextKey(ctx, constant.ContextKeyResponsesBillableStreamOutput, true)
		}
		return ctx
	}
	newRelayInfo := func(format types.RelayFormat, reason relaycommon.StreamEndReason, withError bool, preConsumed int) *relaycommon.RelayInfo {
		status := relaycommon.NewStreamStatus()
		if withError {
			status.RecordError("stream write failed")
		}
		status.SetEndReason(reason, nil)
		return &relaycommon.RelayInfo{
			IsStream:                true,
			RelayFormat:             format,
			FinalRequestRelayFormat: format,
			OriginModelName:         "gpt-responses-test",
			PriceData: types.PriceData{
				ModelRatio:             1,
				CompletionRatio:        1,
				SystemGlobalModelRatio: 1,
				UserGlobalModelRatio:   1,
				ChannelModelRatio:      1,
				GlobalModelRatio:       1,
				GroupRatioInfo:         types.GroupRatioInfo{GroupRatio: 1},
			},
			StartTime:             time.Now(),
			StreamStatus:          status,
			FinalPreConsumedQuota: preConsumed,
		}
	}

	tests := []struct {
		name        string
		billable    bool
		format      types.RelayFormat
		reason      relaycommon.StreamEndReason
		withError   bool
		preConsumed int
		usage       dto.Usage
		wantQuota   int
		wantKeep    bool
	}{
		{name: "client disconnected after output", billable: true, format: types.RelayFormatOpenAIResponses, reason: relaycommon.StreamEndReasonClientGone, preConsumed: 5000, wantQuota: 5000, wantKeep: true},
		{name: "upstream eof after output", billable: true, format: types.RelayFormatOpenAIResponses, reason: relaycommon.StreamEndReasonEOF, preConsumed: 5000, wantQuota: 5000, wantKeep: true},
		{name: "handler write error after output", billable: true, format: types.RelayFormatOpenAIResponses, reason: relaycommon.StreamEndReasonHandlerStop, withError: true, preConsumed: 5000, wantQuota: 5000, wantKeep: true},
		{name: "handler stop without error refunds", billable: true, format: types.RelayFormatOpenAIResponses, reason: relaycommon.StreamEndReasonHandlerStop, preConsumed: 5000},
		{name: "normal done refunds missing usage", billable: true, format: types.RelayFormatOpenAIResponses, reason: relaycommon.StreamEndReasonDone, preConsumed: 5000},
		{name: "no billable output refunds", format: types.RelayFormatOpenAIResponses, reason: relaycommon.StreamEndReasonClientGone, preConsumed: 5000},
		{name: "zero pre-consume refunds", billable: true, format: types.RelayFormatOpenAIResponses, reason: relaycommon.StreamEndReasonClientGone},
		{name: "non responses stream refunds", billable: true, format: types.RelayFormatOpenAI, reason: relaycommon.StreamEndReasonClientGone, preConsumed: 5000},
		{name: "actual usage wins", billable: true, format: types.RelayFormatOpenAIResponses, reason: relaycommon.StreamEndReasonClientGone, preConsumed: 5000, usage: dto.Usage{PromptTokens: 100, CompletionTokens: 20}, wantQuota: 120},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := calculateTextQuotaSummary(newContext(tt.billable), newRelayInfo(tt.format, tt.reason, tt.withError, tt.preConsumed), &tt.usage)
			require.Equal(t, tt.wantQuota, summary.Quota)
			require.Equal(t, tt.wantKeep, summary.MissingUsagePreConsumed)
		})
	}
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

func TestCalculateTextQuotaSummaryBillsOpenAICacheWriteTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	relayInfo := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		OriginModelName: "gpt-cache-write",
		PriceData: types.PriceData{
			ModelRatio: 1, CompletionRatio: 2, CacheRatio: 0.1,
			CacheCreationRatio: 1.25, GlobalModelRatio: 1,
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}

	t.Run("positive uncached remainder", func(t *testing.T) {
		usage := &dto.Usage{
			PromptTokens: 1473, CompletionTokens: 19,
			PromptTokensDetails: dto.InputTokenDetails{CacheWriteTokens: 1470},
		}
		summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
		require.Equal(t, 1470, summary.CacheCreationTokens)
		// (1473-1470) + 1470*1.25 + 19*2 = 1878.5, rounded half away from zero.
		require.Equal(t, 1879, summary.Quota)
	})

	t.Run("overlapping prefixes clamp uncached remainder", func(t *testing.T) {
		usage := &dto.Usage{
			PromptTokens: 3619, CompletionTokens: 36,
			PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 2921, CacheWriteTokens: 3616},
		}
		summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
		// max(3619-2921-3616, 0) + 2921*0.1 + 3616*1.25 + 36*2 = 4884.1.
		require.Equal(t, 4884, summary.Quota)
	})

	t.Run("negative cache write cannot reduce charge", func(t *testing.T) {
		usage := &dto.Usage{
			PromptTokens: 100, CompletionTokens: 10,
			PromptTokensDetails: dto.InputTokenDetails{CacheWriteTokens: -1000},
		}
		summary := calculateTextQuotaSummary(ctx, relayInfo, usage)
		require.Zero(t, summary.CacheCreationTokens)
		require.Equal(t, 120, summary.Quota)
	})
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
