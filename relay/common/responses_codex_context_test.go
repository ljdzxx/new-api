package common

import (
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func jsonQuote(t *testing.T, value string) string {
	t.Helper()
	encoded, err := basecommon.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

func TestNormalizeCodexUntrustedContextRewritesStandaloneOutputs(t *testing.T) {
	open := `{"page_id":null}`
	selected := "Companion Page snapshot part 1 of 1:\n" + `{"page_id":"page_1"}`
	body := []byte(`{"model":"gpt-5","previous_response_id":"resp_history","metadata":{"large":9007199254740993},"input":[` +
		`{"type":"function_call_output","id":"fco_page","namespace":"agent","name":"codex_apps_open_page","output":` + jsonQuote(t, open) + `},` +
		`{"type":"function_call_output","namespace":"agent","name":"codex_apps_selected_page_part_1_of_1","output":` + jsonQuote(t, selected) + `},` +
		`{"type":"function_call","call_id":"call_real","name":"read_file","arguments":"{}"},` +
		`{"type":"function_call_output","call_id":"call_real","output":"result"},` +
		`{"role":"user","content":"hello"}]}`)

	got, names, err := NormalizeCodexUntrustedContext(body)
	require.NoError(t, err)
	require.Equal(t, []string{"codex_apps_open_page", "codex_apps_selected_page_part_1_of_1"}, names)

	first := gjson.GetBytes(got, "input.0")
	require.Equal(t, "message", first.Get("type").String())
	require.Equal(t, "user", first.Get("role").String())
	require.Equal(t, "<external_codex_apps_open_page>\n"+open+"\n</external_codex_apps_open_page>", first.Get("content.0.text").String())
	require.False(t, first.Get("call_id").Exists())
	require.False(t, first.Get("id").Exists())

	second := gjson.GetBytes(got, "input.1")
	require.Equal(t, "message", second.Get("type").String())
	require.Equal(t, "<external_codex_apps_selected_page_part_1_of_1>\n"+selected+"\n</external_codex_apps_selected_page_part_1_of_1>", second.Get("content.0.text").String())

	// Real tool calls, their outputs and plain messages stay byte-identical.
	for _, index := range []string{"input.2", "input.3", "input.4"} {
		require.JSONEq(t, gjson.GetBytes(body, index).Raw, gjson.GetBytes(got, index).Raw)
	}
	// Continuation and unmodified top-level values are preserved verbatim.
	require.Equal(t, "resp_history", gjson.GetBytes(got, "previous_response_id").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(got, "metadata.large").Raw)
	require.Equal(t, "gpt-5", gjson.GetBytes(got, "model").String())

	again, names, err := NormalizeCodexUntrustedContext(got)
	require.NoError(t, err)
	require.Empty(t, names)
	require.Equal(t, got, again)
}

func TestNormalizeCodexUntrustedContextPreservesRealAndMalformedOutputs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		override map[string]any
	}{
		{"real call", map[string]any{"call_id": "call_real"}},
		{"null call id", map[string]any{"call_id": nil}},
		{"numeric call id", map[string]any{"call_id": 42}},
		{"other namespace", map[string]any{"namespace": "other"}},
		{"missing namespace", map[string]any{"namespace": nil}},
		{"numeric namespace", map[string]any{"namespace": 42}},
		{"other tool", map[string]any{"name": "read_file"}},
		{"other type", map[string]any{"type": "custom_tool_call_output"}},
		{"non-string output", map[string]any{"output": []any{map[string]any{"type": "encrypted_content", "encrypted_content": "sealed"}}}},
		{"blank output", map[string]any{"output": " "}},
		{"null output", map[string]any{"output": nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := map[string]any{"type": "function_call_output", "namespace": "agent", "name": "browser_info", "output": "context"}
			for key, value := range tc.override {
				item[key] = value
			}
			payload, err := basecommon.Marshal(map[string]any{"model": "gpt-5", "input": []any{item}})
			require.NoError(t, err)
			got, names, err := NormalizeCodexUntrustedContext(payload)
			require.NoError(t, err)
			require.Empty(t, names)
			require.Equal(t, payload, got)
		})
	}

	// Recognized external context cannot make an unrelated tool result valid.
	body := []byte(`{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"agent","name":"browser_info","output":"tab"},{"type":"function_call_output","namespace":"agent","name":"read_file","output":"result"}]}`)
	got, names, err := NormalizeCodexUntrustedContext(body)
	require.NoError(t, err)
	require.Equal(t, []string{"browser_info"}, names)
	require.Equal(t, "message", gjson.GetBytes(got, "input.0.type").String())
	require.Equal(t, "function_call_output", gjson.GetBytes(got, "input.1.type").String())
	require.Equal(t, "read_file", gjson.GetBytes(got, "input.1.name").String())
}

func TestNormalizeCodexUntrustedContextMultipartAndShapeChecks(t *testing.T) {
	first := "Companion Page snapshot part 1 of 2:\n" + `{"page_id":"page1","document_view":"hello`
	second := "Companion Page snapshot part 2 of 2:\n" + ` world"}`
	body := []byte(`{"model":"gpt-5","input":[` +
		`{"type":"function_call_output","namespace":"agent","name":"codex_apps_selected_page_part_1_of_2","output":` + jsonQuote(t, first) + `},` +
		`{"type":"function_call_output","namespace":"agent","name":"codex_apps_selected_page_part_2_of_2","output":` + jsonQuote(t, second) + `}]}`)
	got, names, err := NormalizeCodexUntrustedContext(body)
	require.NoError(t, err)
	require.Len(t, names, 2)
	require.Contains(t, gjson.GetBytes(got, "input.0.content.0.text").String(), first)
	require.Contains(t, gjson.GetBytes(got, "input.1.content.0.text").String(), second)

	for _, tc := range []struct{ name, output string }{
		{"codex_apps_selected_page_part_2_of_1", "Companion Page snapshot part 2 of 1:\n{}"},
		{"codex_apps_selected_page_part_1_of_1", "part 1 of 1:\n{}"},
		{"codex_apps_selected_page_part_0_of_2", "Companion Page snapshot part 0 of 2:\n{}"},
		{"codex_apps_open_page", `{"other":null}`},
		{"codex_apps_open_page", `{"page_id":1}`},
		{"codex_apps_open_page", `not json`},
		{"browser_info", ""},
	} {
		payload, err := basecommon.Marshal(map[string]any{"model": "gpt-5", "input": []any{map[string]any{
			"type": "function_call_output", "namespace": "agent", "name": tc.name, "output": tc.output,
		}}})
		require.NoError(t, err)
		got, names, err := NormalizeCodexUntrustedContext(payload)
		require.NoError(t, err)
		require.Empty(t, names, "name=%s output=%s", tc.name, tc.output)
		require.Equal(t, payload, got)
	}

	// A blank call_id is treated as missing, matching the upstream validation.
	body = []byte(`{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"agent","name":"untrusted_context","call_id":" ","output":"external"}]}`)
	got, names, err = NormalizeCodexUntrustedContext(body)
	require.NoError(t, err)
	require.Equal(t, []string{"untrusted_context"}, names)
	require.Equal(t, "<external_untrusted_context>external</external_untrusted_context>", gjson.GetBytes(got, "input.0.content.0.text").String())
}

func TestNormalizeCodexUntrustedContextNoopAndInvalid(t *testing.T) {
	unchanged := []byte(`{"model":"gpt-5","input":[{"role":"user","content":"hello"}]}`)
	got, names, err := NormalizeCodexUntrustedContext(unchanged)
	require.NoError(t, err)
	require.Empty(t, names)
	require.Equal(t, unchanged, got)

	got, names, err = NormalizeCodexUntrustedContext([]byte(`{"model":"gpt-5","input":"hello"}`))
	require.NoError(t, err)
	require.Empty(t, names)

	_, _, err = NormalizeCodexUntrustedContext([]byte(`{"model":`))
	require.Error(t, err)
}
