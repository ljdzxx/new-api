package types

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServiceTierMatchingAndValidation(t *testing.T) {
	rule := ServiceTierBillingRule{Model: "gpt-6-astra", Tier: "Ultrafast", TagColor: "green", TagType: "solid", Multiplier: 6}
	fallback := rule
	fallback.Model, fallback.Tier, fallback.Multiplier = "*", "fast", 2
	config := &ServiceTierBilling{Enabled: true, Rules: []ServiceTierBillingRule{fallback, rule}}
	exact, err := config.Match("gpt-6-astra")
	require.NoError(t, err)
	require.Equal(t, rule, *exact)
	wildcard, err := config.Match("any-model")
	require.NoError(t, err)
	require.Equal(t, fallback, *wildcard)
	config.Rules[1].Multiplier = 3
	require.Equal(t, 6.0, exact.Multiplier, "request snapshot must not follow channel edits")
	config.Enabled = false
	matched, err := config.Match("gpt-6-astra")
	require.NoError(t, err)
	require.Nil(t, matched)
	for _, model := range []string{"*-xxx", "gpt-*", "**", "", " gpt-6-astra"} {
		invalid := rule
		invalid.Model = model
		require.Error(t, invalid.Validate())
	}
	for _, multiplier := range []float64{0, -1, 1001, math.NaN(), math.Inf(1), math.Inf(-1)} {
		invalid := rule
		invalid.Multiplier = multiplier
		require.Error(t, invalid.Validate())
	}
	for _, change := range []func(*ServiceTierBillingRule){
		func(r *ServiceTierBillingRule) { r.Tier = "" },
		func(r *ServiceTierBillingRule) { r.TagColor = "success" },
		func(r *ServiceTierBillingRule) { r.TagType = "warn" },
	} {
		invalid := rule
		change(&invalid)
		require.Error(t, invalid.Validate())
	}
	require.Error(t, (&ServiceTierBilling{Enabled: true}).Validate())
	require.Error(t, (&ServiceTierBilling{Rules: []ServiceTierBillingRule{rule, rule}}).Validate())
}
