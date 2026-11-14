package common

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesSanitizersPreserveOpaqueToolAndAgentData(t *testing.T) {
	raw := []byte(`{"input":[{"type":"message","id":"bad","role":"assistant","content":"hello"},{"type":"reasoning","encrypted_content":"invalid"},{"type":"function_call_output","call_id":"call_1","output":{"n":9007199254740993,"metadata":[{"type":"reasoning","encrypted_content":"plain tool data"}]}},{"type":"agent_message","id":"` + strings.Repeat("x", 100) + `","content":[{"type":"encrypted_content","encrypted_content":"opaque"}]}],"tools":[{"parameters":{"const":9007199254740993}}]}`)
	out, count, err := SanitizeInvalidResponsesItemIDs(raw)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	out, count, err = SanitizeInvalidResponsesEncryptedContent(out)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Contains(t, string(out), "9007199254740993")
	require.Contains(t, string(out), "plain tool data")
	require.Contains(t, string(out), "opaque")
	require.Contains(t, string(out), strings.Repeat("x", 100))
	require.Equal(t, "9007199254740993", gjson.GetBytes(out, "input.1.output.n").Raw)
}
func TestRemoveDisabledFieldsPreservesRawNumbersAndOrder(t *testing.T) {
	raw := []byte(`{"service_tier":"priority","tools":[{"parameters":{"properties":{"z":{"const":9007199254740993},"a":{"const":1e-25}}}}],"service_tier":"flex","stream_options":{"include_obfuscation":false,"custom":9007199254740993},"multi_agent":{"enabled":false}}`)
	for _, passthrough := range []bool{false, true} {
		out, err := RemoveDisabledFields(raw, dto.ChannelOtherSettings{}, passthrough)
		require.NoError(t, err)
		require.NotContains(t, string(out), "service_tier")
		require.Contains(t, string(out), `"properties":{"z":{"const":9007199254740993},"a":{"const":1e-25}}`)
		require.Equal(t, "9007199254740993", gjson.GetBytes(out, "stream_options.custom").Raw)
		require.Equal(t, "false", gjson.GetBytes(out, "multi_agent.enabled").Raw)
		require.Equal(t, passthrough, gjson.GetBytes(out, "stream_options.include_obfuscation").Exists())
	}
}

func TestPassthroughHeaderOverridesKeepRawBody(t *testing.T) {
	raw := []byte(`{"input":[{"value":9007199254740993}],"model":"m"}`)
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ParamOverride: map[string]interface{}{"model": "changed", "operations": []map[string]interface{}{{"mode": "set_header", "path": "OpenAI-Beta", "value": "responses_multi_agent=v1"}, {"mode": "set", "path": "input", "value": "do not apply"}}}}}
	before := string(raw)
	require.NoError(t, ApplyPassthroughHeaderOverrides(raw, info))
	require.Equal(t, before, string(raw))
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "responses_multi_agent=v1", info.RuntimeHeadersOverride["openai-beta"])
}
