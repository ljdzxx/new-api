package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeneralOpenAIRequestPreserveExplicitZeroValues(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-4.1",
		"stream":false,
		"max_tokens":0,
		"max_completion_tokens":0,
		"top_p":0,
		"top_k":0,
		"n":0,
		"frequency_penalty":0,
		"presence_penalty":0,
		"seed":0,
		"logprobs":false,
		"top_logprobs":0,
		"dimensions":0,
		"return_images":false,
		"return_related_questions":false
	}`)

	var req GeneralOpenAIRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_completion_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_p").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_k").Exists())
	require.True(t, gjson.GetBytes(encoded, "n").Exists())
	require.True(t, gjson.GetBytes(encoded, "frequency_penalty").Exists())
	require.True(t, gjson.GetBytes(encoded, "presence_penalty").Exists())
	require.True(t, gjson.GetBytes(encoded, "seed").Exists())
	require.True(t, gjson.GetBytes(encoded, "logprobs").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_logprobs").Exists())
	require.True(t, gjson.GetBytes(encoded, "dimensions").Exists())
	require.True(t, gjson.GetBytes(encoded, "return_images").Exists())
	require.True(t, gjson.GetBytes(encoded, "return_related_questions").Exists())
}

func TestOpenAIResponsesRequestPreserveExplicitZeroValues(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-4.1",
		"max_output_tokens":0,
		"max_tool_calls":0,
		"stream":false,
		"top_p":0
	}`)

	var req OpenAIResponsesRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "max_output_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_tool_calls").Exists())
	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_p").Exists())
}

func TestOpenAIResponsesRequestPreservesCodexFields(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.6-sol",
		"client_metadata":{"session_id":"session-1"},
		"reasoning":{"effort":"high","mode":"auto","context":{"turn":2}}
	}`)

	var req OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal(raw, &req))

	encoded, err := common.Marshal(req)
	require.NoError(t, err)
	require.Equal(t, "session-1", gjson.GetBytes(encoded, "client_metadata.session_id").String())
	require.Equal(t, "auto", gjson.GetBytes(encoded, "reasoning.mode").String())
	require.Equal(t, int64(2), gjson.GetBytes(encoded, "reasoning.context.turn").Int())
}

func TestOpenAIResponsesCompactionRequestPreservesCodexFields(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.6-sol",
		"tools":[],
		"parallel_tool_calls":false,
		"reasoning":{"mode":"auto","context":{}},
		"service_tier":"default",
		"prompt_cache_key":"cache-1",
		"prompt_cache_options":{"type":"ephemeral"},
		"prompt_cache_retention":"24h",
		"text":{"format":{"type":"text"}}
	}`)

	var req OpenAIResponsesCompactionRequest
	require.NoError(t, common.Unmarshal(raw, &req))

	encoded, err := common.Marshal(req)
	require.NoError(t, err)
	for _, path := range []string{"tools", "parallel_tool_calls", "reasoning.mode", "reasoning.context", "service_tier", "prompt_cache_key", "prompt_cache_options.type", "prompt_cache_retention", "text.format.type"} {
		require.True(t, gjson.GetBytes(encoded, path).Exists(), path)
	}
}
