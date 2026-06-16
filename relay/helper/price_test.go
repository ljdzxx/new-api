package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestGetEffectiveGlobalModelRatioIncludesChannelRatio(t *testing.T) {
	origin := ratio_setting.GetGlobalModelRatio()
	t.Cleanup(func() {
		ratio_setting.SetGlobalModelRatio(origin)
	})
	ratio_setting.SetGlobalModelRatio(1.5)

	systemRatio, userRatio, channelRatio, effectiveRatio := getEffectiveGlobalModelRatio(0, 2)

	require.Equal(t, 1.5, systemRatio)
	require.Equal(t, 1.0, userRatio)
	require.Equal(t, 2.0, channelRatio)
	require.Equal(t, 3.0, effectiveRatio)
}
