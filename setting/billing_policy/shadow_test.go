package billing_policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShadowStatsSeparatePreConsumeAndSettlement(t *testing.T) {
	ResetShadowStats()
	t.Cleanup(ResetShadowStats)

	ObserveShadowPreConsume("gpt-test", 100, 200)
	ObserveShadowSettlement("gpt-test", 50, 50)

	stats := GetShadowStats()
	require.EqualValues(t, 2, stats.Observations)
	require.EqualValues(t, 1, stats.Matches)
	require.EqualValues(t, 1, stats.Mismatches)
	require.Zero(t, stats.Errors)
	require.EqualValues(t, 1, stats.PreConsume.Observations)
	require.EqualValues(t, 1, stats.PreConsume.Mismatches)
	require.EqualValues(t, 1, stats.Settlement.Observations)
	require.EqualValues(t, 1, stats.Settlement.Matches)
	require.EqualValues(t, 2, stats.ByModel["gpt-test"])
	require.EqualValues(t, 1, stats.PreConsume.ByModel["gpt-test"])
	require.EqualValues(t, 1, stats.Settlement.ByModel["gpt-test"])
	require.True(t, ShadowReadyForSwitch(stats))
}

func TestShadowReadyForSwitchRejectsErrorsAndSettlementMismatches(t *testing.T) {
	tests := []struct {
		name    string
		observe func()
	}{
		{
			name: "no settlement observations",
			observe: func() {
				ObserveShadowPreConsume("gpt-test", 100, 200)
			},
		},
		{
			name: "pre-consume calculation error",
			observe: func() {
				ObserveShadowPreConsumeError("gpt-test")
				ObserveShadowSettlement("gpt-test", 50, 50)
			},
		},
		{
			name: "settlement mismatch",
			observe: func() {
				ObserveShadowSettlement("gpt-test", 50, 51)
			},
		},
		{
			name: "settlement calculation error",
			observe: func() {
				ObserveShadowSettlementError("gpt-test")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ResetShadowStats()
			test.observe()
			require.False(t, ShadowReadyForSwitch(GetShadowStats()))
		})
	}
	t.Cleanup(ResetShadowStats)
}

func TestGetShadowStatsReturnsIndependentModelMaps(t *testing.T) {
	ResetShadowStats()
	t.Cleanup(ResetShadowStats)
	ObserveShadowSettlement("gpt-test", 1, 1)

	stats := GetShadowStats()
	stats.ByModel["gpt-test"] = 99
	stats.Settlement.ByModel["gpt-test"] = 99

	current := GetShadowStats()
	require.EqualValues(t, 1, current.ByModel["gpt-test"])
	require.EqualValues(t, 1, current.Settlement.ByModel["gpt-test"])
}
