package claude

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeNativeRequestExtensionsSurviveConversion(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
	}{
		{"absent", ""},
		{"objects", `,"safeguards":{"z":false,"a":{"count":0,"id":9007199254740993}},"chat_template_kwargs":{"enable_thinking":false,"limit":0,"extra":[18446744073709551615,1e-25]}`},
		{"empty", `,"safeguards":{},"chat_template_kwargs":{}`},
		{"null", `,"safeguards":null,"chat_template_kwargs":null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := `{"model":"claude-sonnet-5-5","max_tokens":64,"messages":[{"role":"user","content":"test"}]` + tc.fields + `}`
			var request dto.ClaudeRequest
			require.NoError(t, common.Unmarshal([]byte(input), &request))
			copied, err := common.DeepCopy(&request)
			require.NoError(t, err)
			converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, copied)
			require.NoError(t, err)
			out, err := common.Marshal(converted)
			require.NoError(t, err)
			out, err = relaycommon.RemoveDisabledFields(out, dto.ChannelOtherSettings{}, false)
			require.NoError(t, err)
			out, err = relaycommon.ApplyParamOverride(out, map[string]any{"max_tokens": 128}, nil)
			require.NoError(t, err)
			for _, field := range []string{"safeguards", "chat_template_kwargs"} {
				want := gjson.Get(input, field)
				got := gjson.GetBytes(out, field)
				require.Equal(t, want.Exists(), got.Exists(), field)
				require.Equal(t, want.Raw, got.Raw, field)
			}
		})
	}
}
