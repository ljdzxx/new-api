package helper

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetEffectiveGlobalModelRatioIncludesChannelRatio(t *testing.T) {
	origin := ratio_setting.GetGlobalModelRatio()
	t.Cleanup(func() {
		ratio_setting.SetGlobalModelRatio(origin)
	})
	ratio_setting.SetGlobalModelRatio(1.5)

	systemRatio, userRatio, channelRatio, effectiveRatio := getEffectiveGlobalModelRatio(0, 2, 0, 0)

	require.Equal(t, 1.5, systemRatio)
	require.Equal(t, 1.0, userRatio)
	require.Equal(t, 2.0, channelRatio)
	require.Equal(t, 3.0, effectiveRatio)
}

func TestUserGlobalModelRatioInputTokenThreshold(t *testing.T) {
	require.True(t, globalModelRatioApplies(0, 0))
	require.False(t, globalModelRatioApplies(999, 1000))
	require.True(t, globalModelRatioApplies(1000, 1000))
	require.True(t, globalModelRatioApplies(1001, 1000))
}

func TestSystemAndChannelGlobalModelRatioInputTokenThresholds(t *testing.T) {
	originalRatio := ratio_setting.GetGlobalModelRatio()
	originalThreshold := ratio_setting.GetGlobalModelRatioInputTokenThreshold()
	t.Cleanup(func() {
		ratio_setting.SetGlobalModelRatio(originalRatio)
		ratio_setting.SetGlobalModelRatioInputTokenThreshold(originalThreshold)
	})
	ratio_setting.SetGlobalModelRatio(1.5)
	ratio_setting.SetGlobalModelRatioInputTokenThreshold(1000)

	_, _, channelRatio, effectiveRatio := getEffectiveGlobalModelRatio(0, 2, 500, 499)
	require.Equal(t, 1.0, channelRatio)
	require.Equal(t, 1.0, effectiveRatio)

	systemRatio, _, channelRatio, effectiveRatio := getEffectiveGlobalModelRatio(0, 2, 500, 500)
	require.Equal(t, 1.0, systemRatio)
	require.Equal(t, 2.0, channelRatio)
	require.Equal(t, 2.0, effectiveRatio)

	systemRatio, _, channelRatio, effectiveRatio = getEffectiveGlobalModelRatio(0, 2, 500, 1000)
	require.Equal(t, 1.5, systemRatio)
	require.Equal(t, 2.0, channelRatio)
	require.Equal(t, 3.0, effectiveRatio)
}

func TestReevaluateGlobalModelRatioForActualInput(t *testing.T) {
	originalRatio := ratio_setting.GetGlobalModelRatio()
	originalThreshold := ratio_setting.GetGlobalModelRatioInputTokenThreshold()
	t.Cleanup(func() {
		ratio_setting.SetGlobalModelRatio(originalRatio)
		ratio_setting.SetGlobalModelRatioInputTokenThreshold(originalThreshold)
	})
	ratio_setting.SetGlobalModelRatio(1.25)
	ratio_setting.SetGlobalModelRatioInputTokenThreshold(20_000)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelModelRatio: 1.1, RatioThreshold: 10_000},
		PriceData: types.PriceData{
			SystemGlobalModelRatio: 1,
			UserGlobalModelRatio:   1,
			ChannelModelRatio:      1,
			GlobalModelRatio:       1,
		},
	}

	ratio := ReevaluateGlobalModelRatioForActualInput(info, 15_000)
	require.Equal(t, 1.0, info.PriceData.SystemGlobalModelRatio)
	require.Equal(t, 1.1, info.PriceData.ChannelModelRatio)
	require.Equal(t, 1.1, ratio)

	ratio = ReevaluateGlobalModelRatioForActualInput(info, 25_000)
	require.Equal(t, 1.25, info.PriceData.SystemGlobalModelRatio)
	require.Equal(t, 1.1, info.PriceData.ChannelModelRatio)
	require.Equal(t, 1.375, ratio)

	ratio = ReevaluateGlobalModelRatioForActualInput(info, 5_000)
	require.Equal(t, 1.0, info.PriceData.SystemGlobalModelRatio)
	require.Equal(t, 1.0, info.PriceData.ChannelModelRatio)
	require.Equal(t, 1.0, ratio)
}

func TestBindChannelModelRatio(t *testing.T) {
	info := &relaycommon.RelayInfo{}

	BindChannelModelRatioConfig(info, 1.3, 1000)
	require.NotNil(t, info.ChannelMeta)
	require.Equal(t, 1.3, info.ChannelMeta.ChannelModelRatio)
	require.Equal(t, int64(1000), info.ChannelMeta.RatioThreshold)

	BindChannelModelRatio(info, math.NaN())
	require.Equal(t, ratio_setting.DefaultGlobalModelRatio, info.ChannelMeta.ChannelModelRatio)
	require.Zero(t, info.ChannelMeta.RatioThreshold)
}

func TestPerCallPriceIgnoresZeroGlobalTokenRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalPolicy := billing_policy.GetConfig()
	originalPrices := ratio_setting.ModelPrice2JSONString()
	originalGroups := ratio_setting.GroupRatio2JSONString()
	originalGlobal := ratio_setting.GetGlobalModelRatio()
	originalFreePreConsume := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(originalPolicy)))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroups))
		ratio_setting.SetGlobalModelRatio(originalGlobal)
		operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = originalFreePreConsume
	})
	require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(billing_policy.NewConfig())))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"per-call-global-zero":0.02}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	ratio_setting.SetGlobalModelRatio(0)
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("group", "default")
	info := &relaycommon.RelayInfo{OriginModelName: "per-call-global-zero", UserGroup: "default", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelModelRatio: 1}}
	priceData, err := ModelPriceHelperPerCall(ctx, info)
	require.NoError(t, err)
	require.False(t, priceData.FreeModel)
	require.Equal(t, 10000, priceData.Quota)
}

func TestBillingPolicyPreConsumeEstimatesOutputByPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalPolicy := billing_policy.GetConfig()
	originalGlobalRatio := ratio_setting.GetGlobalModelRatio()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(originalPolicy)))
		ratio_setting.SetGlobalModelRatio(originalGlobalRatio)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	ratio_setting.SetGlobalModelRatio(1)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"free":0}`))
	require.NoError(t, billing_policy.UpdateFromJSON(`{
		"schema_version":1,"revision":1,"state":"active","migration":{},"policies":{
			"policy-preconsume-test":{"version":1,"mode":"per_token","currency":"USD","unit":"per_million_tokens","prices":{"input":"2","output":"10"}}
		}}`))

	tests := []struct {
		name      string
		group     string
		maxTokens int
		expected  int
	}{
		{name: "omitted max tokens uses fallback", group: "default", expected: 41460},
		{name: "explicit max tokens is priced as output", group: "default", maxTokens: 100, expected: 1000},
		{name: "free group does not add fallback", group: "free", expected: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			ctx.Set("group", tt.group)
			info := &relaycommon.RelayInfo{
				OriginModelName: "policy-preconsume-test",
				UserGroup:       tt.group,
				UsingGroup:      tt.group,
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelModelRatio: 1},
			}

			priceData, err := ModelPriceHelper(ctx, info, 500, &types.TokenCountMeta{MaxTokens: tt.maxTokens})
			require.NoError(t, err)
			require.Equal(t, tt.expected, priceData.QuotaToPreConsume)
		})
	}
}

func TestTieredBillingPolicyPreConsumeUsesEstimatedOutputForTier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalPolicy := billing_policy.GetConfig()
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(originalPolicy)))
	})
	require.NoError(t, billing_policy.UpdateFromJSON(`{
		"schema_version":1,"revision":1,"state":"active","migration":{},"policies":{
			"tiered-preconsume-test":{"version":1,"mode":"tiered","currency":"USD","unit":"per_million_tokens","tiers":[
				{"id":"has-output","priority":1,"conditions":[{"metric":"output_total_tokens","operator":"gt","value":0}],"prices":{"input":"2","output":"10"}},
				{"id":"fallback","priority":2,"fallback":true,"prices":{"input":"2","output":"2"}}
			]}
		}}`))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("group", "default")
	info := &relaycommon.RelayInfo{
		OriginModelName: "tiered-preconsume-test", UserGroup: "default", UsingGroup: "default",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelModelRatio: 1},
	}

	priceData, err := ModelPriceHelper(ctx, info, 500, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 41460, priceData.QuotaToPreConsume)
	require.Equal(t, 5.0, priceData.CompletionRatio)
}

func TestBillingPolicyPreConsumeRejectsOverflow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalPolicy := billing_policy.GetConfig()
	originalGlobalRatio := ratio_setting.GetGlobalModelRatio()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(originalPolicy)))
		ratio_setting.SetGlobalModelRatio(originalGlobalRatio)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	ratio_setting.SetGlobalModelRatio(1)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, billing_policy.UpdateFromJSON(`{
		"schema_version":1,"revision":1,"state":"active","migration":{},"policies":{
			"policy-overflow-test":{"version":1,"mode":"per_token","currency":"USD","unit":"per_million_tokens","prices":{"input":"1000000000000000","output":"1"}}
		}}`))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Set("group", "default")
	info := &relaycommon.RelayInfo{
		OriginModelName: "policy-overflow-test", UserGroup: "default", UsingGroup: "default",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelModelRatio: 1},
	}

	_, err := ModelPriceHelper(ctx, info, 500, &types.TokenCountMeta{MaxTokens: 1})

	var clamp *common.QuotaClamp
	require.ErrorAs(t, err, &clamp)
	require.Equal(t, "QuotaFromDecimal", clamp.Op)
	require.Equal(t, common.QuotaClampOverflow, clamp.Kind)
	require.Nil(t, info.Billing)
}

func TestModelPriceHelperAppliesImageCountOnceForFixedPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalPolicy := billing_policy.GetConfig()
	originalPrices := ratio_setting.ModelPrice2JSONString()
	originalRatios := ratio_setting.ModelRatio2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	originalGlobalRatio := ratio_setting.GetGlobalModelRatio()
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(originalPolicy)))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalPrices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		ratio_setting.SetGlobalModelRatio(originalGlobalRatio)
	})

	legacyConfig := billing_policy.NewConfig()
	require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(legacyConfig)))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	ratio_setting.SetGlobalModelRatio(1)
	prices, err := common.Marshal(map[string]float64{
		"fixed-image-price":      0.04,
		"fractional-image-price": 0.0000012,
		"overflow-image-price":   float64(common.MaxQuota) / common.QuotaPerUnit / 2,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(prices)))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"ratio-image-price":15}`))

	newInfo := func(model string) (*gin.Context, *relaycommon.RelayInfo) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		ctx.Set("group", "default")
		return ctx, &relaycommon.RelayInfo{
			OriginModelName: model,
			UserGroup:       "default",
			UsingGroup:      "default",
			ChannelMeta:     &relaycommon.ChannelMeta{ChannelModelRatio: 1},
		}
	}
	meta := &types.TokenCountMeta{BillingRatios: map[string]float64{"n": 3}}

	ctx, info := newInfo("fixed-image-price")
	priceData, err := ModelPriceHelper(ctx, info, 0, meta)
	require.NoError(t, err)
	require.Equal(t, 60000, priceData.QuotaToPreConsume)
	require.True(t, priceData.HasOtherRatio("n"))
	require.Equal(t, priceData.OtherRatios(), info.PriceData.OtherRatios())

	ctx, info = newInfo("ratio-image-price")
	priceData, err = ModelPriceHelper(ctx, info, 0, meta)
	require.NoError(t, err)
	require.Equal(t, common.PreConsumedQuota*15, priceData.QuotaToPreConsume)
	require.False(t, priceData.HasOtherRatio("n"))

	ctx, info = newInfo("fractional-image-price")
	priceData, err = ModelPriceHelper(ctx, info, 0, meta)
	require.NoError(t, err)
	// 0.0000012 * 500000 * 3 = 1.8; convert to quota only once.
	require.Equal(t, 1, priceData.QuotaToPreConsume)

	ctx, info = newInfo("overflow-image-price")
	_, err = ModelPriceHelper(ctx, info, 0, meta)
	var clamp *common.QuotaClamp
	require.ErrorAs(t, err, &clamp)
	require.Equal(t, common.QuotaClampOverflow, clamp.Kind)
	require.Nil(t, info.Billing)
}

func TestBillingPolicyFixedPricePreConsumeAppliesImageCountAndAdjustment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalPolicy := billing_policy.GetConfig()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(originalPolicy)))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, billing_policy.UpdateFromJSON(`{
		"schema_version":1,"revision":1,"state":"active","migration":{},"policies":{
			"policy-image-price":{"version":1,"mode":"per_request","currency":"USD","unit":"per_request","price":"0.04",
				"adjustments":[{"id":"double","multiplier":"2","conditions":[{"source":"header","path":"x-price-adjustment","operator":"eq","value":"double"}]}]}
		}}`))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	ctx.Request.Header.Set("x-price-adjustment", "double")
	ctx.Set("group", "default")
	info := &relaycommon.RelayInfo{
		OriginModelName: "policy-image-price", UserGroup: "default", UsingGroup: "default",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelModelRatio: 1},
	}

	priceData, err := ModelPriceHelper(ctx, info, 0, &types.TokenCountMeta{BillingRatios: map[string]float64{"n": 3}})
	require.NoError(t, err)
	require.Equal(t, 120000, priceData.QuotaToPreConsume)
	require.Equal(t, 2.0, priceData.EffectivePolicyAdjustmentMultiplier())
	require.True(t, priceData.HasOtherRatio("n"))
}

func TestBillingPolicyShadowFixedPriceAppliesImageCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalPolicy := billing_policy.GetConfig()
	originalPrices := ratio_setting.ModelPrice2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(originalPolicy)))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
		billing_policy.ResetShadowStats()
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"shadow-image-price":0.04}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, billing_policy.UpdateFromJSON(`{
		"schema_version":1,"revision":1,"state":"shadow","migration":{},"policies":{
			"shadow-image-price":{"version":1,"mode":"per_request","currency":"USD","unit":"per_request","price":"0.04"}
		}}`))
	billing_policy.ResetShadowStats()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	ctx.Set("group", "default")
	info := &relaycommon.RelayInfo{
		OriginModelName: "shadow-image-price", UserGroup: "default", UsingGroup: "default",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelModelRatio: 1},
	}

	priceData, err := ModelPriceHelper(ctx, info, 0, &types.TokenCountMeta{BillingRatios: map[string]float64{"n": 3}})
	require.NoError(t, err)
	require.Equal(t, 60000, priceData.QuotaToPreConsume)
	stats := billing_policy.GetShadowStats()
	require.EqualValues(t, 1, stats.Observations)
	require.EqualValues(t, 1, stats.Matches)
	require.Zero(t, stats.Mismatches)
}
