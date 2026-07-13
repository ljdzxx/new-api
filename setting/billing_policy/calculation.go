package billing_policy

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// claudeCacheWrite1hFallbackMultiplier 是 1h 缓存写与 5m 缓存写的官方价格比
// （Anthropic: 1h 写 = 2x 输入价，5m 写 = 1.25x 输入价，2 / 1.25 = 1.6）。
// 当策略未配置 cache_write_1h 时，按 5m 写价 × 1.6 兜底，避免 1h 缓存写被漏计费。
var claudeCacheWrite1hFallbackMultiplier = decimal.NewFromFloat(1.6)

type BillingUsage struct {
	TierInputTotalTokens  int64            `json:"tier_input_total_tokens,omitempty"`
	TierOutputTotalTokens int64            `json:"tier_output_total_tokens,omitempty"`
	InputTokens           int64            `json:"input_tokens"`
	OutputTokens          int64            `json:"output_tokens"`
	CacheReadTokens       int64            `json:"cache_read_tokens"`
	CacheWriteTokens      int64            `json:"cache_write_tokens"`
	CacheWrite5mTokens    int64            `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens    int64            `json:"cache_write_1h_tokens"`
	ImageInputTokens      int64            `json:"image_input_tokens"`
	AudioInputTokens      int64            `json:"audio_input_tokens"`
	AudioOutputTokens     int64            `json:"audio_output_tokens"`
	ToolUsage             map[string]int64 `json:"tool_usage,omitempty"`
}

type BillingLineItem struct {
	Field           string `json:"field"`
	Tokens          int64  `json:"tokens,omitempty"`
	Units           int64  `json:"units,omitempty"`
	PricePerMillion string `json:"price_per_million,omitempty"`
	UnitPrice       string `json:"unit_price,omitempty"`
	CostUSD         string `json:"cost_usd"`
}

type BillingCalculation struct {
	Mode                 string              `json:"mode"`
	TierID               string              `json:"tier_id,omitempty"`
	Currency             string              `json:"currency"`
	Unit                 string              `json:"unit"`
	Prices               Prices              `json:"prices,omitempty"`
	Usage                BillingUsage        `json:"usage"`
	LineItems            []BillingLineItem   `json:"line_items"`
	SubtotalUSD          string              `json:"subtotal_usd"`
	AdjustmentMultiplier string              `json:"adjustment_multiplier"`
	AppliedAdjustments   []AppliedAdjustment `json:"applied_adjustments,omitempty"`
	TotalUSD             string              `json:"total_usd"`
}

// CalculateBilling evaluates every price field in a policy into an auditable
// USD breakdown. Group/global multipliers and quota conversion are intentionally
// applied by the settlement layer because they are not model-pricing fields.
func CalculateBilling(policy Policy, usage BillingUsage, requestCtx RequestContext) (BillingCalculation, error) {
	if err := ValidatePolicy(policy); err != nil {
		return BillingCalculation{}, err
	}
	for field, tokens := range map[string]int64{
		"tier_input_total_tokens":  usage.TierInputTotalTokens,
		"tier_output_total_tokens": usage.TierOutputTotalTokens,
		"input":                    usage.InputTokens,
		"output":                   usage.OutputTokens,
		"cache_read":               usage.CacheReadTokens,
		"cache_write":              usage.CacheWriteTokens,
		"cache_write_5m":           usage.CacheWrite5mTokens,
		"cache_write_1h":           usage.CacheWrite1hTokens,
		"image_input":              usage.ImageInputTokens,
		"audio_input":              usage.AudioInputTokens,
		"audio_output":             usage.AudioOutputTokens,
	} {
		if tokens < 0 {
			return BillingCalculation{}, fmt.Errorf("%s tokens cannot be negative", field)
		}
	}
	for name, units := range usage.ToolUsage {
		if units < 0 {
			return BillingCalculation{}, fmt.Errorf("tool %s units cannot be negative", name)
		}
	}
	result := BillingCalculation{
		Mode:                 policy.Mode,
		Currency:             policy.Currency,
		Unit:                 policy.Unit,
		Usage:                usage,
		LineItems:            make([]BillingLineItem, 0, 10),
		AdjustmentMultiplier: "1",
	}

	subtotal := decimal.Zero
	if policy.Mode == "per_request" {
		price, err := parseBillingPrice(policy.Price, "price")
		if err != nil {
			return BillingCalculation{}, err
		}
		subtotal = price
		result.LineItems = append(result.LineItems, BillingLineItem{
			Field: "request", Units: 1, UnitPrice: price.String(), CostUSD: price.String(),
		})
	} else {
		tierInputTotal := usage.TierInputTotalTokens
		if tierInputTotal == 0 {
			tierInputTotal = usage.InputTokens + usage.CacheReadTokens + usage.CacheWriteTokens + usage.CacheWrite5mTokens + usage.CacheWrite1hTokens + usage.ImageInputTokens + usage.AudioInputTokens
		}
		tierOutputTotal := usage.TierOutputTotalTokens
		if tierOutputTotal == 0 {
			tierOutputTotal = usage.OutputTokens + usage.AudioOutputTokens
		}
		prices, tierID, err := EffectivePricesForUsage(policy, Usage{
			InputTotalTokens: tierInputTotal, OutputTotalTokens: tierOutputTotal,
		})
		if err != nil {
			return BillingCalculation{}, err
		}
		result.Prices = prices
		result.TierID = tierID
		cacheWrite5mPrice := prices.CacheWrite5m
		if strings.TrimSpace(cacheWrite5mPrice) == "" {
			cacheWrite5mPrice = prices.CacheWrite
		}
		cacheWrite1hPrice := prices.CacheWrite1h
		if strings.TrimSpace(cacheWrite1hPrice) == "" && strings.TrimSpace(cacheWrite5mPrice) != "" {
			if basePrice, err := parseBillingPrice(cacheWrite5mPrice, "cache_write_5m"); err == nil {
				cacheWrite1hPrice = basePrice.Mul(claudeCacheWrite1hFallbackMultiplier).String()
			}
		}
		fields := []struct {
			name   string
			raw    string
			tokens int64
		}{
			{"input", prices.Input, usage.InputTokens},
			{"output", prices.Output, usage.OutputTokens},
			{"cache_read", prices.CacheRead, usage.CacheReadTokens},
			{"cache_write", prices.CacheWrite, usage.CacheWriteTokens},
			{"cache_write_5m", cacheWrite5mPrice, usage.CacheWrite5mTokens},
			{"cache_write_1h", cacheWrite1hPrice, usage.CacheWrite1hTokens},
			{"image_input", prices.ImageInput, usage.ImageInputTokens},
			{"audio_input", prices.AudioInput, usage.AudioInputTokens},
			{"audio_output", prices.AudioOutput, usage.AudioOutputTokens},
		}
		million := decimal.NewFromInt(1_000_000)
		for _, field := range fields {
			if field.tokens == 0 || strings.TrimSpace(field.raw) == "" {
				continue
			}
			if field.tokens < 0 {
				return BillingCalculation{}, fmt.Errorf("%s tokens cannot be negative", field.name)
			}
			price, err := parseBillingPrice(field.raw, field.name)
			if err != nil {
				return BillingCalculation{}, err
			}
			cost := decimal.NewFromInt(field.tokens).Mul(price).Div(million)
			subtotal = subtotal.Add(cost)
			result.LineItems = append(result.LineItems, BillingLineItem{
				Field: field.name, Tokens: field.tokens, PricePerMillion: price.String(), CostUSD: cost.String(),
			})
		}
	}
	for name, units := range usage.ToolUsage {
		if units == 0 {
			continue
		}
		tool, ok := policy.Tools[name]
		if !ok {
			return BillingCalculation{}, fmt.Errorf("tool %s price is not configured in policy", name)
		}
		price, err := parseBillingPrice(tool.Price, "tool "+name)
		if err != nil {
			return BillingCalculation{}, err
		}
		cost := price.Mul(decimal.NewFromInt(units))
		if tool.Unit == "per_thousand_calls" {
			cost = cost.Div(decimal.NewFromInt(1000))
		}
		subtotal = subtotal.Add(cost)
		result.LineItems = append(result.LineItems, BillingLineItem{
			Field: name, Units: units, UnitPrice: price.String(), CostUSD: cost.String(),
		})
	}

	multiplier, applied := EvaluateAdjustments(policy, requestCtx)
	result.SubtotalUSD = subtotal.String()
	result.AdjustmentMultiplier = multiplier.String()
	result.AppliedAdjustments = applied
	result.TotalUSD = subtotal.Mul(multiplier).String()
	return result, nil
}

func parseBillingPrice(raw, field string) (decimal.Decimal, error) {
	value, err := decimal.NewFromString(raw)
	if err != nil || value.IsNegative() {
		return decimal.Zero, fmt.Errorf("invalid %s price", field)
	}
	return value, nil
}
