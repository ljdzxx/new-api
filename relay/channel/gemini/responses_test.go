package gemini

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func responsesTestContext() (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses, OriginModelName: "gemini-3-flash-preview", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeGemini, UpstreamModelName: "gemini-3-flash-preview"}}
	info.SetEstimatePromptTokens(10)
	info.PriceData.GlobalModelRatio = 1
	return c, recorder, info
}

func TestGeminiResponsesRequest(t *testing.T) {
	c, _, info := responsesTestContext()
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-3-flash-preview","instructions":"System","temperature":0,"top_p":0,"max_output_tokens":0,"reasoning":{"effort":"low"},"text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object","properties":{"ok":{"type":"boolean"}},"additionalProperties":false}}},"tools":[{"type":"custom","name":"patch","description":"edit"},{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"x":{"type":"integer"}},"additionalProperties":false}},{"type":"web_search"}],"tool_choice":{"type":"custom","name":"patch"},"input":[{"role":"developer","content":"Developer"},{"role":"user","content":"Go"},{"type":"custom_tool_call","call_id":"call_1","name":"patch","input":"raw patch"},{"type":"custom_tool_call_output","call_id":"call_1","output":"done"},{"type":"function_call","call_id":"call_2","name":"lookup","arguments":"{\"x\":0}"},{"type":"function_call_output","call_id":"call_2","output":{"ok":false}}]}`, &req))
	before, _ := common.Marshal(req)
	got, err := ConvertResponsesToGemini(c, info, req)
	require.NoError(t, err)
	encoded, err := common.Marshal(got)
	require.NoError(t, err)
	for _, field := range []string{"temperature", "topP", "maxOutputTokens"} {
		require.True(t, gjson.GetBytes(encoded, "generationConfig."+field).Exists())
		require.Zero(t, gjson.GetBytes(encoded, "generationConfig."+field).Int())
	}
	require.Contains(t, string(encoded), "additionalProperties")
	require.Equal(t, "low", got.GenerationConfig.ThinkingConfig.ThinkingLevel)
	require.Equal(t, "System\nDeveloper", got.SystemInstructions.Parts[0].Text)
	require.Equal(t, []string{"patch"}, got.ToolConfig.FunctionCallingConfig.AllowedFunctionNames)
	require.Equal(t, "call_1", *got.Contents[1].Parts[0].FunctionCall.ID)
	require.Equal(t, map[string]any{"input": "raw patch"}, got.Contents[1].Parts[0].FunctionCall.Arguments)
	require.Equal(t, "patch", got.Contents[2].Parts[0].FunctionResponse.Name)
	require.JSONEq(t, `"call_1"`, string(got.Contents[2].Parts[0].FunctionResponse.ID))
	require.Contains(t, string(got.Tools), "googleSearch")
	after, _ := common.Marshal(req)
	require.Equal(t, before, after)
}

func TestGeminiResponsesThinkingAndToolChoiceNone(t *testing.T) {
	c, _, info := responsesTestContext()
	info.UpstreamModelName = "gemini-2.5-flash"
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-2.5-flash","input":"hi","reasoning":{"effort":"low","summary":"none"},"tools":[{"type":"web_search"}],"tool_choice":"none"}`, &req))
	got, err := ConvertResponsesToGemini(c, info, req)
	require.NoError(t, err)
	require.Equal(t, 1024, *got.GenerationConfig.ThinkingConfig.ThinkingBudget)
	require.False(t, *got.GenerationConfig.ThinkingConfig.IncludeThoughts)
	require.Empty(t, got.Tools)
	require.Nil(t, got.ToolConfig)
	request := dto.OpenAIResponsesRequest{Model: info.UpstreamModelName, Input: []byte(`"hi"`)}
	got, err = ConvertResponsesToGemini(c, info, request)
	require.NoError(t, err)
	require.Nil(t, got.GenerationConfig.TopP)
	require.Nil(t, got.GenerationConfig.MaxOutputTokens)
	require.Nil(t, got.GenerationConfig.Temperature)
}

func TestGeminiResponsesToolMedia(t *testing.T) {
	for _, model := range []string{"gemini-2.5-flash", "gemini-3-flash-preview"} {
		t.Run(model, func(t *testing.T) {
			c, _, info := responsesTestContext()
			info.UpstreamModelName = model
			var req dto.OpenAIResponsesRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"`+model+`","input":[{"type":"function_call","call_id":"a","name":"look","arguments":"{}"},{"type":"function_call_output","call_id":"a","output":[{"type":"input_text","text":"document"},{"type":"input_file","file_data":"data:application/pdf;base64,JVBERi0xLjQK"}]},{"type":"function_call_output","call_id":"b","name":"other","output":"ok"}]}`, &req))
			got, err := ConvertResponsesToGemini(c, info, req)
			require.NoError(t, err)
			parts := got.Contents[1].Parts
			require.Equal(t, "look", parts[0].FunctionResponse.Name)
			require.Equal(t, "other", parts[1].FunctionResponse.Name)
			require.NotContains(t, parts[0].FunctionResponse.Response, "JVBER")
			if strings.HasPrefix(model, "gemini-2.") {
				require.Len(t, parts, 3)
				require.Equal(t, "application/pdf", parts[2].InlineData.MimeType)
			} else {
				require.Len(t, parts, 2)
				require.Contains(t, string(parts[0].FunctionResponse.Parts), "JVBERi0xLjQK")
			}
		})
	}
}

func TestGeminiResponsesToolOutputDataArray(t *testing.T) {
	c, _, info := responsesTestContext()
	data := []any{map[string]any{"type": "record", "value": false}}
	plan := &responsesRequestPlan{c: c, info: info, policy: types.ConversionLossPolicyAllow}
	output, media, err := plan.toolOutput(data, true)
	require.NoError(t, err)
	require.Empty(t, media)
	require.Equal(t, data, output["result"])
}

func TestGeminiResponsesRejectsUnsupportedInput(t *testing.T) {
	for _, body := range []string{
		`{"input":"hi","previous_response_id":"resp_old"}`,
		`{"input":[{"type":"item_reference","id":"old"}]}`,
		`{"input":[{"role":"user","content":[{"type":"input_file","file_id":"file_x"}]}]}`,
		`{"input":[{"type":"function_call","call_id":"a","name":"f","arguments":"[]"}]}`,
		`{"input":[{"type":"function_call_output","call_id":"a","output":"x"}]}`,
		`{"input":42}`,
	} {
		t.Run(body, func(t *testing.T) {
			c, _, info := responsesTestContext()
			var req dto.OpenAIResponsesRequest
			require.NoError(t, common.UnmarshalJsonStr(body, &req))
			req.Model = info.UpstreamModelName
			_, err := ConvertResponsesToGemini(c, info, req)
			require.Error(t, err)
		})
	}
}

func responseEvents(t *testing.T, body string) []gjson.Result {
	t.Helper()
	var events []gjson.Result
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: ") {
			value := strings.TrimPrefix(line, "data: ")
			require.True(t, gjson.Valid(value), value)
			event := gjson.Parse(value)
			if event.Get("type").String() == "response.keepalive" {
				continue
			}
			require.EqualValues(t, len(events), event.Get("sequence_number").Int())
			events = append(events, event)
		}
	}
	return events
}

func geminiSSE(frames ...string) *http.Response {
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}
}

func TestGeminiResponsesStreamLifecycleAndUsage(t *testing.T) {
	c, recorder, info := responsesTestContext()
	info.IsStream = true
	c.Set(geminiResponsesCustomToolsKey, map[string]bool{"patch": true})
	resp := geminiSSE(
		`{"candidates":[{"content":{"parts":[{"text":"think","thought":true}]}}],"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":30}}`,
		`{"candidates":[{"content":{"parts":[{"text":"你好"}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call_a","name":"patch","partialArgs":[{"jsonPath":"$.input","stringValue":"hello "}],"willContinue":true}}]}}]}`,
		`{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call_a","name":"patch","partialArgs":[{"jsonPath":"$.input","stringValue":"world"}],"willContinue":false}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":8,"thoughtsTokenCount":2,"totalTokenCount":110}}`,
		`{"candidates":[{"groundingMetadata":{"webSearchQueries":["q","q"],"groundingChunks":[{"web":{"uri":"https://example.com","title":"source"}}],"groundingSupports":[{"segment":{"startIndex":0,"endIndex":6,"text":"你好"},"groundingChunkIndices":[0]}]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":8,"thoughtsTokenCount":2,"totalTokenCount":110}}`,
	)
	u, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
	require.Nil(t, apiErr, recorder.Body.String())
	usage := u.(*dto.Usage)
	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, 10, usage.CompletionTokens)
	require.Equal(t, 30, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, dto.BillingUsageSourceGeminiChat, usage.BillingUsage.Source)
	events := responseEvents(t, recorder.Body.String())
	require.Equal(t, "response.created", events[0].Get("type").String())
	require.Equal(t, "response.in_progress", events[1].Get("type").String())
	last := events[len(events)-1]
	require.Equal(t, "response.completed", last.Get("type").String())
	output := last.Get("response.output").Array()
	require.Len(t, output, 4)
	require.Equal(t, "reasoning", output[0].Get("type").String())
	require.Equal(t, "think", output[0].Get("summary.0.text").String())
	require.Equal(t, "你好", output[1].Get("content.0.text").String())
	require.EqualValues(t, 2, output[1].Get("content.0.annotations.0.end_index").Int())
	require.Equal(t, "https://example.com", output[1].Get("content.0.annotations.0.url").String())
	require.Equal(t, "custom_tool_call", output[2].Get("type").String())
	require.Equal(t, "call_a", output[2].Get("call_id").String())
	require.Equal(t, "hello world", output[2].Get("input").String())
	require.Equal(t, "web_search_call", output[3].Get("type").String())
	require.Len(t, output[3].Get("action.queries").Array(), 1)
	require.EqualValues(t, 110, last.Get("response.usage.total_tokens").Int())
	require.EqualValues(t, 2, last.Get("response.usage.output_tokens_details.reasoning_tokens").Int())
	require.NotContains(t, recorder.Body.String(), "[DONE]")
	added, done := map[int64]string{}, map[int64]string{}
	for _, e := range events {
		switch e.Get("type").String() {
		case "response.output_item.added":
			added[e.Get("output_index").Int()] = e.Get("item.id").String()
		case "response.output_item.done":
			done[e.Get("output_index").Int()] = e.Get("item.id").String()
		}
	}
	require.Equal(t, added, done)
}

func TestGeminiResponsesStreamFailures(t *testing.T) {
	for _, test := range []struct{ name, tail string }{
		{"truncated", `{"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120}}`},
		{"malformed", `{broken`},
		{"upstream_error", `{"error":{"message":"upstream broke"}}`},
		{"partial_tool", `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"f","partialArgs":[{"jsonPath":"$.x","stringValue":"part"}],"willContinue":true}}]},"finishReason":"STOP"}]}`},
		{"bad_path", `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"f","partialArgs":[{"jsonPath":"$.x[999999999]","stringValue":"part"}],"willContinue":false}}]},"finishReason":"STOP"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, recorder, info := responsesTestContext()
			info.IsStream = true
			usage, err := GeminiResponsesStreamHandler(c, info, geminiSSE(`{"candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120}}`, test.tail))
			require.NotNil(t, err)
			require.True(t, types.IsSkipRetryError(err))
			require.Equal(t, 100, usage.PromptTokens)
			require.Equal(t, 20, usage.CompletionTokens)
			require.Contains(t, recorder.Body.String(), "event: response.failed\n")
			require.NotContains(t, recorder.Body.String(), "response.completed")
			require.NoError(t, helper.WriteResponsesStreamFailure(c, err))
			require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed\n"))
		})
	}
}

func TestGeminiResponsesMissingUsageFailureEstimated(t *testing.T) {
	c, recorder, info := responsesTestContext()
	info.IsStream = true
	usage, err := GeminiResponsesStreamHandler(c, info, geminiSSE(`{"candidates":[{"content":{"parts":[{"text":"generated answer before disconnect"}]}}]}`, `{broken`))
	require.NotNil(t, err)
	require.Positive(t, usage.CompletionTokens)
	require.Equal(t, 10, usage.PromptTokens)
	require.NotNil(t, usage.BillingUsage)
	require.NotContains(t, recorder.Body.String(), "response.completed")
}

type geminiResponsesBrokenReader struct{}

func (geminiResponsesBrokenReader) Read([]byte) (int, error) {
	return 0, errors.New("upstream read failed")
}

func TestGeminiResponsesTransportFailures(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 1
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	first := `data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120}}` + "\n\n"
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "read_error", true: "timeout"}[timeout], func(t *testing.T) {
			c, recorder, info := responsesTestContext()
			info.IsStream = true
			info.DisablePing = true
			body := io.NopCloser(io.MultiReader(strings.NewReader(first), geminiResponsesBrokenReader{}))
			if timeout {
				r, w := io.Pipe()
				defer w.Close()
				body = r
				go func() { _, _ = io.WriteString(w, first) }()
			}
			usage, err := GeminiResponsesStreamHandler(c, info, &http.Response{Body: body})
			require.NotNil(t, err)
			require.True(t, types.IsSkipRetryError(err))
			require.Equal(t, 100, usage.PromptTokens)
			require.Equal(t, 20, usage.CompletionTokens)
			events := responseEvents(t, recorder.Body.String())
			last := events[len(events)-1]
			require.Equal(t, "response.failed", last.Get("type").String())
			require.Equal(t, "partial", last.Get("response.output.0.content.0.text").String())
			require.NotContains(t, recorder.Body.String(), "response.completed")
		})
	}
}

func TestGeminiResponsesIncompleteAndScaling(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previous := settings.ResponseUsageScaleEnabled
	settings.ResponseUsageScaleEnabled = true
	t.Cleanup(func() { settings.ResponseUsageScaleEnabled = previous })
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
			c, recorder, info := responsesTestContext()
			info.IsStream = stream
			info.PriceData.GlobalModelRatio = 2
			body := `{"candidates":[{"content":{"parts":[{"text":"limited"}]},"finishReason":"MAX_TOKENS"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120}}`
			resp := geminiSSE(body)
			if !stream {
				resp = &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
			}
			u, err := (&Adaptor{}).DoResponse(c, resp, info)
			require.Nil(t, err)
			usage := u.(*dto.Usage)
			require.Equal(t, 100, usage.PromptTokens)
			require.Equal(t, 20, usage.CompletionTokens)
			result := gjson.Parse(recorder.Body.String())
			if stream {
				events := responseEvents(t, recorder.Body.String())
				require.Equal(t, "response.incomplete", events[len(events)-1].Get("type").String())
				result = events[len(events)-1].Get("response")
			}
			require.Equal(t, "incomplete", result.Get("status").String())
			require.Equal(t, "max_output_tokens", result.Get("incomplete_details.reason").String())
			require.EqualValues(t, 200, result.Get("usage.input_tokens").Int())
			require.EqualValues(t, 40, result.Get("usage.output_tokens").Int())
		})
	}
}

type geminiResponsesFailWriter struct{ gin.ResponseWriter }

func (w geminiResponsesFailWriter) Write([]byte) (int, error) { return 0, errors.New("client gone") }

func TestGeminiResponsesClientDisconnect(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "write", true: "context"}[cancelled], func(t *testing.T) {
			c, recorder, info := responsesTestContext()
			info.IsStream = true
			if cancelled {
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			} else {
				c.Writer = geminiResponsesFailWriter{c.Writer}
			}
			_, err := GeminiResponsesStreamHandler(c, info, geminiSSE(`{"candidates":[{"content":{"parts":[{"text":"answer"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120}}`))
			require.NotNil(t, err)
			require.Equal(t, types.ErrorCodeClientDisconnected, err.GetErrorCode())
			require.NotContains(t, recorder.Body.String(), "response.completed")
		})
	}
}

func TestGeminiResponsesNonStreamAndErrors(t *testing.T) {
	c, recorder, info := responsesTestContext()
	body := `{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true},{"text":"answer"},{"functionCall":{"id":"call_x","name":"f","args":{"n":0}}},{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":4,"thoughtsTokenCount":2,"totalTokenCount":18}}`
	_, err := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, info)
	require.Nil(t, err)
	result := gjson.Parse(recorder.Body.String())
	require.Equal(t, "response", result.Get("object").String())
	require.Len(t, result.Get("output").Array(), 3)
	require.Equal(t, "answer\n![image](data:image/png;base64,aW1hZ2U=)", result.Get("output.1.content.0.text").String())
	require.Equal(t, `{"n":0}`, result.Get("output.2.arguments").String())
	for _, bad := range []string{`{bad`, `{"error":{"message":"failed"}}`, `{"promptFeedback":{"blockReason":"SAFETY"}}`, `{"candidates":[]}`} {
		c, recorder, info := responsesTestContext()
		_, err := GeminiResponsesHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(bad))})
		require.NotNil(t, err)
		require.Empty(t, recorder.Body.String())
	}
}
