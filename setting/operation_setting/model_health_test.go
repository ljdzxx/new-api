package operation_setting

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestModelHealthSettingsValidation(t *testing.T) {
	for _, value := range []string{"", "200", "600", "429,", "429-503", "NaN"} {
		require.Error(t, ValidateModelHealthOption("monitor_setting.model_health_status_codes", value))
	}
	require.NoError(t, ValidateModelHealthOption("monitor_setting.model_health_status_codes", "429, 502,503"))
	for _, value := range []string{"0", "-1", "1001", "2.5", "18446744073686646784"} {
		require.Error(t, ValidateModelHealthOption("monitor_setting.model_health_threshold", value))
	}
	require.NoError(t, ValidateModelHealthOption("monitor_setting.model_health_threshold", "2"))
	require.Error(t, ValidateModelHealthOption("monitor_setting.model_health_enabled", "1"))
}
