package helper

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPreConsumeLogMatchesPricing(t *testing.T) {
	originalPolicy := billing_policy.GetConfig()
	originalGlobal := ratio_setting.GetGlobalModelRatio()
	originalThreshold := ratio_setting.GetGlobalModelRatioInputTokenThreshold()
	originalGroups := ratio_setting.GroupRatio2JSONString()
	originalModels := ratio_setting.ModelRatio2JSONString()
	originalQuotaSetting := *operation_setting.GetQuotaSetting()
	originalWriter := gin.DefaultWriter
	originalPreConsumedQuota, originalQuotaPerUnit := common.PreConsumedQuota, common.QuotaPerUnit
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(originalPolicy)))
		ratio_setting.SetGlobalModelRatio(originalGlobal)
		ratio_setting.SetGlobalModelRatioInputTokenThreshold(originalThreshold)
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroups))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModels))
		*operation_setting.GetQuotaSetting() = originalQuotaSetting
		gin.DefaultWriter = originalWriter
		common.PreConsumedQuota, common.QuotaPerUnit = originalPreConsumedQuota, originalQuotaPerUnit
	})
	common.PreConsumedQuota, common.QuotaPerUnit = 500, 500000
	ratio_setting.SetGlobalModelRatioInputTokenThreshold(0)
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"preconsume-log":1.1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"preconsume-log":2}`))
	operation_setting.GetQuotaSetting().EnablePreConsumeMinBalance = true
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false

	const tokenPolicy = `{"version":1,"mode":"per_token","currency":"USD","unit":"per_million_tokens","prices":{"input":"4","output":"20"}}`
	tests := []struct {
		name       string
		policy     string
		maxTokens  int
		global     float64
		service    float64
		expected   int
		calculated int
		fragments  []string
	}{
		{
			name: "reported 91212 includes default output", policy: tokenPolicy, global: 1, expected: 91212, calculated: 91212,
			fragments: []string{
				"pre_consume_calculation_path=billing_policy", "raw_input_tokens=500 estimated_output_tokens=8192 output_estimate_source=default",
				"scaled_input_tokens=500 scaled_output_tokens=8192",
				`"tokens":500,"price_per_million":"4","cost_usd":"0.002"`,
				`"tokens":8192,"price_per_million":"20","cost_usd":"0.16384"`,
				`quota_formula="round_half_away_from_zero(0.16584 USD * quota_per_unit 500000 * group_ratio 1.1)"`,
			},
		},
		{
			name: "explicit output and decimal rounding", maxTokens: 1, global: 1, expected: 1101, calculated: 1101,
			policy:    `{"version":1,"mode":"per_token","currency":"USD","unit":"per_million_tokens","prices":{"input":"4","output":"1"}}`,
			fragments: []string{"estimated_output_tokens=1 output_estimate_source=max_tokens", "scaled_input_tokens=500 scaled_output_tokens=1", "total_usd=0.002001", `quota_formula="round_half_away_from_zero(0.002001 USD * quota_per_unit 500000 * group_ratio 1.1)"`},
		},
		{
			name: "fractional scaled tokens are truncated", policy: tokenPolicy, maxTokens: 1, global: 0.5, expected: 550, calculated: 550,
			fragments: []string{"estimated_output_tokens=1 output_estimate_source=max_tokens", "scaled_input_tokens=250 scaled_output_tokens=0", "total_usd=0.001"},
		},
		{
			name: "global scaling applies to input and output", policy: tokenPolicy, global: 2, expected: 182424, calculated: 182424,
			fragments: []string{"scaled_input_tokens=1000 scaled_output_tokens=16384", "total_usd=0.33168"},
		},
		{
			name: "tier service and adjustment use actual calculation", global: 1, service: 3, expected: 547272, calculated: 547272,
			policy: `{"version":1,"mode":"tiered","currency":"USD","unit":"per_million_tokens","tiers":[
				{"id":"output-tier","priority":1,"conditions":[{"metric":"output_total_tokens","operator":"gt","value":0}],"prices":{"input":"4","output":"20"}},
				{"id":"fallback","priority":2,"fallback":true,"prices":{"input":"2","output":"2"}}],
				"adjustments":[{"id":"double","multiplier":"2","conditions":[{"source":"header","path":"x-price-adjustment","operator":"eq","value":"double"}]}]}`,
			fragments: []string{`billing_mode=tiered matched_tier="output-tier"`, "service_tier_multiplier=3 subtotal_usd=0.49752 policy_adjustment_multiplier=2 total_usd=0.99504"},
		},
		{
			name: "free override is distinct from calculated quota", global: 1, expected: 0, calculated: 90112,
			policy:    `{"version":1,"mode":"per_token","currency":"USD","unit":"per_million_tokens","prices":{"input":"0","output":"20"}}`,
			fragments: []string{"total_usd=0.16384", "free_model=true quota_to_pre_consume=0"},
		},
		{
			name: "legacy formula includes service multiplier", global: 1, service: 3, maxTokens: 100, expected: 3960, calculated: 3960,
			fragments: []string{"pre_consume_calculation_path=formula", "raw_preconsume_tokens=600 scaled_preconsume_tokens=600", `quota_formula="truncate(600 tokens * (model_ratio 2 * group_ratio 1.1 * service_tier_multiplier 3))"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := billing_policy.NewConfig()
			if tt.policy != "" {
				require.NoError(t, billing_policy.UpdateFromJSON(fmt.Sprintf(`{"schema_version":1,"revision":1,"state":"active","policies":{"preconsume-log":%s}}`, tt.policy)))
			} else {
				require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(config)))
			}
			ratio_setting.SetGlobalModelRatio(tt.global)
			var output bytes.Buffer
			gin.DefaultWriter = &output
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
			ctx.Request.Header.Set("x-price-adjustment", "double")
			ctx.Set("group", "preconsume-log")
			info := &relaycommon.RelayInfo{
				OriginModelName: "preconsume-log", UserGroup: "preconsume-log", UsingGroup: "preconsume-log",
				ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 313, ChannelType: constant.ChannelTypeOpenAI, ChannelModelRatio: 1},
			}
			if tt.service != 0 {
				info.ChannelOtherSettings = dto.ChannelOtherSettings{ServiceTierBilling: &types.ServiceTierBilling{
					Enabled: true, Rules: []types.ServiceTierBillingRule{{Model: "*", Tier: "fast", TagColor: "blue", TagType: "light", Multiplier: tt.service}},
				}}
			}
			price, err := ModelPriceHelper(ctx, info, 500, &types.TokenCountMeta{MaxTokens: tt.maxTokens})
			require.NoError(t, err)
			require.Equal(t, tt.expected, price.QuotaToPreConsume)
			log := output.String()
			require.Contains(t, log, "channel_id=313")
			require.Contains(t, log, "pre_consume_policy=minimum_balance pre_consume_policy_enabled=true")
			require.Contains(t, log, fmt.Sprintf("calculated_quota=%d ", tt.calculated))
			require.Contains(t, log, fmt.Sprintf("quota_to_pre_consume=%d", price.QuotaToPreConsume))
			require.NotContains(t, log, "scaled_formula=")
			for _, fragment := range tt.fragments {
				require.Contains(t, log, fragment)
			}
		})
	}
}
