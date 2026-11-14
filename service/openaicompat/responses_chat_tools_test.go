package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestResponsesChatToolsAndOutputsRoundTrip(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{Model: "m", Tools: []byte(`[{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent","parameters":{"const":9007199254740993}}]},{"type":"custom","name":"exec","format":{"type":"text"}}]`), Input: []byte(`[{"type":"function_call","namespace":"collaboration","name":"spawn_agent","call_id":"one","arguments":"{}"},{"type":"function_call_output","call_id":"one","output":{"id":9007199254740993}},{"type":"custom_tool_call","name":"exec","call_id":"two","input":"echo hi"},{"type":"custom_tool_call_output","call_id":"two","output":"ok"}]`)}
	chat, err := ResponsesRequestToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Equal(t, "collaboration__spawn_agent", chat.Tools[0].Function.Name)
	require.Equal(t, "collaboration__spawn_agent", chat.Messages[0].ParseToolCalls()[0].Function.Name)
	require.Equal(t, `{"id":9007199254740993}`, chat.Messages[1].StringContent())
	require.JSONEq(t, `{"input":"echo hi"}`, chat.Messages[2].ParseToolCalls()[0].Function.Arguments)
	encoded, err := common.Marshal(chat)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "9007199254740993")
	require.NotContains(t, string(encoded), "ResponsesToolMapping")
	message := dto.Message{Role: "assistant"}
	message.SetToolCalls([]dto.ToolCallRequest{{ID: "call_result", Type: "function", Function: dto.FunctionRequest{Name: "exec", Arguments: `{"input":"run()"}`}}})
	response, _, err := ChatCompletionsResponseToResponsesResponseWithTools(&dto.OpenAITextResponse{Id: "test", Choices: []dto.OpenAITextResponseChoice{{Message: message}}}, chat.ResponsesToolMapping)
	require.NoError(t, err)
	require.Equal(t, "custom_tool_call", response.Output[1].Type)
	require.Equal(t, "run()", *response.Output[1].Input)
}
func TestResponsesChatRejectsStateAndConflictingTools(t *testing.T) {
	for _, raw := range []string{`{"model":"m","multi_agent":{"enabled":true}}`, `{"model":"m","previous_response_id":"r"}`, `{"model":"m","input":[{"type":"agent_message"}]}`, `{"model":"m","tools":[{"type":"function","name":"a__b"},{"type":"namespace","name":"a","tools":[{"type":"function","name":"b"}]}]}`} {
		var req dto.OpenAIResponsesRequest
		require.NoError(t, common.UnmarshalJsonStr(raw, &req))
		_, err := ResponsesRequestToChatCompletionsRequest(&req)
		require.Error(t, err)
	}
}
