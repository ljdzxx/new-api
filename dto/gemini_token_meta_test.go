package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestGeminiTokenMetaCountsAgentHistoryAndToolMedia(t *testing.T) {
	var req GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(`{
		"systemInstruction":{"parts":[{"text":"system rules"}]},
		"generationConfig":{"mediaResolution":"MEDIA_RESOLUTION_LOW"},
		"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT","properties":{"x":{"type":"INTEGER"}}}}]}],
		"contents":[{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"x":3}}}]},
		{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"content":"line one\nline two","result":[1,2]},"parts":[{"inlineData":{"mimeType":"image/png","data":"AAAA"},"mediaResolution":{"level":"MEDIA_RESOLUTION_HIGH"}}]}},
		{"inlineData":{"mimeType":"image/png","data":"BBBB"}},
		{"fileData":{"mimeType":"image/jpeg","fileUri":"gs://bucket/image.jpg"}}]}]}`, &req))
	before, _ := common.Marshal(req)
	meta := req.GetTokenCountMeta()
	for _, value := range []string{"system rules", "lookup", `{"x":3}`, "line one\nline two", "[1,2]", `"properties"`} {
		require.Contains(t, meta.CombineText, value)
	}
	for _, value := range []string{"AAAA", "BBBB", "gs://", `line one\nline two`} {
		require.NotContains(t, meta.CombineText, value)
	}
	require.Len(t, meta.Files, 3)
	require.Equal(t, "MEDIA_RESOLUTION_HIGH", meta.Files[0].Detail)
	require.Equal(t, "MEDIA_RESOLUTION_LOW", meta.Files[1].Detail)
	require.Equal(t, types.FileTypeImage, meta.Files[2].FileType)
	require.Equal(t, meta.CombineText, req.GetTokenCountMeta().CombineText)
	after, _ := common.Marshal(req)
	require.Equal(t, before, after)
}

func TestGeminiEstimateMetaPreservesOtherProtocolMetadata(t *testing.T) {
	var chat GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"messages":[{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{\"x\":1}"}}]}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{"x":{"type":"integer"}}}}}]}`, &chat))
	original := chat.GetTokenCountMeta()
	meta := GeminiEstimateTokenMeta(&chat)
	require.Contains(t, meta.CombineText, `{"x":1}`)
	require.Contains(t, meta.CombineText, `"properties":{"x":{"type":"integer"}}`)
	require.NotContains(t, meta.CombineText, "map[")
	require.Equal(t, original, chat.GetTokenCountMeta())

	var responses OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"instructions":"system","tools":[{"type":"namespace","name":"fs","tools":[{"type":"custom","name":"patch","format":{"type":"grammar","definition":"grammar rules"}}]}],"input":[
	{"type":"custom_tool_call","name":"patch","input":"raw patch","call_id":"secret-id"},
	{"type":"custom_tool_call_output","call_id":"secret-id","output":[{"type":"input_text","text":"result"},{"type":"input_image","image_url":"data:image/png;base64,AAAA"},{"type":"input_file","file_data":"data:application/pdf;base64,BBBB"}]},
	{"type":"function_call_output","output":{"entries":[1,2]}},
	{"type":"reasoning","encrypted_content":"secret reasoning"}]}`, &responses))
	original = responses.GetTokenCountMeta()
	meta = GeminiEstimateTokenMeta(&responses)
	for _, value := range []string{"system", "raw patch", "patch", "grammar rules", "result", `{"entries":[1,2]}`} {
		require.Contains(t, meta.CombineText, value)
	}
	for _, value := range []string{"AAAA", "BBBB", "secret-id", "secret reasoning"} {
		require.NotContains(t, meta.CombineText, value)
	}
	require.Len(t, meta.Files, 2)
	require.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
	require.Equal(t, types.FileTypeFile, meta.Files[1].FileType)
	require.Equal(t, 1, strings.Count(meta.CombineText, "raw patch"))
	require.Equal(t, original, responses.GetTokenCountMeta())
}
