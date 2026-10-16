package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// TestGeminiConversionPreservesCallIDsAndJSONSchema 确认调用与结果的 ID 匹配，
// 并保留 Gemini 参数 JSON Schema 中的约束。
func TestGeminiConversionPreservesCallIDsAndJSONSchema(t *testing.T) {
	id, _ := common.Marshal("call_123")
	request := &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{
		{Role: "model", Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{ID: common.GetPointer("call_123"), FunctionName: "lookup", Arguments: map[string]any{"city": "Tokyo"}}}}},
		{Role: "user", Parts: []dto.GeminiPart{{FunctionResponse: &dto.GeminiFunctionResponse{ID: id, Name: "lookup", Response: map[string]any{"ok": true}}}}},
	}}
	request.SetTools([]dto.GeminiChatTool{{FunctionDeclarations: []dto.GeminiFunctionDeclaration{{Name: "lookup", ParametersJsonSchema: map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"const": "Tokyo"}}}}}}})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-flash"}}
	converted, err := GeminiToOpenAIRequest(request, info)
	require.NoError(t, err)
	require.Len(t, converted.Messages, 2)
	require.Equal(t, "call_123", converted.Messages[0].ParseToolCalls()[0].ID)
	require.Equal(t, "call_123", converted.Messages[1].ToolCallId)
	require.Len(t, converted.Tools, 1)
	parameters := converted.Tools[0].Function.Parameters.(map[string]any)
	require.Equal(t, "Tokyo", parameters["properties"].(map[string]any)["city"].(map[string]any)["const"])
}
