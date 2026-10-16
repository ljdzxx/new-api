package common

import (
	"encoding/json"
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
)

func TestRemoveDisabledFieldsPassthroughServiceTier(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	original := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = original })

	for _, mode := range []struct {
		name    string
		global  bool
		channel bool
	}{
		{"channel", false, true},
		{"global", true, false},
		{"both", true, true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			settings.PassThroughRequestEnabled = mode.global
			for _, fields := range []string{
				`"service_tier":"priority",`,
				`"service_tier":"fast",`,
				`"service_tier":"flex",`,
				`"service_tier":null,`,
				`"service_tier":"",`,
				`"service_tier":"default","service_tier":"priority",`,
				`"service_\u0074ier":"priority",`,
				`"service_tier":"default","service_\u0074ier":"priority",`,
			} {
				t.Run(fields, func(t *testing.T) {
					input := []byte(`{` + fields + `"model":"test","store":false,"safety_identifier":"user-123","speed":"fast","stream_options":{"include_obfuscation":false},"extension":{"service_tier":"nested","large":18446744073709551615,"precise":0.1234567890123456789,"zero":0,"enabled":false}}`)
					before := string(input)
					out, err := RemoveDisabledFields(input, dto.ChannelOtherSettings{DisableStore: true}, mode.channel)
					require.NoError(t, err)
					var want, got map[string]json.RawMessage
					require.NoError(t, appcommon.Unmarshal(input, &want))
					require.NoError(t, appcommon.Unmarshal(out, &got))
					delete(want, "service_tier")
					require.Equal(t, want, got)
					require.Equal(t, before, string(input), "cached body must remain available for channel retries")

					out, err = RemoveDisabledFields(input, dto.ChannelOtherSettings{AllowServiceTier: true}, mode.channel)
					require.NoError(t, err)
					require.Equal(t, before, string(out), "explicitly allowed tiers must pass through unchanged")
				})
			}

			t.Run("absent", func(t *testing.T) {
				input := []byte(`{ "model": "test", "extension": {"service_tier":"nested"} }`)
				out, err := RemoveDisabledFields(input, dto.ChannelOtherSettings{}, mode.channel)
				require.NoError(t, err)
				require.Equal(t, input, out)
			})
			t.Run("invalid_json", func(t *testing.T) {
				out, err := RemoveDisabledFields([]byte(`{"service_tier":"priority",`), dto.ChannelOtherSettings{}, mode.channel)
				require.Error(t, err)
				require.Nil(t, out, "filter failures must not forward the unfiltered body")
			})
		})
	}
}
