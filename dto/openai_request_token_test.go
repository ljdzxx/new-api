package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesTokenMetaKeepsUnknownInputTextAndSkipsEncryptedContent(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: json.RawMessage(`[{"type":"message","content":[{"type":"output_text","text":"historical assistant content"}]},{"type":"compaction","encrypted_content":"opaque-history-payload"}]`),
	}

	meta := req.GetTokenCountMeta()
	require.NotNil(t, meta)
	require.True(t, strings.Contains(meta.CombineText, "historical assistant content"))
	require.NotContains(t, meta.CombineText, "opaque-history-payload")
}

func TestOpenAIResponsesTokenMetaExtractsBillableTextOnly(t *testing.T) {
	imageData := "data:image/png;base64," + strings.Repeat("A", 1024)
	imageURL := "https://example.com/image.png"
	fileData := strings.Repeat("B", 1024)
	fileURL := "https://example.com/document.txt"
	req := &OpenAIResponsesRequest{
		Input: json.RawMessage(`[
			{"type":"message","id":"msg_123","role":"user","metadata":{"private":"do not count"},"content":[
				{"type":"input_text","text":"input text"},
				{"type":"input_image","image_url":"` + imageData + `"},
				{"type":"input_image","image_url":"` + imageURL + `"},
				{"type":"input_file","file_data":"` + fileData + `"},
				{"type":"input_file","file_url":"` + fileURL + `"},
				{"type":"output_text","text":"output text"}
			]},
			{"type":"function_call","id":"fc_123","name":"lookup_weather","arguments":"{\"city\":\"Paris\"}"},
			{"type":"function_call_output","call_id":"fc_123","output":"tool output"},
			{"type":"reasoning","summary":[{"type":"summary_text","text":"reasoning summary"}]},
			{"type":"compaction","encrypted_content":"opaque blob","summary":"compaction summary"}
		]`),
		Instructions: json.RawMessage(`{"role":"developer","content":[{"type":"input_text","text":"instruction text"}],"id":"instr_1"}`),
		Metadata:     json.RawMessage(`{"tenant":"do not count","blob":"` + strings.Repeat("C", 1024) + `"}`),
		Text:         json.RawMessage(`{"format":{"type":"json_schema","name":"do not count"}}`),
		ToolChoice:   json.RawMessage(`{"type":"function","name":"do not count"}`),
		Prompt:       json.RawMessage(`{"id":"pmpt_123","version":"1","variables":{"name":"do not count"}}`),
		Tools: json.RawMessage(`[
			{"type":"function","name":"search","description":"search description","parameters":{"type":"object","properties":{"query":{"type":"string","description":"query description"}},"required":["query"]}},
			{"type":"custom","id":"tool_123","description":"custom description"}
		]`),
	}

	meta := req.GetTokenCountMeta()
	require.NotNil(t, meta)
	for _, want := range []string{
		"input text",
		"output text",
		"lookup_weather",
		`{"city":"Paris"}`,
		"tool output",
		"reasoning summary",
		"compaction summary",
		"instruction text",
		"search",
		"search description",
		"query description",
		"custom description",
	} {
		require.Contains(t, meta.CombineText, want)
	}
	for _, unwanted := range []string{
		imageData,
		imageURL,
		fileData,
		fileURL,
		"opaque blob",
		"msg_123",
		"fc_123",
		"instr_1",
		"do not count",
		"tenant",
		"json_schema",
		"tool_123",
	} {
		require.NotContains(t, meta.CombineText, unwanted)
	}

	// Media remains represented in FileMeta and is billed by the media token
	// path rather than by counting its raw payload as text.
	require.Len(t, meta.Files, 3)
	require.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
	require.Equal(t, types.FileTypeImage, meta.Files[1].FileType)
	require.Equal(t, types.FileTypeFile, meta.Files[2].FileType)
}
