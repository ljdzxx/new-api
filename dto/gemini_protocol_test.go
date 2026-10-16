package dto

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiStreamAction(t *testing.T) {
	request := &GeminiChatRequest{}
	require.False(t, request.IsStream(nil))
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"/v1beta/models/gemini-2.5-flash:streamGenerateContent", true},
		{"/v1beta/models/gemini-2.5-flash:generateContent?alt=sse", true},
		{"/v1beta/models/gemini-2.5-flash:generateContent", false},
		{"/v1beta/models/streamGenerateContent-alias:generateContent", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", tc.url, nil)
			require.Equal(t, tc.want, request.IsStream(c))
		})
	}
}

func TestGeminiNewProtocolFieldsRoundTrip(t *testing.T) {
	body := []byte("{\"contents\":[{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"id\":\"\",\"name\":\"lookup\",\"args\":{},\"partialArgs\":[{\"jsonPath\":\"$.count\",\"numberValue\":0,\"stringValue\":\"\",\"boolValue\":false,\"nullValue\":null,\"willContinue\":false}],\"willContinue\":false}}]}],\"toolConfig\":{\"includeServerSideToolInvocations\":false,\"functionCallingConfig\":{\"streamFunctionCallArguments\":false}},\"generationConfig\":{\"thinkingConfig\":{\"includeThoughts\":false,\"thinkingBudget\":0}}}")
	var request GeminiChatRequest
	require.NoError(t, common.Unmarshal(body, &request))
	encoded, err := common.Marshal(request)
	require.NoError(t, err)
	for _, path := range []string{
		"toolConfig.includeServerSideToolInvocations",
		"toolConfig.functionCallingConfig.streamFunctionCallArguments",
		"generationConfig.thinkingConfig.includeThoughts",
		"contents.0.parts.0.functionCall.willContinue",
		"contents.0.parts.0.functionCall.partialArgs.0.boolValue",
		"contents.0.parts.0.functionCall.partialArgs.0.willContinue",
	} {
		require.True(t, gjson.GetBytes(encoded, path).Exists(), path)
		require.False(t, gjson.GetBytes(encoded, path).Bool(), path)
	}
	require.Equal(t, "0", gjson.GetBytes(encoded, "contents.0.parts.0.functionCall.partialArgs.0.numberValue").Raw)
	require.Equal(t, "\"\"", gjson.GetBytes(encoded, "contents.0.parts.0.functionCall.id").Raw)
	require.Equal(t, "\"\"", gjson.GetBytes(encoded, "contents.0.parts.0.functionCall.partialArgs.0.stringValue").Raw)
	require.Equal(t, "null", gjson.GetBytes(encoded, "contents.0.parts.0.functionCall.partialArgs.0.nullValue").Raw)
	require.Equal(t, "0", gjson.GetBytes(encoded, "generationConfig.thinkingConfig.thinkingBudget").Raw)

	for _, body := range []string{"{}", "{\"include_thoughts\":false}"} {
		var thinking GeminiThinkingConfig
		require.NoError(t, common.UnmarshalJsonStr(body, &thinking))
		encoded, err := common.Marshal(thinking)
		require.NoError(t, err)
		require.Equal(t, body != "{}", gjson.GetBytes(encoded, "includeThoughts").Exists())
	}
}

func TestGeminiToolsRoundTrip(t *testing.T) {
	var request GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr("{\"tools\":[{\"googleMaps\":{},\"enterpriseWebSearch\":{},\"fileSearch\":{\"fileSearchStoreNames\":[\"store\"]},\"computerUse\":{},\"retrieval\":{}}]}", &request))
	request.SetTools(request.GetTools())
	for _, name := range []string{"googleMaps", "enterpriseWebSearch", "fileSearch", "computerUse", "retrieval"} {
		require.True(t, gjson.GetBytes(request.Tools, "0."+name).Exists(), name)
	}
}

func TestGeminiUsagePresenceAndGrounding(t *testing.T) {
	var response GeminiChatResponse
	require.NoError(t, common.UnmarshalJsonStr("{\"candidates\":[{\"groundingMetadata\":{\"webSearchQueries\":[\"weather\"],\"imageSearchQueries\":[\"map\"],\"groundingChunks\":[{\"web\":{\"uri\":\"https://example.com\"}}]}}],\"usageMetadata\":{}}", &response))
	require.True(t, response.HasUsageMetadata)
	require.NotNil(t, response.GetUsageMetadata())
	require.Equal(t, []string{"weather"}, response.Candidates[0].GroundingMetadata.WebSearchQueries)
	encoded, err := common.Marshal(response)
	require.NoError(t, err)
	require.Equal(t, "https://example.com", gjson.GetBytes(encoded, "candidates.0.groundingMetadata.groundingChunks.0.web.uri").String())
	require.NoError(t, common.UnmarshalJsonStr("{}", &response))
	require.False(t, response.HasUsageMetadata)
	require.Nil(t, response.GetUsageMetadata())
	response.UsageMetadata.PromptTokenCount = 10
	require.NotNil(t, response.GetUsageMetadata())
}
