package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const recoveryReasoning = `{"type":"reasoning","id":"rs_old","summary":[],"encrypted_content":"gAAAAAB_old_account_reasoning_ciphertext"}`

func TestRebuildResponsesInputWithoutReasoningPreservesStructuredHistory(t *testing.T) {
	body := []byte(`{
		"model":"test-model","temperature":0,"parallel_tool_calls":false,"store":false,
		"extra":{"large_integer":9007199254740993,"encrypted_content":"metadata"},
		"input":[
			{"role":"system","content":"Keep these instructions"},
			{"role":"user","content":[{"type":"input_text","text":"继续"},{"type":"input_file","file_id":"file_keep"}]},
			` + recoveryReasoning + `,
			{"type":"message","id":"msg_old","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Running tools"}]},
			{"type":"function_call","id":"fc_old","call_id":"call_a","name":"run","arguments":"{\"n\":9007199254740993}"},
			{"type":"custom_tool_call","id":"ctc_old","call_id":"call_b","name":"edit","input":"patch"},
			{"type":"custom_tool_call_output","id":"ctco_old","call_id":"call_b","output":"applied"},
			{"type":"function_call_output","call_id":"call_a","output":[{"type":"input_text","text":"success"},{"type":"input_image","file_id":"file_result"}]},
			{"type":"reasoning","summary":[{"type":"summary_text","text":"old summary"}]}
		],"max_output_tokens":0
	}`)
	original := string(body)
	rebuilt, removed, err := RebuildResponsesInputWithoutReasoning(body)
	require.NoError(t, err)
	require.Equal(t, 2, removed)
	require.Equal(t, original, string(body), "never change client history")
	require.True(t, gjson.ValidBytes(rebuilt))
	require.Less(t, len(rebuilt), len(body))
	require.Equal(t, "9007199254740993", gjson.GetBytes(rebuilt, "extra.large_integer").Raw)
	require.Equal(t, "metadata", gjson.GetBytes(rebuilt, "extra.encrypted_content").String())
	require.Equal(t, "0", gjson.GetBytes(rebuilt, "temperature").Raw)
	require.Equal(t, "0", gjson.GetBytes(rebuilt, "max_output_tokens").Raw)
	require.Equal(t, "false", gjson.GetBytes(rebuilt, "store").Raw)
	require.Equal(t, "false", gjson.GetBytes(rebuilt, "parallel_tool_calls").Raw)
	before, after := gjson.GetBytes(body, "input").Array(), gjson.GetBytes(rebuilt, "input").Array()
	require.Len(t, after, 7)
	for i, originalIndex := range []int{0, 1, 3, 4, 5, 6, 7} {
		require.False(t, after[i].Get("id").Exists())
		for _, key := range []string{"type", "role", "phase", "content", "name", "call_id", "arguments", "input", "output"} {
			require.Equal(t, before[originalIndex].Get(key).Raw, after[i].Get(key).Raw, key)
		}
	}
	require.Contains(t, string(rebuilt), "file_keep")
	require.Contains(t, string(rebuilt), "file_result")
}

func TestRebuildResponsesInputWithoutReasoningRefusesIncompleteHistory(t *testing.T) {
	for _, tc := range []struct{ name, input, extra string }{
		{"compaction", `{"type":"compaction","encrypted_content":"opaque"}`, ""},
		{"previous response", `{"role":"user","content":"continue"}`, `,"previous_response_id":"resp_old"`},
		{"conversation", `{"role":"user","content":"continue"}`, `,"conversation":{"id":"conv_old"}`},
		{"reference", `{"type":"item_reference","id":"msg_old"}`, ""},
		{"hosted tool", `{"type":"computer_call","id":"cu_old"}`, ""},
		{"orphan output", `{"type":"function_call_output","call_id":"call_a","output":"success"}`, ""},
		{"unfinished call", `{"type":"function_call","call_id":"call_a","name":"run","arguments":"{}"}`, ""},
		{"wrong output type", `{"type":"function_call","call_id":"call_a","name":"run","arguments":"{}"},{"type":"custom_tool_call_output","call_id":"call_a","output":"success"}`, ""},
		{"duplicate call", `{"type":"function_call","call_id":"call_a","name":"run","arguments":"{}"},{"type":"function_call","call_id":"call_a","name":"run","arguments":"{}"}`, ""},
		{"incomplete message", `{"type":"message","id":"msg_old"}`, ""},
		{"other ciphertext", `{"role":"user","content":"hi","encrypted_content":"opaque"}`, ""},
		{"scalar item", `"hello"`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"input":[` + recoveryReasoning + `,` + tc.input + `]` + tc.extra + `}`)
			rebuilt, removed, err := RebuildResponsesInputWithoutReasoning(body)
			require.Error(t, err)
			require.Nil(t, rebuilt)
			require.Zero(t, removed)
		})
	}
}

func TestRebuildResponsesInputWithoutReasoningNoOp(t *testing.T) {
	for _, body := range []string{
		`{"input":"hi"}`, `{"input":[]}`, `{"input":[{"role":"user","content":"hi"}]}`,
		`{"input":[` + recoveryReasoning + `]}`,
		`{"input":[{"type":"reasoning","summary":[]},{"role":"user","content":"hi"}]}`,
	} {
		rebuilt, removed, err := RebuildResponsesInputWithoutReasoning([]byte(body))
		require.NoError(t, err)
		require.Nil(t, rebuilt)
		require.Zero(t, removed)
	}
	_, _, err := RebuildResponsesInputWithoutReasoning([]byte(`{"input":`))
	require.Error(t, err)
}

func TestRebuildResponsesInputWithoutReasoningNullReferences(t *testing.T) {
	body := []byte(`{"conversation":null,"previous_response_id":"","input":[` + recoveryReasoning + `,{"role":"user","content":"hi"}]}`)
	rebuilt, removed, err := RebuildResponsesInputWithoutReasoning(body)
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	require.True(t, gjson.ValidBytes(rebuilt))
}

func BenchmarkRebuildResponsesInputWithoutReasoning(b *testing.B) {
	body := []byte(`{"input":[` + recoveryReasoning + `,{"role":"user","content":"` + strings.Repeat("x", 1024*1024) + `"}]}`)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _, err := RebuildResponsesInputWithoutReasoning(body)
		if err != nil {
			b.Fatal(err)
		}
	}
}
