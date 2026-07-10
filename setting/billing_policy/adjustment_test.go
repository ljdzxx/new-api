package billing_policy

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestEvaluateAdjustmentsMultipliesAllMatches(t *testing.T) {
	policy := testTokenPolicy("1")
	policy.Adjustments = []Adjustment{
		{ID: "priority", Multiplier: "2", Conditions: []AdjustmentCondition{{Source: "param", Path: "service_tier", Operator: "eq", Value: "priority"}}},
		{ID: "beta", Multiplier: "0.5", Conditions: []AdjustmentCondition{{Source: "header", Path: "x-beta", Operator: "contains", Value: "fast"}}},
	}
	multiplier, applied := EvaluateAdjustments(policy, RequestContext{
		Headers: map[string]string{"x-beta": "fast-mode"},
		Body:    []byte(`{"service_tier":"priority"}`),
	})
	assert.True(t, multiplier.Equal(decimalOne()))
	assert.Len(t, applied, 2)
}

func TestEvaluateTimeAdjustment(t *testing.T) {
	policy := testTokenPolicy("1")
	policy.Adjustments = []Adjustment{{
		ID: "night", Multiplier: "0.5",
		Conditions: []AdjustmentCondition{{Source: "hour", Timezone: "Asia/Shanghai", Operator: "gte", Value: "21"}},
	}}
	multiplier, applied := EvaluateAdjustments(policy, RequestContext{Now: time.Date(2026, 1, 1, 22, 0, 0, 0, time.FixedZone("CST", 8*3600))})
	assert.Equal(t, "0.5", multiplier.String())
	assert.Len(t, applied, 1)
}

func decimalOne() decimal.Decimal { return decimal.NewFromInt(1) }
