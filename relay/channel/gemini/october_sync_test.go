package gemini

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestEmbeddingRejectsContentParts(t *testing.T) {
	for _, input := range []string{`["text",{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]`, `[{"type":"text","text":"hello"}]`, `[{"type":"video_url","video_url":{"url":"https://example.com/video.mp4"}}]`} {
		var req dto.EmbeddingRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"input":`+input+`}`, &req))
		_, _, info := responsesTestContext()
		_, err := (&Adaptor{}).ConvertEmbeddingRequest(nil, info, req)
		var apiErr *types.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
		require.True(t, types.IsSkipRetryError(apiErr))
	}
	_, _, info := responsesTestContext()
	got, err := (&Adaptor{}).ConvertEmbeddingRequest(nil, info, dto.EmbeddingRequest{Input: []any{"hello", "world"}})
	require.NoError(t, err)
	body, err := common.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, `["hello","world"]`, gjson.GetBytes(body, "requests.#.content.parts.0.text").Raw)
}

func TestChatGeminiPreservesExplicitZero(t *testing.T) {
	for _, params := range []string{"", `,"temperature":0,"top_p":0,"seed":0,"max_tokens":0`, `,"temperature":0,"top_p":0,"seed":0,"max_completion_tokens":0`} {
		var req dto.GeneralOpenAIRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"messages":[{"role":"user","content":"hello"}]`+params+`}`, &req))
		c, _, info := responsesTestContext()
		got, err := CovertOpenAI2Gemini(c, req, info)
		require.NoError(t, err)
		body, err := common.Marshal(got)
		require.NoError(t, err)
		for _, field := range []string{"temperature", "topP", "seed", "maxOutputTokens"} {
			value := gjson.GetBytes(body, "generationConfig."+field)
			require.Equal(t, params != "", value.Exists(), field)
			if params != "" {
				require.Equal(t, "0", value.Raw, field)
			}
		}
	}
}

func TestResponsesPartialToolCallsEstimateOnce(t *testing.T) {
	var counts []int
	for _, frames := range [][]string{
		{`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"x":"hello world"}}}]},"finishReason":"STOP"}]}`},
		{
			`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","partialArgs":[{"jsonPath":"$.x","stringValue":"hello "}],"willContinue":true}}]}}]}`,
			`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"x":"hello world"},"willContinue":false}}]},"finishReason":"STOP"}]}`,
		},
	} {
		c, recorder, info := responsesTestContext()
		info.IsStream = true
		usage, err := GeminiResponsesStreamHandler(c, info, geminiSSE(frames...))
		require.Nil(t, err, recorder.Body.String())
		require.Positive(t, usage.CompletionTokens)
		require.True(t, usage.BillingUsage.Estimated)
		counts = append(counts, usage.CompletionTokens)
	}
	require.Equal(t, counts[0], counts[1], "fragmentation must not change estimated output tokens")
}
