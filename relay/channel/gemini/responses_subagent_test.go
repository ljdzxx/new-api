package gemini

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiResponsesCollaborationContinuation(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/stream=%t", channelType, stream), func(t *testing.T) {
				c, recorder, info := responsesTestContext()
				info.ChannelType, info.IsStream = channelType, stream
				var req dto.OpenAIResponsesRequest
				require.NoError(t, common.UnmarshalJsonStr(`{
					"model":"gemini-3-flash-preview",
					"tools":[{"type":"namespace","name":"collaboration","tools":[
						{"type":"function","name":"spawn_agent","parameters":{"type":"object"}},
						{"type":"function","name":"followup_task","parameters":{"type":"object"}}
					]}],
					"input":[
						{"role":"user","content":"Delegate and then continue the same worker"},
						{"type":"function_call","namespace":"collaboration","name":"spawn_agent","call_id":"call_spawn","arguments":"{\"task_name\":\"worker\",\"message\":\"inspect\"}"},
						{"type":"function_call_output","call_id":"call_spawn","output":"{\"task_name\":\"/root/worker\"}"}
					]
				}`, &req))
				converted, err := ConvertResponsesToGemini(c, info, req)
				require.NoError(t, err)
				encoded, err := common.Marshal(converted)
				require.NoError(t, err)
				require.Equal(t, "collaboration__spawn_agent", gjson.GetBytes(encoded, "tools.0.functionDeclarations.0.name").String())
				require.Equal(t, "collaboration__followup_task", gjson.GetBytes(encoded, "tools.0.functionDeclarations.1.name").String())
				require.Equal(t, "collaboration__spawn_agent", converted.Contents[1].Parts[0].FunctionCall.FunctionName)
				require.Equal(t, "collaboration__spawn_agent", converted.Contents[2].Parts[0].FunctionResponse.Name)
				require.Contains(t, string(encoded), "/root/worker")
				payload := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call_followup","name":"collaboration__followup_task","args":{"target":"worker","message":"verify again"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`
				response := geminiSSE(payload)
				if !stream {
					response = &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}
				}
				_, apiErr := (&Adaptor{}).DoResponse(c, response, info)
				require.Nil(t, apiErr)
				result := gjson.Parse(recorder.Body.String())
				if stream {
					events := responseEvents(t, recorder.Body.String())
					result = events[len(events)-1].Get("response")
				}
				require.Equal(t, "collaboration", result.Get("output.0.namespace").String())
				require.Equal(t, "followup_task", result.Get("output.0.name").String())
				require.Equal(t, "call_followup", result.Get("output.0.call_id").String())
				require.JSONEq(t, `{"target":"worker","message":"verify again"}`, result.Get("output.0.arguments").String())
			})
		}
	}
}
