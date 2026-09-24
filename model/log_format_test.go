package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price":  0.004,
		"client_model": "gpt-5.4",
		"admin_info": map[string]interface{}{
			"routing_model": "gpt-5.4-openai-compact",
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "model_price")
	require.Equal(t, "gpt-5.4", parsed["client_model"])
}

func TestFormatUserLogsPreservesResponsesBadges(t *testing.T) {
	logs := []*Log{{Other: common.MapToJsonStr(map[string]interface{}{
		"responses_badges": []string{"R1", "E1", "E2"},
		"admin_info":       map[string]interface{}{"channel_id": 7},
	})}}
	formatUserLogs(logs, 0)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, []interface{}{"R1", "E1", "E2"}, parsed["responses_badges"])
	require.NotContains(t, parsed, "admin_info")
}

func TestFormatUserLogsPreservesDownstreamStreamCancellation(t *testing.T) {
	logs := []*Log{{Other: common.MapToJsonStr(map[string]interface{}{
		"stream_canceled_by_downstream": true,
		"responses_badges":              []string{"R1"},
		"admin_info":                    map[string]interface{}{"channel_id": 7},
	})}}
	formatUserLogs(logs, 0)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, true, parsed["stream_canceled_by_downstream"])
	require.Equal(t, []interface{}{"R1"}, parsed["responses_badges"])
	require.NotContains(t, parsed, "admin_info")
}

func TestFormatUserLogsPreservesClientEffortAndStripsUpstreamEffort(t *testing.T) {
	logs := []*Log{{Other: common.MapToJsonStr(map[string]interface{}{
		"reasoning_effort":    "xhigh",
		"is_model_mapped":     true,
		"upstream_model_name": "gpt-5.6-terra",
		"admin_info": map[string]interface{}{
			"upstream_reasoning_effort": "max",
			"upstream_model_name":       "gpt-5.6-terra",
		},
	})}}

	formatUserLogs(logs, 0)
	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "xhigh", parsed["reasoning_effort"])
	require.NotContains(t, parsed, "admin_info")
	require.NotContains(t, parsed, "is_model_mapped")
	require.NotContains(t, parsed, "upstream_model_name")
}
