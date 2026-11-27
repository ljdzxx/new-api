package claude

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeResponsesUpstreamGolden(t *testing.T) {
	c, _, info := claudeConversionContext(types.RelayFormatOpenAIResponses)
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gpt-test","instructions":"You are a helpful assistant.","max_output_tokens":1024,"stream":true,"tools":[{"type":"function","name":"get_weather","description":"Get weather by city","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}],"input":[{"role":"user","content":[{"type":"input_text","text":"What is in this image?"},{"type":"input_image","image_url":"data:image/png;base64,aGVsbG8="}]},{"type":"function_call","call_id":"call_abc","name":"get_weather","arguments":"{\"city\":\"Paris\"}"},{"type":"function_call_output","call_id":"call_abc","output":"15 degrees"}]}`, &req))
	out, err := ConvertResponsesToClaude(c, info, req)
	require.NoError(t, err)
	body, err := common.Marshal(out)
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/upstream_responses_to_claude.json")
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(body))
}

func TestClaudeResponsesCustomToolsAndNamespaceRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
			c, w, info := claudeConversionContext(types.RelayFormatOpenAIResponses)
			_, err := ConvertResponsesToClaude(c, info, dto.OpenAIResponsesRequest{Model: "claude-test", Input: json.RawMessage(`"hi"`), Tools: json.RawMessage(`[{"type":"namespace","name":"ops","tools":[{"type":"custom","name":"exec"}]}]`)})
			require.NoError(t, err)
			payload := `{"type":"message","model":"claude-test","content":[{"type":"tool_use","id":"one","name":"ops__exec","input":{"input":"echo hi"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":8}}`
			var apiErr *types.NewAPIError
			if stream {
				frames := []string{claudeConvertedFrames()[0], `{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"one","name":"ops__exec","input":{}}}`, `{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"input\":\"echo "}}`, `{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"hi\"}"}}`, `{"type":"content_block_stop","index":3}`, claudeConvertedFrames()[11], claudeConvertedFrames()[12]}
				_, apiErr = runClaudeConvertedFrames(c, info, frames)
			} else {
				_, apiErr = (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, info)
			}
			require.Nil(t, apiErr)
			body := w.Body.String()
			var result gjson.Result
			if stream {
				for _, line := range strings.Split(body, "\n") {
					if strings.HasPrefix(line, "data: ") {
						event := gjson.Parse(strings.TrimPrefix(line, "data: "))
						if event.Get("type").String() == "response.completed" {
							result = event.Get("response")
						}
					}
				}
				require.Contains(t, body, "event: response.custom_tool_call_input.delta")
			} else {
				result = gjson.Parse(body)
			}
			require.Equal(t, "custom_tool_call", result.Get("output.0.type").String())
			require.Equal(t, "ops", result.Get("output.0.namespace").String())
			require.Equal(t, "exec", result.Get("output.0.name").String())
			require.Equal(t, "echo hi", result.Get("output.0.input").String())
			require.False(t, result.Get("output.0.arguments").Exists())
		})
	}
}

func TestClaudeConvertedUsageScalingAndMaxTokens(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
		c, w, info := claudeConversionContext(format)
		info.PriceData.GlobalModelRatio = 2
		frames := claudeConvertedFrames()
		frames[11] = strings.ReplaceAll(frames[11], "tool_use", "max_tokens")
		usage, apiErr := runClaudeConvertedFrames(c, info, frames)
		require.Nil(t, apiErr)
		require.Equal(t, 10, usage.PromptTokens)
		require.Equal(t, 8, usage.CompletionTokens)
		var last gjson.Result
		for _, line := range strings.Split(w.Body.String(), "\n") {
			if strings.HasPrefix(line, "data: ") {
				last = gjson.Parse(strings.TrimPrefix(line, "data: "))
			}
		}
		if format == types.RelayFormatOpenAIResponses {
			require.Equal(t, "response.incomplete", last.Get("type").String())
			require.Equal(t, "incomplete", last.Get("response.output.1.status").String())
			require.Equal(t, "max_output_tokens", last.Get("response.incomplete_details.reason").String())
			require.Equal(t, int64(120), last.Get("response.usage.input_tokens").Int())
		} else {
			require.Equal(t, "MAX_TOKENS", last.Get("candidates.0.finishReason").String())
			require.Equal(t, int64(120), last.Get("usageMetadata.promptTokenCount").Int())
		}
	}
}

type claudeFlushingRecorder struct {
	*httptest.ResponseRecorder
	observed chan struct{}
}

func (w *claudeFlushingRecorder) Flush() {
	w.ResponseRecorder.Flush()
	if strings.Contains(w.Body.String(), "Answer") {
		select {
		case w.observed <- struct{}{}:
		default:
		}
	}
}

func TestClaudeConvertedStreamFlushesBeforeUpstreamEnds(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
		t.Run(string(format), func(t *testing.T) {
			_, _, info := claudeConversionContext(format)
			w := &claudeFlushingRecorder{ResponseRecorder: httptest.NewRecorder(), observed: make(chan struct{}, 1)}
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			info.IsStream = true
			done := make(chan *types.NewAPIError, 1)
			go func() {
				_, err := ClaudeConvertedStreamHandler(c, &http.Response{StatusCode: 200, Header: make(http.Header), Body: reader}, info)
				done <- err
			}()
			frames := claudeConvertedFrames()
			for _, frame := range frames[:6] {
				_, err := io.WriteString(writer, "data: "+frame+"\n\n")
				require.NoError(t, err)
			}
			select {
			case <-w.observed:
			case <-time.After(3 * time.Second):
				t.Fatal("text waited for upstream completion")
			}
			for _, frame := range frames[6:] {
				_, err := io.WriteString(writer, "data: "+frame+"\n\n")
				require.NoError(t, err)
			}
			writer.Close()
			select {
			case err := <-done:
				require.Nil(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("stream did not finish")
			}
		})
	}
}

type claudeFailingWriter struct{ *httptest.ResponseRecorder }

func (w *claudeFailingWriter) Write([]byte) (int, error) { return 0, errors.New("client closed") }

func TestClaudeConvertedClientWriteFailure(t *testing.T) {
	_, _, info := claudeConversionContext(types.RelayFormatOpenAIResponses)
	c, _ := gin.CreateTestContext(&claudeFailingWriter{httptest.NewRecorder()})
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	usage, err := runClaudeConvertedFrames(c, info, claudeConvertedFrames())
	require.NotNil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeClientDisconnected, err.GetErrorCode())
	require.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	c, _, info = claudeConversionContext(types.RelayFormatGemini)
	ctx, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(ctx)
	_, err = runClaudeConvertedFrames(c, info, claudeConvertedFrames())
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeClientDisconnected, err.GetErrorCode())
}

func TestClaudeConversionBoundaryCases(t *testing.T) {
	c, _, info := claudeConversionContext(types.RelayFormatOpenAIResponses)
	info.ChannelOtherSettings.ToolLossPolicy = types.ConversionLossPolicySafe
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-test","text":{"format":{"type":"text"}},"truncation":"disabled","tools":[{"type":"function","name":"f","parameters":null}],"input":[{"role":"user","content":"hi"},{"type":"function_call","call_id":"one","name":"f","arguments":"{}"},{"type":"function_call_output","call_id":"one","output":[9007199254740993,false]}]}`, &req))
	out, err := ConvertResponsesToClaude(c, info, req)
	require.NoError(t, err)
	body, err := common.Marshal(out)
	require.NoError(t, err)
	require.Equal(t, `[9007199254740993,false]`, gjson.GetBytes(body, "messages.2.content.0.content").String())
	require.Equal(t, "object", gjson.GetBytes(body, "tools.0.input_schema.type").String())
	c, _, info = claudeConversionContext(types.RelayFormatGemini)
	_, err = ConvertGeminiToClaude(c, info, &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{MaxOutputTokens: common.GetPointer(uint(helper.MaxTokensLimit) + 1)}})
	require.Error(t, err)
	_, err = ConvertGeminiToClaude(c, info, &dto.GeminiChatRequest{ToolConfig: &dto.ToolConfig{FunctionCallingConfig: &dto.FunctionCallingConfig{Mode: "ANY", AllowedFunctionNames: []string{"absent"}}}})
	require.Error(t, err)
	schema, err := geminiSchemaToClaude(json.RawMessage(`{"type":"OBJECT","properties":{"a.b":{"type":"STRING","nullable":true},"id":{"type":"INTEGER","enum":[9007199254740993]}}}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"object","properties":{"a.b":{"type":["string","null"]},"id":{"type":"integer","enum":[9007199254740993]}}}`, string(schema))
}

func TestClaudeConvertedFullyCachedEmptyOutput(t *testing.T) {
	c, w, info := claudeConversionContext(types.RelayFormatOpenAIResponses)
	frames := []string{`{"type":"message_start","message":{"model":"claude-test","usage":{"input_tokens":0,"cache_read_input_tokens":20,"cache_creation_input_tokens":30,"output_tokens":0}}}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`, `{"type":"message_stop"}`}
	usage, err := runClaudeConvertedFrames(c, info, frames)
	require.Nil(t, err)
	require.Zero(t, usage.PromptTokens)
	require.Zero(t, usage.CompletionTokens)
	require.Contains(t, w.Body.String(), `"input_tokens":50`)
	require.Contains(t, w.Body.String(), `"output_tokens":0`)
}

func claudeConversionContext(format types.RelayFormat) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	path := "/v1/responses"
	if format == types.RelayFormatGemini {
		path = "/v1beta/models/claude-test:streamGenerateContent"
	}
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	info := &relaycommon.RelayInfo{RelayFormat: format, OriginModelName: "claude-test", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAnthropic, UpstreamModelName: "claude-test"}}
	info.PriceData.GlobalModelRatio = 1
	info.SetEstimatePromptTokens(10)
	return c, w, info
}

func TestResponsesToClaudeHistoryAndTools(t *testing.T) {
	c, _, info := claudeConversionContext(types.RelayFormatOpenAIResponses)
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-test","stream":false,"temperature":0,"top_p":0,"max_output_tokens":1024,"parallel_tool_calls":false,"instructions":"System","tools":[{"type":"namespace","name":"ops","tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"z":{"const":9007199254740993},"a":{"type":"boolean"}}}}]},{"type":"custom","name":"exec"}],"input":[{"role":"developer","content":"Developer"},{"role":"user","content":"Go"},{"type":"function_call","namespace":"ops","name":"lookup","call_id":"one","arguments":"{\"id\":9007199254740993,\"enabled\":false}"},{"type":"function_call","name":"lookup2","call_id":"two","arguments":"{}"},{"type":"function_call_output","call_id":"one","output":[{"type":"input_text","text":"Found"},{"type":"input_image","image_url":"data:image/png;base64,aGk="}]},{"type":"function_call_output","call_id":"two","output":{"value":18446744073709551615}},{"type":"custom_tool_call","name":"exec","call_id":"three","input":"echo hi"},{"type":"custom_tool_call_output","call_id":"three","output":"ok"}]}`, &req))
	before, err := common.Marshal(req)
	require.NoError(t, err)
	out, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	body, err := common.Marshal(out)
	require.NoError(t, err)
	for _, field := range []string{"temperature", "top_p", "stream"} {
		require.True(t, gjson.GetBytes(body, field).Exists())
	}
	require.Equal(t, "false", gjson.GetBytes(body, "stream").Raw)
	require.Equal(t, "System", gjson.GetBytes(body, "system.0.text").String())
	require.Equal(t, "Developer", gjson.GetBytes(body, "system.1.text").String())
	require.True(t, gjson.GetBytes(body, "tool_choice.disable_parallel_tool_use").Bool())
	require.Equal(t, "ops__lookup", gjson.GetBytes(body, "messages.1.content.0.name").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(body, "messages.1.content.0.input.id").Raw)
	require.Equal(t, "two", gjson.GetBytes(body, "messages.1.content.1.id").String())
	require.Equal(t, "image", gjson.GetBytes(body, "messages.2.content.0.content.1.type").String())
	require.Contains(t, gjson.GetBytes(body, "messages.2.content.1.content").String(), "18446744073709551615")
	require.Equal(t, "echo hi", gjson.GetBytes(body, "messages.3.content.0.input.input").String())
	require.Equal(t, `{"type":"object","properties":{"z":{"const":9007199254740993},"a":{"type":"boolean"}}}`, gjson.GetBytes(body, "tools.0.input_schema").Raw)
	after, err := common.Marshal(req)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after))
}

func TestGeminiToClaudeHistoryAndThinking(t *testing.T) {
	c, _, info := claudeConversionContext(types.RelayFormatGemini)
	var req dto.GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"systemInstruction":{"parts":[{"text":"System"}]},"generationConfig":{"temperature":0,"topP":0,"topK":0,"maxOutputTokens":4096,"stopSequences":["END"]},"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT","properties":{"z":{"type":"INTEGER","enum":[9007199254740993]},"a":{"type":"BOOLEAN"}}}}]}],"toolConfig":{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["lookup"]}},"contents":[{"role":"user","parts":[{"text":"Go"}]},{"role":"model","parts":[{"text":"Checking"},{"functionCall":{"name":"lookup","args":{"id":9007199254740993}}},{"functionCall":{"name":"lookup","args":{"id":2}}}]},{"role":"user","parts":[{"functionResponse":{"name":"lookup","response":{"result":9007199254740993}}},{"functionResponse":{"name":"lookup","response":{"result":false}}},{"text":"Continue"}]}]}`, &req))
	copied, err := common.DeepCopy(&req)
	require.NoError(t, err)
	out, err := (&Adaptor{}).ConvertGeminiRequest(c, info, copied)
	require.NoError(t, err)
	body, err := common.Marshal(out)
	require.NoError(t, err)
	for _, field := range []string{"temperature", "top_p", "top_k"} {
		require.Equal(t, "0", gjson.GetBytes(body, field).Raw)
	}
	require.Equal(t, "END", gjson.GetBytes(body, "stop_sequences.0").String())
	require.Equal(t, "Checking", gjson.GetBytes(body, "messages.1.content.0.text").String())
	id1 := gjson.GetBytes(body, "messages.1.content.1.id").String()
	id2 := gjson.GetBytes(body, "messages.1.content.2.id").String()
	require.NotEmpty(t, id1)
	require.NotEqual(t, id1, id2)
	require.Equal(t, id1, gjson.GetBytes(body, "messages.2.content.0.tool_use_id").String())
	require.Equal(t, id2, gjson.GetBytes(body, "messages.2.content.1.tool_use_id").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(body, "messages.1.content.1.input.id").Raw)
	require.Contains(t, gjson.GetBytes(body, "messages.2.content.0.content.0.text").String(), "9007199254740993")
	require.Equal(t, `{"type":"object","properties":{"z":{"type":"integer","enum":[9007199254740993]},"a":{"type":"boolean"}}}`, gjson.GetBytes(body, "tools.0.input_schema").Raw)
	require.Equal(t, "lookup", gjson.GetBytes(body, "tool_choice.name").String())
	info.UpstreamModelName = "claude-sonnet-5-5"
	info.OriginModelName = info.UpstreamModelName
	req.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{ThinkingLevel: "high"}
	thinking, err := ConvertGeminiToClaude(c, info, &req)
	require.NoError(t, err)
	require.NotNil(t, thinking.Thinking)
	require.Equal(t, "adaptive", thinking.Thinking.Type)
}

func TestClaudeConvertedRequestValidation(t *testing.T) {
	for _, input := range []string{
		`{"model":"claude-test","input":"hi","previous_response_id":"resp_old"}`,
		`{"model":"claude-test","input":"hi","max_output_tokens":18446744073709551615}`,
		`{"model":"claude-test","input":{}}`,
		`{"model":"claude-test","input":[{"type":"function_call","name":"f","call_id":"one","arguments":"[1]"}]}`,
		`{"model":"claude-test","input":"hi","multi_agent":{"enabled":true}}`,
	} {
		c, _, info := claudeConversionContext(types.RelayFormatOpenAIResponses)
		var req dto.OpenAIResponsesRequest
		require.NoError(t, common.UnmarshalJsonStr(input, &req))
		_, err := ConvertResponsesToClaude(c, info, req)
		require.Error(t, err, input)
	}
	c, _, info := claudeConversionContext(types.RelayFormatOpenAIResponses)
	info.ChannelOtherSettings.ToolLossPolicy = types.ConversionLossPolicySafe
	_, err := ConvertResponsesToClaude(c, info, dto.OpenAIResponsesRequest{Model: "claude-test", Input: json.RawMessage(`"hi"`), Tools: json.RawMessage(`[{"type":"web_search"}]`)})
	require.Error(t, err)
	c, _, info = claudeConversionContext(types.RelayFormatGemini)
	_, err = ConvertGeminiToClaude(c, info, &dto.GeminiChatRequest{CachedContent: "cached/1"})
	require.Error(t, err)
	_, err = ConvertGeminiToClaude(c, info, &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: "high", ThinkingBudget: common.GetPointer(1024)}}})
	require.Error(t, err)
}

const claudeConvertedMessage = `{"type":"message","id":"msg_test","model":"claude-test","content":[{"type":"thinking","thinking":"Reason"},{"type":"text","text":"Answer"},{"type":"tool_use","id":"call_1","name":"lookup","input":{"id":9007199254740993}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":30,"output_tokens":8}}`

func TestClaudeConvertedNonStreamResponse(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
		t.Run(string(format), func(t *testing.T) {
			c, w, info := claudeConversionContext(format)
			usage, apiErr := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(claudeConvertedMessage))}, info)
			require.Nil(t, apiErr)
			require.Equal(t, 10, usage.(*dto.Usage).PromptTokens)
			require.Equal(t, 20, usage.(*dto.Usage).PromptTokensDetails.CachedTokens)
			require.NotNil(t, usage.(*dto.Usage).BillingUsage)
			body := w.Body.String()
			if format == types.RelayFormatOpenAIResponses {
				require.Equal(t, "reasoning", gjson.Get(body, "output.0.type").String())
				require.Equal(t, "Answer", gjson.Get(body, "output.1.content.0.text").String())
				require.Equal(t, `{"id":9007199254740993}`, gjson.Get(body, "output.2.arguments").String())
				require.Equal(t, int64(60), gjson.Get(body, "usage.input_tokens").Int())
			} else {
				require.True(t, gjson.Get(body, "candidates.0.content.parts.0.thought").Bool())
				require.Equal(t, "Answer", gjson.Get(body, "candidates.0.content.parts.1.text").String())
				require.Equal(t, "9007199254740993", gjson.Get(body, "candidates.0.content.parts.2.functionCall.args.id").Raw)
				require.Equal(t, "call_1", gjson.Get(body, "candidates.0.content.parts.2.functionCall.id").String())
				require.Equal(t, int64(60), gjson.Get(body, "usageMetadata.promptTokenCount").Int())
			}
		})
	}
}

func claudeConvertedFrames() []string {
	return []string{
		`{"type":"message_start","message":{"id":"msg_test","model":"claude-test","usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":30,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Reason"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Answer"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call_1","name":"lookup","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"id\":"}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"9007199254740993}"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}`,
		`{"type":"message_stop"}`,
	}
}

func runClaudeConvertedFrames(c *gin.Context, info *relaycommon.RelayInfo, frames []string) (*dto.Usage, *types.NewAPIError) {
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	info.IsStream = true
	info.ChannelSetting.PassThroughBodyEnabled = true
	usage, err := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}, info)
	if usage == nil {
		return nil, err
	}
	return usage.(*dto.Usage), err
}

func TestClaudeConvertedStreams(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
		t.Run(string(format), func(t *testing.T) {
			c, w, info := claudeConversionContext(format)
			usage, err := runClaudeConvertedFrames(c, info, claudeConvertedFrames())
			require.Nil(t, err)
			require.Equal(t, 8, usage.CompletionTokens)
			require.Equal(t, 10, usage.PromptTokens)
			wire := w.Body.String()
			require.NotContains(t, wire, "event: message_start")
			require.NotContains(t, wire, "[DONE]")
			var events []gjson.Result
			for _, line := range strings.Split(wire, "\n") {
				if strings.HasPrefix(line, "data: ") {
					events = append(events, gjson.Parse(strings.TrimPrefix(line, "data: ")))
				}
			}
			require.NotEmpty(t, events)
			if format == types.RelayFormatOpenAIResponses {
				for i, event := range events {
					require.Equal(t, int64(i), event.Get("sequence_number").Int())
				}
				last := events[len(events)-1]
				require.Equal(t, "response.completed", last.Get("type").String())
				require.Equal(t, `{"id":9007199254740993}`, last.Get("response.output.2.arguments").String())
				require.Equal(t, int64(60), last.Get("response.usage.input_tokens").Int())
				ids := map[int64]string{}
				for _, event := range events {
					if event.Get("type").String() == "response.output_item.added" {
						ids[event.Get("output_index").Int()] = event.Get("item.id").String()
					}
					if event.Get("item_id").Exists() {
						require.Equal(t, ids[event.Get("output_index").Int()], event.Get("item_id").String())
					}
				}
			} else {
				require.Len(t, events, 4)
				require.True(t, events[0].Get("candidates.0.content.parts.0.thought").Bool())
				require.Equal(t, "Answer", events[1].Get("candidates.0.content.parts.0.text").String())
				require.Equal(t, "9007199254740993", events[2].Get("candidates.0.content.parts.0.functionCall.args.id").Raw)
				require.Equal(t, int64(60), events[3].Get("usageMetadata.promptTokenCount").Int())
			}
		})
	}
}

func TestClaudeResponsesInterruptedStream(t *testing.T) {
	for _, tail := range []string{"", `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`, `{"type":`, `{"type":"message_stop"}`} {
		t.Run(tail, func(t *testing.T) {
			c, w, info := claudeConversionContext(types.RelayFormatOpenAIResponses)
			frames := claudeConvertedFrames()[:6]
			if tail != "" {
				frames = append(append([]string{}, frames...), tail)
			}
			usage, err := runClaudeConvertedFrames(c, info, frames)
			require.NotNil(t, err)
			require.NotNil(t, usage)
			require.Equal(t, 20, usage.PromptTokensDetails.CachedTokens)
			require.True(t, types.IsSkipRetryError(err))
			wire := w.Body.String()
			require.NotContains(t, wire, "event: response.completed")
			require.Equal(t, 1, strings.Count(wire, "event: response.failed"))
			require.Contains(t, wire, `"Answer"`)
			require.NoError(t, helper.WriteResponsesStreamFailure(c, err, info))
			require.Equal(t, 1, strings.Count(w.Body.String(), "event: response.failed"))
		})
	}
}

func TestClaudeResponsesForcedToolChoice(t *testing.T) {
	for _, tc := range []struct{ kind, namespace, name string }{{"function", "", "lookup"}, {"custom", "ops", "ops__lookup"}} {
		c, _, info := claudeConversionContext(types.RelayFormatOpenAIResponses)
		tool := map[string]any{"type": tc.kind, "name": "lookup"}
		choice := map[string]any{"type": tc.kind, "name": "lookup"}
		if tc.namespace != "" {
			tool = map[string]any{"type": "namespace", "name": tc.namespace, "tools": []any{tool}}
			choice["namespace"] = tc.namespace
		}
		tools, err := common.Marshal([]any{tool})
		require.NoError(t, err)
		selected, err := common.Marshal(choice)
		require.NoError(t, err)
		out, err := ConvertResponsesToClaude(c, info, dto.OpenAIResponsesRequest{Model: "claude-test", Input: json.RawMessage(`"hello"`), Tools: tools, ToolChoice: selected})
		require.NoError(t, err)
		body, err := common.Marshal(out)
		require.NoError(t, err)
		require.Equal(t, "tool", gjson.GetBytes(body, "tool_choice.type").String())
		require.Equal(t, tc.name, gjson.GetBytes(body, "tool_choice.name").String())
	}
}

func TestClaudeRejectsGeminiContinuation(t *testing.T) {
	for _, key := range []string{"continuationToken", "continuation_token"} {
		for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicyAllow, types.ConversionLossPolicyStrict} {
			c, _, info := claudeConversionContext(types.RelayFormatGemini)
			info.ChannelOtherSettings.ToolLossPolicy = policy
			var req dto.GeminiChatRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"parts":[{"text":"continue"}]}],"`+key+`":"previous"}`, &req))
			_, err := ConvertGeminiToClaude(c, info, &req)
			require.ErrorContains(t, err, "continuationToken requires a native Gemini upstream")
		}
	}
}

func TestClaudeTruncatedToolArguments(t *testing.T) {
	for _, stop := range []string{"max_tokens", "model_context_window_exceeded", "tool_use"} {
		for _, format := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
			t.Run(stop+"/"+string(format), func(t *testing.T) {
				c, w, info := claudeConversionContext(format)
				frames := claudeConvertedFrames()
				frames = append(frames[:9], frames[10:]...)
				frames[10] = strings.Replace(frames[10], "tool_use", stop, 1)
				usage, err := runClaudeConvertedFrames(c, info, frames)
				wire := w.Body.String()
				require.Equal(t, 8, usage.CompletionTokens)
				if stop == "tool_use" {
					require.NotNil(t, err)
					if format == types.RelayFormatOpenAIResponses {
						require.Contains(t, wire, "event: response.failed")
					}
					return
				}
				require.Nil(t, err)
				require.Contains(t, wire, "Answer")
				require.NotContains(t, wire, "event: response.failed")
				if format == types.RelayFormatGemini {
					require.Contains(t, wire, `"finishReason":"MAX_TOKENS"`)
					require.NotContains(t, wire, `"functionCall"`)
					return
				}
				require.Equal(t, 1, strings.Count(wire, "event: response.incomplete"))
				require.NotContains(t, wire, "event: response.function_call_arguments.done")
				for _, line := range strings.Split(wire, "\n") {
					if data, ok := strings.CutPrefix(line, "data: "); ok && gjson.Get(data, "type").String() == "response.incomplete" {
						require.Equal(t, "max_output_tokens", gjson.Get(data, "response.incomplete_details.reason").String())
						require.Equal(t, "incomplete", gjson.Get(data, "response.output.2.status").String())
						require.Equal(t, `{"id":`, gjson.Get(data, "response.output.2.arguments").String())
					}
				}
			})
		}
	}
}
