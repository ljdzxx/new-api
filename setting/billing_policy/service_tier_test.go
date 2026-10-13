package billing_policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceTierMultiplierExcludesTools(t *testing.T) {
	for _, mode := range []string{"per_token", "per_request", "tiered"} {
		t.Run(mode, func(t *testing.T) {
			policy := testTokenPolicy("1")
			policy.Prices = Prices{Input: "1", Output: "2", CacheRead: "3", CacheWrite: "4", CacheWrite5m: "5", CacheWrite1h: "6", ImageInput: "7", AudioInput: "8", AudioOutput: "9"}
			if mode == "per_request" {
				policy.Mode, policy.Unit, policy.Price = mode, "per_request", "45"
				policy.Prices = Prices{}
			} else if mode == "tiered" {
				policy.Mode = mode
				policy.Tiers = []Tier{{ID: "fallback", Fallback: true, Prices: policy.Prices}}
				policy.Prices = Prices{}
			}
			policy.Tools = map[string]ToolPrice{ToolFileSearch: {Unit: "per_thousand_calls", Price: "10"}}
			usage := BillingUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000, CacheWriteTokens: 1_000_000, CacheWrite5mTokens: 1_000_000, CacheWrite1hTokens: 1_000_000, ImageInputTokens: 1_000_000, AudioInputTokens: 1_000_000, AudioOutputTokens: 1_000_000, ToolUsage: map[string]int64{ToolFileSearch: 1000}}
			calculation, err := CalculateBilling(policy, usage, RequestContext{ModelPriceMultiplier: 6, FreezeAdjustments: true, AdjustmentMultiplier: "2"})
			require.NoError(t, err)
			require.Equal(t, "280", calculation.SubtotalUSD) // 45 * 6 + 10
			require.Equal(t, "560", calculation.TotalUSD)
			require.Equal(t, 6.0, calculation.ModelPriceMultiplier)
			require.Equal(t, usage, calculation.Usage, "pricing must not change usage or tier thresholds")
			for _, item := range calculation.LineItems {
				if item.Kind == BillingLineItemKindTool {
					require.Equal(t, "10", item.CostUSD)
				}
			}
		})
	}
}
