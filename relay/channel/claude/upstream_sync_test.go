package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeHeadersPreserveEveryBetaValue(t *testing.T) {
	for _, values := range [][]string{nil, {"first,second"}, {"first,second", "third"}} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
		for _, value := range values {
			ctx.Request.Header.Add("anthropic-beta", value)
		}
		headers := make(http.Header)
		CommonClaudeHeadersOperation(ctx, &headers, &relaycommon.RelayInfo{OriginModelName: "claude-test"})
		switch len(values) {
		case 0:
			require.Empty(t, headers.Get("anthropic-beta"))
		case 1:
			require.Equal(t, "first,second", headers.Get("anthropic-beta"))
		case 2:
			require.Equal(t, "first,second,third", headers.Get("anthropic-beta"))
		}
	}
}

func TestClaudeStreamCacheOnlyAndMissingUsage(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, tc := range []struct {
			name, usage, text     string
			done                  bool
			wantInput, wantOutput int
		}{
			{"cached refusal", `{"input_tokens":0,"cache_read_input_tokens":123,"output_tokens":0}`, "", true, 0, 0},
			{"cache write refusal", `{"input_tokens":0,"cache_creation_input_tokens":123,"output_tokens":0}`, "", true, 0, 0},
			{"cached interrupted", `{"input_tokens":0,"cache_read_input_tokens":123,"output_tokens":0}`, "some output", false, 0, -1},
			{"missing input with output", `{"input_tokens":0,"output_tokens":7}`, "some output", true, 1000, 7},
			{"missing output", `{"input_tokens":25,"output_tokens":0}`, "some output", true, 25, -1},
		} {
			t.Run(tc.name+common.Interface2String(passthrough), func(t *testing.T) {
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
				info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-4"}, RelayFormat: types.RelayFormatClaude}
				info.SetEstimatePromptTokens(1000)
				state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
				data := `{"type":"message_start","message":{"id":"msg_test","model":"claude-sonnet-4","usage":` + tc.usage + `}}`
				if passthrough {
					updateClaudePassThroughUsage(ctx, info, state, []byte("data: "+data))
				} else {
					require.Nil(t, HandleStreamResponseData(ctx, info, state, data))
				}
				state.Done = tc.done
				state.ResponseText.WriteString(tc.text)
				FinalizeStreamUsage(ctx, info, state)
				require.Equal(t, tc.wantInput, state.Usage.PromptTokens)
				if tc.wantOutput < 0 {
					require.Positive(t, state.Usage.CompletionTokens)
				} else {
					require.Equal(t, tc.wantOutput, state.Usage.CompletionTokens)
				}
				if gjson.Get(tc.usage, "cache_read_input_tokens").Exists() {
					require.Equal(t, 123, state.Usage.PromptTokensDetails.CachedTokens)
				}
				if gjson.Get(tc.usage, "cache_creation_input_tokens").Exists() {
					require.Equal(t, 123, state.Usage.PromptTokensDetails.CacheWriteTokens)
				}
			})
		}
	}
}

func TestOpenAIToClaudePreservesSchemaAndToolNumbers(t *testing.T) {
	const schema = `{"type":"object","properties":{"reasoning":{"type":"string"},"answer":{"type":"integer"}},"required":["reasoning","answer"]}`
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"claude-sonnet-4","tools":[{"type":"function","function":{"name":"f","parameters":`+schema+`}}],"messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"f","arguments":"{\"id\":9007199254740993,\"nested\":[18446744073709551615]}"}}]}]}`), &request))
	converted, err := RequestOpenAI2ClaudeMessage(nil, request)
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	require.Equal(t, schema, gjson.GetBytes(encoded, "tools.0.input_schema").Raw)
	require.Equal(t, "9007199254740993", gjson.GetBytes(encoded, `messages.#(role=="assistant").content.#(type=="tool_use").input.id`).Raw)
	require.Equal(t, "18446744073709551615", gjson.GetBytes(encoded, `messages.#(role=="assistant").content.#(type=="tool_use").input.nested.0`).Raw)
	filtered, err := relaycommon.RemoveDisabledFields(encoded, dto.ChannelOtherSettings{}, false)
	require.NoError(t, err)
	require.Equal(t, schema, gjson.GetBytes(filtered, "tools.0.input_schema").Raw)
}

func TestClaudeResponseToOpenAIPreservesToolNumbers(t *testing.T) {
	var response dto.ClaudeResponse
	require.NoError(t, common.Unmarshal([]byte(`{"id":"msg_1","content":[{"type":"tool_use","id":"call_1","name":"f","input":{"id":9007199254740993,"nested":[18446744073709551615]}}],"stop_reason":"tool_use"}`), &response))
	converted := ResponseClaude2OpenAI(&response)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	args := gjson.GetBytes(encoded, "choices.0.message.tool_calls.0.function.arguments").String()
	require.Equal(t, "9007199254740993", gjson.Get(args, "id").Raw)
	require.Equal(t, "18446744073709551615", gjson.Get(args, "nested.0").Raw)
}
