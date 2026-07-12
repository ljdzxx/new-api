package billing_policy

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePrefersExactThenMostSpecificWildcard(t *testing.T) {
	backup := GetConfig()
	t.Cleanup(func() {
		data := marshalForTest(t, backup)
		require.NoError(t, UpdateFromJSON(data))
	})

	config := NewConfig()
	config.State = StateShadow
	config.Policies = map[string]Policy{
		"gpt-*":      testTokenPolicy("1"),
		"gpt-5-*":    testTokenPolicy("2"),
		"gpt-5-mini": testTokenPolicy("3"),
	}
	require.NoError(t, UpdateFromJSON(marshalForTest(t, config)))

	policy, ok := Resolve("gpt-5-mini")
	require.True(t, ok)
	assert.Equal(t, "3", policy.Prices.Input)
	policy, ok = Resolve("gpt-5-high")
	require.True(t, ok)
	assert.Equal(t, "2", policy.Prices.Input)
}

func TestSourceChecksumIsOrderIndependent(t *testing.T) {
	a := SourceChecksum(map[string]string{"a": "1", "b": "2"})
	b := SourceChecksum(map[string]string{"b": "2", "a": "1"})
	assert.Equal(t, a, b)
}

func TestTieredPolicyMatchesRawUsage(t *testing.T) {
	policy := Policy{
		Version: SchemaVersion, Mode: "tiered", Currency: "USD", Unit: "per_million_tokens",
		Tiers: []Tier{
			{ID: "short", Priority: 10, Conditions: []TierCondition{{Metric: "input_total_tokens", Operator: "lte", Value: 200000}}, Prices: Prices{Input: "3", Output: "15"}},
			{ID: "long", Priority: 20, Fallback: true, Prices: Prices{Input: "6", Output: "22.5"}},
		},
	}
	require.NoError(t, ValidatePolicy(policy))

	values, tier, err := ToLegacyValuesForUsage(policy, Usage{InputTotalTokens: 150000})
	require.NoError(t, err)
	assert.Equal(t, "short", tier)
	assert.Equal(t, 1.5, values.ModelRatio)
	assert.Equal(t, 5.0, values.CompletionRatio)

	values, tier, err = ToLegacyValuesForUsage(policy, Usage{InputTotalTokens: 250000})
	require.NoError(t, err)
	assert.Equal(t, "long", tier)
	assert.Equal(t, 3.0, values.ModelRatio)
	assert.Equal(t, 3.75, values.CompletionRatio)
}

func TestTieredPolicyRequiresSingleFallback(t *testing.T) {
	policy := Policy{Version: SchemaVersion, Mode: "tiered", Currency: "USD", Unit: "per_million_tokens", Tiers: []Tier{{ID: "only", Priority: 1, Prices: Prices{Input: "1"}}}}
	assert.Error(t, ValidatePolicy(policy))
}

func TestCacheWritePolicySupportsGenericAndTTLSpecificPrices(t *testing.T) {
	policy := testTokenPolicy("2")
	policy.Prices.CacheWrite = "3"
	policy.Prices.CacheWrite5m = "4"
	policy.Prices.CacheWrite1h = "8"

	values, err := ToLegacyValues(policy)

	require.NoError(t, err)
	assert.Equal(t, 1.5, values.CacheCreationRatio)
	assert.Equal(t, 2.0, values.CacheCreation5mRatio)
	assert.Equal(t, 4.0, values.CacheCreation1hRatio)
}

func TestCacheWritePolicyFallsBackToLegacy5mField(t *testing.T) {
	policy := testTokenPolicy("2")
	policy.Prices.CacheWrite5m = "4"

	values, err := ToLegacyValues(policy)

	require.NoError(t, err)
	assert.Equal(t, 2.0, values.CacheCreationRatio)
	assert.Equal(t, 2.0, values.CacheCreation5mRatio)
}

func TestLegacyCacheHitsRefreshesMigratesToCacheRead(t *testing.T) {
	var prices Prices
	require.NoError(t, common.UnmarshalJsonStr(`{"input":"2","cache_hits_refreshes":"4"}`, &prices))
	assert.Equal(t, "4", prices.CacheRead)

	encoded := marshalForTest(t, prices)
	assert.Contains(t, encoded, `"cache_read":"4"`)
	assert.NotContains(t, encoded, "cache_hits_refreshes")
}

func TestCacheRatioFallsBackToLegacyCacheRead(t *testing.T) {
	policy := testTokenPolicy("2")
	policy.Prices.CacheRead = "6"

	values, err := ToLegacyValues(policy)

	require.NoError(t, err)
	assert.Equal(t, 3.0, values.CacheRatio)
}

func testTokenPolicy(input string) Policy {
	return Policy{Version: SchemaVersion, Mode: "per_token", Currency: "USD", Unit: "per_million_tokens", Prices: Prices{Input: input}}
}

func marshalForTest(t *testing.T, value any) string {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
