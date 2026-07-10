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

func testTokenPolicy(input string) Policy {
	return Policy{Version: SchemaVersion, Mode: "per_token", Currency: "USD", Unit: "per_million_tokens", Prices: Prices{Input: input}}
}

func marshalForTest(t *testing.T, value any) string {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
