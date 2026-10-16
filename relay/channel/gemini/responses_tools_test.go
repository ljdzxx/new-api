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

func TestGeminiResponsesNamespacesRoundTrip(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/stream=%t", channelType, stream), func(t *testing.T) {
				c, recorder, info := responsesTestContext()
				info.ChannelType = channelType
				info.IsStream = stream
				var req dto.OpenAIResponsesRequest
				require.NoError(t, common.UnmarshalJsonStr(`{
					"model":"gemini-3-flash-preview",
					"tools":[
						{"type":"namespace","name":"functions","tools":[{"type":"function","name":"run","parameters":{"type":"object"}}]},
						{"type":"namespace","name":"fs","description":"Filesystem operations","tools":[{"type":"function","name":"run","parameters":{"type":"object","properties":{"enabled":{"type":"boolean"}},"additionalProperties":false}},{"type":"custom","name":"patch"}]},
						{"type":"namespace","name":"db","tools":[{"type":"function","name":"run","parameters":{"type":"object"}}]},
						{"type":"function","name":"ordinary__name","parameters":{"type":"object"}}
					],
					"tool_choice":{"type":"custom","namespace":"fs","name":"patch"},
					"input":[
						{"role":"user","content":"run tools"},
						{"type":"function_call","namespace":"db","name":"run","call_id":"a","arguments":"{}"},
						{"type":"function_call_output","call_id":"a","output":"done"},
						{"type":"custom_tool_call","namespace":"fs","name":"patch","call_id":"b","input":"old patch"},
						{"type":"custom_tool_call_output","namespace":"fs","name":"patch","call_id":"b","output":"done"}
					]
				}`, &req))
				before, err := common.Marshal(req)
				require.NoError(t, err)
				converted, err := ConvertResponsesToGemini(c, info, req)
				require.NoError(t, err)
				encoded, err := common.Marshal(converted)
				require.NoError(t, err)
				declarations := gjson.GetBytes(encoded, "tools.0.functionDeclarations").Array()
				require.Len(t, declarations, 5)
				var names []string
				for _, d := range declarations {
					names = append(names, d.Get("name").String())
				}
				require.Equal(t, []string{"run", "fs__run", "fs__patch", "db__run", "ordinary__name"}, names)
				require.Contains(t, declarations[1].Get("description").String(), "Filesystem operations")
				require.True(t, declarations[1].Get("parametersJsonSchema.additionalProperties").Exists())
				require.Equal(t, []string{"fs__patch"}, converted.ToolConfig.FunctionCallingConfig.AllowedFunctionNames)
				require.Equal(t, "db__run", converted.Contents[1].Parts[0].FunctionCall.FunctionName)
				require.Equal(t, "db__run", converted.Contents[2].Parts[0].FunctionResponse.Name)
				require.Equal(t, "fs__patch", converted.Contents[3].Parts[0].FunctionCall.FunctionName)
				require.Equal(t, "fs__patch", converted.Contents[4].Parts[0].FunctionResponse.Name)
				after, _ := common.Marshal(req)
				require.Equal(t, before, after)
				payload := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"one","name":"fs__run","args":{"enabled":false}}},{"functionCall":{"id":"two","name":"db__run","args":{}}},{"functionCall":{"id":"three","name":"fs__patch","args":{"input":"new patch"}}},{"functionCall":{"id":"four","name":"run","args":{}}},{"functionCall":{"id":"five","name":"ordinary__name","args":{}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`
				response := geminiSSE(payload)
				if !stream {
					response = &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}
				}
				_, apiErr := (&Adaptor{}).DoResponse(c, response, info)
				require.Nil(t, apiErr)
				result := gjson.Parse(recorder.Body.String())
				if stream {
					events := responseEvents(t, recorder.Body.String())
					result = events[len(events)-1].Get("response")
					for _, event := range events {
						if event.Get("type").String() == "response.output_item.added" || event.Get("type").String() == "response.output_item.done" {
							if event.Get("output_index").Int() == 0 {
								require.Equal(t, "fs", event.Get("item.namespace").String())
								require.Equal(t, "run", event.Get("item.name").String())
							}
						}
					}
				}
				output := result.Get("output").Array()
				require.Len(t, output, 5)
				require.Equal(t, "fs", output[0].Get("namespace").String())
				require.Equal(t, "run", output[0].Get("name").String())
				require.Equal(t, "one", output[0].Get("call_id").String())
				require.Equal(t, "db", output[1].Get("namespace").String())
				require.Equal(t, "run", output[1].Get("name").String())
				require.Equal(t, "fs", output[2].Get("namespace").String())
				require.Equal(t, "patch", output[2].Get("name").String())
				require.Equal(t, "custom_tool_call", output[2].Get("type").String())
				require.Equal(t, "new patch", output[2].Get("input").String())
				require.False(t, output[3].Get("namespace").Exists())
				require.Equal(t, "run", output[3].Get("name").String())
				require.False(t, output[4].Get("namespace").Exists())
				require.Equal(t, "ordinary__name", output[4].Get("name").String())
			})
		}
	}
}

func TestGeminiResponsesNamespaceAliases(t *testing.T) {
	names := make(responsesToolNames)
	namespace := strings.Repeat("long_namespace", 8)
	alias, err := names.register(namespace, "execute")
	require.NoError(t, err)
	require.LessOrEqual(t, len(alias), 64)
	require.Equal(t, responsesToolName{Namespace: namespace, Name: "execute"}, names[alias])
	again, err := make(responsesToolNames).register(namespace, "execute")
	require.NoError(t, err)
	require.Equal(t, alias, again)
	_, err = names.register("fs", "read")
	require.NoError(t, err)
	_, err = names.register("", "fs__read")
	require.ErrorContains(t, err, "collide")
}

func TestGeminiResponsesNamespaceValidation(t *testing.T) {
	for _, tools := range []string{
		`[{"type":"namespace","tools":[]}]`,
		`[{"type":"namespace","name":"fs","tools":{}}]`,
		`[{"type":"namespace","name":"fs","tools":[null]}]`,
		`[{"type":"namespace","name":"fs","tools":[{"type":"function"}]}]`,
	} {
		t.Run(tools, func(t *testing.T) {
			c, _, info := responsesTestContext()
			var req dto.OpenAIResponsesRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-3-flash-preview","input":"hi","tools":`+tools+`}`, &req))
			_, err := ConvertResponsesToGemini(c, info, req)
			require.Error(t, err)
		})
	}
}
