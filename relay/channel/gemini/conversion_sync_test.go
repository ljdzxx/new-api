package gemini

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiSchemaUsesFullJSONOnV1Beta(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeGemini, UpstreamModelName: "gemini-2.5-flash"}}
	schema := map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"const": 3, "type": "integer"}}, "$defs": map[string]any{"count": map[string]any{"type": "integer"}}}
	decl := convertGeminiFunctionDeclaration(nil, info, dto.FunctionRequest{Name: "lookup", Parameters: schema})
	require.Nil(t, decl.Parameters)
	require.Equal(t, schema, decl.ParametersJsonSchema)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	request := dto.GeneralOpenAIRequest{
		Messages:       []dto.Message{{Role: "user", Content: "answer"}},
		ResponseFormat: &dto.ResponseFormat{Type: "json_schema", JsonSchema: []byte(`{"name":"result","schema":{"type":"object","properties":{"x":{"const":3,"type":"integer"}}}}`)},
	}
	got, err := CovertOpenAI2Gemini(c, request, info)
	require.NoError(t, err)
	require.Nil(t, got.GenerationConfig.ResponseSchema)
	require.Equal(t, float64(3), gjson.GetBytes(got.GenerationConfig.ResponseJsonSchema, "properties.x.const").Float())
}

func TestGeminiEmptyObjectConstraintsArePreserved(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeGemini, UpstreamModelName: "gemini-2.5-flash"}}
	for _, key := range []string{"minProperties", "maxProperties", "description"} {
		schema := map[string]any{"type": "object", "properties": map[string]any{}, key: 1}
		if key == "description" {
			schema[key] = "No arguments"
		}
		decl := convertGeminiFunctionDeclaration(nil, info, dto.FunctionRequest{Name: "lookup", Parameters: schema})
		require.Nil(t, decl.Parameters)
		require.Equal(t, schema, decl.ParametersJsonSchema)
	}
	decl := convertGeminiFunctionDeclaration(nil, info, dto.FunctionRequest{Name: "lookup", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}})
	require.Nil(t, decl.Parameters)
	require.Nil(t, decl.ParametersJsonSchema)
}

func TestGeminiV1SchemaDowngradeRemovesUnsupportedConstraints(t *testing.T) {
	settings := model_setting.GetGeminiSettings()
	old, existed := settings.VersionSettings["gemini-v1-fixture"]
	settings.VersionSettings["gemini-v1-fixture"] = "v1"
	t.Cleanup(func() {
		if existed {
			settings.VersionSettings["gemini-v1-fixture"] = old
		} else {
			delete(settings.VersionSettings, "gemini-v1-fixture")
		}
	})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeGemini, UpstreamModelName: "gemini-v1-fixture"}}
	schema := map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "integer", "enum": []any{1, 2}, "const": 1}}}
	decl := convertGeminiFunctionDeclaration(nil, info, dto.FunctionRequest{Name: "lookup", Parameters: schema})
	require.Nil(t, decl.ParametersJsonSchema)
	encoded, err := common.Marshal(decl.Parameters)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(encoded, "properties.x.enum").Exists())
	require.False(t, gjson.GetBytes(encoded, "properties.x.const").Exists())
}

func TestGeminiThinkingCapabilities(t *testing.T) {
	for _, tc := range []struct {
		model      string
		level      string
		budget     *int
		wantLevel  string
		wantBudget *int
	}{
		{"gemini-2.5-pro", "high", nil, "", common.GetPointer(24576)},
		{"gemini-2.5-pro", "xhigh", nil, "", common.GetPointer(32768)},
		{"gemini-2.5-flash-lite", "", common.GetPointer(0), "", common.GetPointer(0)},
		{"gemini-3-pro", "", common.GetPointer(0), "low", nil},
		{"gemini-3.1-flash-image", "LOW", nil, "high", nil},
	} {
		req := &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: tc.level, ThinkingBudget: tc.budget}}}
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tc.model}}
		require.NoError(t, normalizeGeminiThinkingConfig(req, info), tc.model)
		require.Equal(t, tc.wantLevel, req.GenerationConfig.ThinkingConfig.ThinkingLevel, tc.model)
		require.Equal(t, tc.wantBudget, req.GenerationConfig.ThinkingConfig.ThinkingBudget, tc.model)
	}
}

func TestGeminiToolIDsAndGroundingCitations(t *testing.T) {
	call := getResponseToolCall(&dto.GeminiPart{FunctionCall: &dto.FunctionCall{ID: common.GetPointer("call_123"), FunctionName: "lookup", Arguments: map[string]any{"city": "Tokyo"}}})
	require.Equal(t, "call_123", call.ID)
	var response dto.GeminiChatResponse
	require.NoError(t, common.UnmarshalJsonStr(`{"candidates":[{"content":{"role":"model","parts":[{"text":"天气很好"}]},"groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://example.com/weather","title":"Weather"}}],"groundingSupports":[{"segment":{"partIndex":0,"startIndex":0,"endIndex":6,"text":"天气"},"groundingChunkIndices":[0]}]}}]}`, &response))
	annotations := geminiGroundingAnnotations(response.Candidates[0].GroundingMetadata, response.Candidates[0].Content, "天气很好", nil)
	require.Len(t, annotations, 1)
	encoded, err := common.Marshal(annotations)
	require.NoError(t, err)
	require.EqualValues(t, 2, gjson.GetBytes(encoded, "0.url_citation.end_index").Int())
	require.Equal(t, "https://example.com/weather", gjson.GetBytes(encoded, "0.url_citation.url").String())
	seen := make(map[string]struct{})
	require.Len(t, geminiGroundingAnnotations(response.Candidates[0].GroundingMetadata, response.Candidates[0].Content, "天气很好", seen), 1)
	require.Empty(t, geminiGroundingAnnotations(response.Candidates[0].GroundingMetadata, response.Candidates[0].Content, "天气很好", seen))
}

func TestGeminiGroundingCounterDeduplicatesStreamFrames(t *testing.T) {
	response := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{GroundingMetadata: &dto.GeminiGroundingMetadata{WebSearchQueries: []string{" weather ", "weather", ""}, ImageSearchQueries: []string{"map"}}}}}
	var counter geminiGroundingCounter
	counter.observe(response)
	counter.observe(response)
	info := &relaycommon.RelayInfo{}
	counter.commit(info)
	require.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[relaycommon.GeminiGoogleSearchQueryTool].CallCount)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[relaycommon.GeminiGoogleSearchGroundedPromptTool].CallCount)
}
