package service

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTextQuotaLogIncludesServiceTierInFormula(t *testing.T) {
	oldWriter := gin.DefaultWriter
	oldDebug, oldToken := common.DebugTraceEnabled, common.DebugTraceToken
	t.Cleanup(func() {
		gin.DefaultWriter = oldWriter
		common.DebugTraceEnabled, common.DebugTraceToken = oldDebug, oldToken
	})
	common.DebugTraceEnabled, common.DebugTraceToken = true, ""
	for _, active := range []bool{false, true} {
		for _, tc := range []struct {
			name                       string
			input, output, read, write int
			perRequest                 bool
			multiplier                 float64
			wantQuota                  int
			wantFormula                string
		}{
			// Reproduce the real requests audited from logs 11920, 11921 and 11916.
			{"cache write", 15131, 187, 0, 15128, false, 6, 595440, "model_quota_before_tier 99240 * service_tier_multiplier 6 + tool_quota 0"},
			{"cache read and write", 15347, 132, 15128, 216, false, 6, 73374, "model_quota_before_tier 12229 * service_tier_multiplier 6 + tool_quota 0"},
			{"half quota rounding", 10872, 13, 0, 10869, false, 6, 409628, "= quota 409627.5 rounded_quota=409628"},
			{"per request", 15494, 95, 15337, 154, true, 2, 100000, "raw_formula=model_price 0.1 * quota_per_unit 500000 * group_ratio 1 * service_tier_multiplier 2 = model_quota 100000"},
			{"no tier", 15131, 187, 0, 15128, false, 0, 99240, "model_quota_before_tier 99240 * service_tier_multiplier 1 + tool_quota 0"},
		} {
			t.Run(fmt.Sprintf("active=%t/%s", active, tc.name), func(t *testing.T) {
				backup := billing_policy.GetConfig()
				t.Cleanup(func() { require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(backup))) })
				require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(billing_policy.NewConfig())))
				policy := billing_policy.Policy{Version: billing_policy.SchemaVersion, Mode: "per_token", Currency: "USD", Unit: "per_million_tokens", Prices: billing_policy.Prices{Input: "10", Output: "50", CacheRead: "1", CacheWrite: "12.5"}}
				if tc.perRequest {
					policy.Mode, policy.Unit, policy.Price, policy.Prices = "per_request", "per_request", "0.1", billing_policy.Prices{}
				}
				if active {
					installActiveBillingPolicyForTest(t, "log-tier-test", policy)
				}
				var output bytes.Buffer
				gin.DefaultWriter = &output
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				info := &relaycommon.RelayInfo{OriginModelName: "log-tier-test", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 4}, StartTime: time.Now(), PriceData: types.PriceData{
					UsePrice: tc.perRequest, ModelPrice: 0.1, ModelRatio: 5, CompletionRatio: 5, CacheRatio: 0.1, CacheCreationRatio: 1.25,
					GlobalModelRatio: 1, GlobalRatioConfigSnapshot: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
				}}
				if tc.multiplier != 0 {
					info.PriceData.ServiceTier = &types.ServiceTierBillingRule{Tier: "Ultrafast", Multiplier: tc.multiplier}
				}
				usage := &dto.Usage{PromptTokens: tc.input, CompletionTokens: tc.output, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: tc.read, CacheWriteTokens: tc.write}}
				summary := calculateTextQuotaSummary(ctx, info, usage)
				require.Equal(t, tc.wantQuota, summary.Quota)
				log := output.String()
				require.Contains(t, log, tc.wantFormula)
				require.Contains(t, log, fmt.Sprintf("rounded_quota=%d", tc.wantQuota))
				if tc.perRequest {
					require.Contains(t, log, "billing_mode=per_request")
					require.NotContains(t, log, "raw_formula=(base")
				} else {
					require.Contains(t, log, fmt.Sprintf("* service_tier_multiplier %.6f = model_quota", info.PriceData.ServiceTierMultiplier()))
				}
			})
		}
	}
}

func TestTextQuotaLogKeepsToolChargesOutsideTierMultiplier(t *testing.T) {
	oldWriter := gin.DefaultWriter
	oldDebug, oldToken := common.DebugTraceEnabled, common.DebugTraceToken
	t.Cleanup(func() {
		gin.DefaultWriter = oldWriter
		common.DebugTraceEnabled, common.DebugTraceToken = oldDebug, oldToken
	})
	common.DebugTraceEnabled, common.DebugTraceToken = true, ""
	for _, active := range []bool{false, true} {
		t.Run(fmt.Sprint(active), func(t *testing.T) {
			backup := billing_policy.GetConfig()
			t.Cleanup(func() { require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(backup))) })
			require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(billing_policy.NewConfig())))
			if active {
				policy := billing_policy.Policy{Version: billing_policy.SchemaVersion, Mode: "per_request", Currency: "USD", Unit: "per_request", Price: "0.1",
					Tools:       map[string]billing_policy.ToolPrice{billing_policy.ToolFileSearch: {Unit: "per_thousand_calls", Price: "7"}},
					Adjustments: []billing_policy.Adjustment{{ID: "double", Multiplier: "2", Conditions: []billing_policy.AdjustmentCondition{{Source: "header", Path: "x-plan", Operator: "eq", Value: "double"}}}},
				}
				installActiveBillingPolicyForTest(t, "log-tool-test", policy)
			}
			var output bytes.Buffer
			gin.DefaultWriter = &output
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			ctx.Request.Header.Set("x-plan", "double")
			info := &relaycommon.RelayInfo{OriginModelName: "log-tool-test", ChannelMeta: &relaycommon.ChannelMeta{}, StartTime: time.Now(), PriceData: types.PriceData{
				UsePrice: true, ModelPrice: 0.1, GlobalModelRatio: 1, GlobalRatioConfigSnapshot: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
				ServiceTier: &types.ServiceTierBillingRule{Tier: "Fast", Multiplier: 2},
			}, ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{dto.BuildInToolFileSearch: {CallCount: 2}}}}
			info.PriceData.SetPolicyAdjustmentMultiplier(2)
			info.PriceData.AddOtherRatio("test", 3)
			summary := calculateTextQuotaSummary(ctx, info, &dto.Usage{PromptTokens: 100, CompletionTokens: 10})
			toolPrice := operation_setting.GetFileSearchPricePerThousand()
			if active {
				toolPrice = 7 // Policy prices must win over legacy tool defaults.
			}
			toolQuota := toolPrice * 1000 // two calls / 1000 * 500000 quota/USD
			wantQuota := common.QuotaFromFloat((50000*2 + toolQuota) * 2 * 3)
			require.Equal(t, wantQuota, summary.Quota)
			require.Contains(t, output.String(), fmt.Sprintf("final_formula=(model_quota_before_tier 50000 * service_tier_multiplier 2 + tool_quota %g) * policy_adjustment_multiplier 2 * other_ratio_multiplier 3 = quota %d rounded_quota=%d", toolQuota, wantQuota, wantQuota))
		})
	}
}
