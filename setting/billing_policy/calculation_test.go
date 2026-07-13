package billing_policy

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletePolicyJSONReachesEveryBillingDefinition(t *testing.T) {
	backup := GetConfig()
	t.Cleanup(func() {
		require.NoError(t, UpdateFromJSON(marshalForTest(t, backup)))
	})

	const raw = `{
  "schema_version": 1,
  "revision": 42,
  "state": "active",
  "migration": {
    "version": 1,
    "source_checksum": "sha256:complete-policy-test",
    "migrated_at": 1783785600
  },
  "policies": {
    "complete-token-model": {
      "version": 1,
      "mode": "tiered",
      "currency": "USD",
      "unit": "per_million_tokens",
      "tiers": [
        {
          "id": "all-fields",
          "priority": 1,
          "conditions": [
            {"metric": "input_total_tokens", "operator": "lte", "value": 20000000},
            {"metric": "output_total_tokens", "operator": "gt", "value": 0}
          ],
          "prices": {
            "input": "1",
            "output": "2",
            "cache_read": "3",
            "cache_write": "5",
            "cache_write_5m": "6",
            "cache_write_1h": "7",
            "image_input": "8",
            "audio_input": "9",
            "audio_output": "10"
          }
        },
        {
          "id": "fallback",
          "priority": 2,
          "fallback": true,
          "prices": {"input": "11"}
        }
      ],
      "adjustments": [
        {
          "id": "all-condition-fields",
          "conditions": [
            {"source": "header", "path": "x-plan", "operator": "contains", "value": "pro"},
            {"source": "header", "path": "x-present", "operator": "exists"},
            {"source": "param", "path": "service_tier", "operator": "eq", "value": "priority"},
            {"source": "param", "path": "score", "operator": "gt", "value": "10"},
            {"source": "param", "path": "score", "operator": "gte", "value": "15"},
            {"source": "param", "path": "score", "operator": "lt", "value": "16"},
            {"source": "param", "path": "score", "operator": "lte", "value": "15"},
            {"source": "hour", "operator": "eq", "value": "10", "timezone": "Asia/Shanghai"},
            {"source": "weekday", "operator": "eq", "value": "0", "timezone": "Asia/Shanghai"}
          ],
          "multiplier": "2"
        }
      ]
    },
    "complete-request-model": {
      "version": 1,
      "mode": "per_request",
      "currency": "USD",
      "unit": "per_request",
      "price": "12.5"
    }
  }
}`

	require.NoError(t, UpdateFromJSON(raw))
	config := GetConfig()
	assert.Equal(t, 1, config.SchemaVersion)
	assert.Equal(t, int64(42), config.Revision)
	assert.Equal(t, StateActive, config.State)
	assert.Equal(t, 1, config.Migration.Version)
	assert.Equal(t, "sha256:complete-policy-test", config.Migration.SourceChecksum)
	assert.Equal(t, int64(1783785600), config.Migration.MigratedAt)

	policy, ok := Resolve("complete-token-model")
	require.True(t, ok)
	usage := BillingUsage{
		TierInputTotalTokens:  10_000_000,
		TierOutputTotalTokens: 1_000_000,
		InputTokens:           1_000_000,
		OutputTokens:          1_000_000,
		CacheReadTokens:       1_000_000,
		CacheWriteTokens:      1_000_000,
		CacheWrite5mTokens:    1_000_000,
		CacheWrite1hTokens:    1_000_000,
		ImageInputTokens:      1_000_000,
		AudioInputTokens:      1_000_000,
		AudioOutputTokens:     1_000_000,
	}
	calculation, err := CalculateBilling(policy, usage, RequestContext{
		Headers: map[string]string{"x-plan": "pro-plus", "x-present": "yes"},
		Body:    []byte(`{"service_tier":"priority","score":15}`),
		Now:     time.Date(2026, 7, 12, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
	})
	require.NoError(t, err)
	assert.Equal(t, "all-fields", calculation.TierID)
	assert.Equal(t, "51", calculation.SubtotalUSD)
	assert.Equal(t, "2", calculation.AdjustmentMultiplier)
	assert.Equal(t, "102", calculation.TotalUSD)
	require.Len(t, calculation.LineItems, 9)
	fields := []string{"input", "output", "cache_read", "cache_write", "cache_write_5m", "cache_write_1h", "image_input", "audio_input", "audio_output"}
	costs := []string{"1", "2", "3", "5", "6", "7", "8", "9", "10"}
	for index, field := range fields {
		assert.Equal(t, field, calculation.LineItems[index].Field)
		assert.Equal(t, int64(1_000_000), calculation.LineItems[index].Tokens)
		assert.Equal(t, costs[index], calculation.LineItems[index].CostUSD)
	}
	require.Len(t, calculation.AppliedAdjustments, 1)

	legacy, tierID, err := ToLegacyValuesForUsage(policy, Usage{InputTotalTokens: 10_000_000, OutputTotalTokens: 1_000_000})
	require.NoError(t, err)
	assert.Equal(t, "all-fields", tierID)
	assert.Equal(t, 0.5, legacy.ModelRatio)
	assert.Equal(t, 2.0, legacy.CompletionRatio)
	assert.Equal(t, 3.0, legacy.CacheRatio)
	assert.Equal(t, 5.0, legacy.CacheCreationRatio)
	assert.Equal(t, 6.0, legacy.CacheCreation5mRatio)
	assert.Equal(t, 7.0, legacy.CacheCreation1hRatio)
	assert.Equal(t, 8.0, legacy.ImageRatio)
	assert.Equal(t, 9.0, legacy.AudioRatio)
	assert.InDelta(t, 10.0/9.0, legacy.AudioCompletionRatio, 1e-12)

	requestPolicy, ok := Resolve("complete-request-model")
	require.True(t, ok)
	requestCalculation, err := CalculateBilling(requestPolicy, BillingUsage{}, RequestContext{})
	require.NoError(t, err)
	assert.Equal(t, "12.5", requestCalculation.TotalUSD)
	require.Len(t, requestCalculation.LineItems, 1)
	assert.Equal(t, int64(1), requestCalculation.LineItems[0].Units)

	serialized, err := common.Marshal(config)
	require.NoError(t, err)
	var roundTrip Config
	require.NoError(t, common.Unmarshal(serialized, &roundTrip))
	assert.Equal(t, config, roundTrip)
}

func TestValidatePolicyRejectsInvalidUnitAndPerRequestPrice(t *testing.T) {
	invalidUnit := testTokenPolicy("1")
	invalidUnit.Unit = "per_request"
	require.ErrorContains(t, ValidatePolicy(invalidUnit), "per_million_tokens")

	invalidPrice := Policy{Version: SchemaVersion, Mode: "per_request", Currency: "USD", Unit: "per_request", Price: "-1"}
	require.ErrorContains(t, ValidatePolicy(invalidPrice), "non-negative decimal")
}

func TestCalculateBillingIncludesPolicyToolPricesAndFrozenAdjustment(t *testing.T) {
	policy := testTokenPolicy("1")
	policy.Tools = map[string]ToolPrice{
		ToolWebSearchStandard:             {Unit: "per_thousand_calls", Price: "10"},
		ToolImagePrefix + "low.1024x1024": {Unit: "per_request", Price: "0.011"},
	}
	policy.Adjustments = []Adjustment{{
		ID: "live-rule", Multiplier: "9",
		Conditions: []AdjustmentCondition{{Source: "header", Path: "x-plan", Operator: "eq", Value: "live"}},
	}}
	calculation, err := CalculateBilling(policy, BillingUsage{ToolUsage: map[string]int64{
		ToolWebSearchStandard:             2,
		ToolImagePrefix + "low.1024x1024": 1,
	}}, RequestContext{
		Headers:           map[string]string{"x-plan": "live"},
		FreezeAdjustments: true, AdjustmentMultiplier: "2",
		AppliedAdjustments: []AppliedAdjustment{{ID: "frozen-rule", Multiplier: "2"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "0.031", calculation.SubtotalUSD)
	assert.Equal(t, "0.062", calculation.TotalUSD)
	assert.Equal(t, []AppliedAdjustment{{ID: "frozen-rule", Multiplier: "2"}}, calculation.AppliedAdjustments)
}

func TestConfigAndSnapshotAreDeepCopies(t *testing.T) {
	backup := GetConfig()
	t.Cleanup(func() { require.NoError(t, UpdateFromJSON(marshalForTest(t, backup))) })
	policy := testTokenPolicy("1")
	policy.Tiers = nil
	policy.Adjustments = []Adjustment{{ID: "a", Multiplier: "2", Conditions: []AdjustmentCondition{{Source: "header", Path: "x", Operator: "exists"}}}}
	config := NewConfig()
	config.State = StateActive
	config.Policies = map[string]Policy{"copy-model": policy}
	require.NoError(t, UpdateFromJSON(marshalForTest(t, config)))

	copyConfig := GetConfig()
	copyPolicy := copyConfig.Policies["copy-model"]
	copyPolicy.Adjustments[0].Conditions[0].Path = "mutated"
	copyPolicy.Tools[ToolFileSearch] = ToolPrice{Unit: "per_request", Price: "999"}
	copyConfig.Policies["copy-model"] = copyPolicy

	resolved, ok := Resolve("copy-model")
	require.True(t, ok)
	assert.Equal(t, "x", resolved.Adjustments[0].Conditions[0].Path)
	assert.Equal(t, "2.5", resolved.Tools[ToolFileSearch].Price)
}
