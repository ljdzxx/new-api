package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisterRiskSettingsBounds(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{"RegisterRiskControlEnabled", "maybe"},
		{"RegisterRiskCooldownHours", "0"}, {"RegisterRiskCooldownHours", "721"},
		{"RegisterRiskCooldownHours", "1.5"}, {"RegisterRiskHitThreshold", "-1"},
		{"RegisterRiskHitThreshold", "0"}, {"RegisterRiskHitThreshold", "100001"},
		{"RegisterRiskHitThreshold", "18446744073709551615"},
		{"RegisterRiskRejectMessage", " "}, {"RegisterRiskRejectMessage", strings.Repeat("字", 201)},
		{"InviteRiskThreshold", "101"}, {"InviteRiskDailyLimit", "-1"},
	} {
		require.Error(t, ValidateRegisterRiskOption(test.key, test.value), "%s=%s", test.key, test.value)
	}
	for _, test := range []struct{ key, value string }{
		{"RegisterRiskControlEnabled", "true"}, {"RegisterRiskControlEnabled", "false"},
		{"RegisterRiskCooldownHours", "24"}, {"RegisterRiskHitThreshold", "1"},
		{"RegisterRiskRejectMessage", strings.Repeat("字", 200)},
	} {
		require.NoError(t, ValidateRegisterRiskOption(test.key, test.value))
	}
	weights := DefaultInviteRiskScoreWeights()
	weights.IP, weights.Fingerprint = -1, 56 // Still sums to 100.
	require.Error(t, weights.Validate())
}
