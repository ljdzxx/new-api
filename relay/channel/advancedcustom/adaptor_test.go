package advancedcustom

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func testContext() (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1beta/models/gemini-test:generateContent", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatGemini, OriginModelName: "gemini-test", RequestURLPath: c.Request.URL.Path, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAdvancedCustom, UpstreamModelName: "gpt-4o-mini", ApiKey: "test-key", ChannelBaseUrl: "https://gateway.example/prefix", ChannelOtherSettings: dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1beta/models/{model}:generateContent", UpstreamPath: "/v1/responses", Converter: dto.AdvancedCustomGeminiToResponses}}}}}}
	info.PriceData = types.PriceData{GlobalModelRatio: 1, GlobalRatioConfigSnapshot: true}
	return c, w, info
}

func TestRouteURLAuthAndMatching(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, mode := range []string{"default", "header", "query", "none"} {
			t.Run(mode+"/"+map[bool]string{false: "json", true: "stream"}[stream], func(t *testing.T) {
				c, _, info := testContext()
				info.IsStream = stream
				if stream {
					info.RequestURLPath = "/v1beta/models/gemini-test:streamGenerateContent?alt=sse&key=client-secret"
				}
				if mode != "default" {
					info.ChannelOtherSettings.AdvancedCustom.Routes[0].Auth = &dto.AdvancedCustomRouteAuth{Type: mode, Name: "api-key", Value: "{key}"}
				}
				a := &Adaptor{}
				a.Init(info)
				url, err := a.GetRequestURL(info)
				require.NoError(t, err)
				require.True(t, strings.HasPrefix(url, "https://gateway.example/prefix/v1/responses"))
				require.NotContains(t, url, "client-secret")
				require.NotContains(t, url, "alt=sse")
				header := http.Header{}
				require.NoError(t, a.SetupRequestHeader(c, &header, info))
				if mode == "default" {
					require.Equal(t, "Bearer test-key", header.Get("Authorization"))
				}
				if mode == "header" {
					require.Equal(t, "test-key", header.Get("api-key"))
				}
				if mode == "query" {
					require.Contains(t, url, "api-key=test-key")
					require.Empty(t, header.Get("Authorization"))
				}
			})
		}
	}
}

func TestGeminiRequestConversion(t *testing.T) {
	c, _, info := testContext()
	var req dto.GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"systemInstruction":{"parts":[{"text":"system"}]},"generationConfig":{"temperature":0,"topP":0,"maxOutputTokens":0,"responseSchema":{"type":"OBJECT","properties":{"ok":{"type":"BOOLEAN","nullable":true}}}},"tools":[{"googleSearch":{}},{"functionDeclarations":[{"name":"read","parameters":{"type":"OBJECT","properties":{"path":{"type":"STRING"}}}}]}],"toolConfig":{"functionCallingConfig":{"mode":"ANY","allowedFunctionNames":["read"]}},"contents":[{"role":"model","parts":[{"text":"checking"},{"functionCall":{"name":"read","args":{"path":"a"}}}]},{"role":"user","parts":[{"functionResponse":{"name":"read","response":{"text":"result"},"parts":[{"inlineData":{"mimeType":"application/pdf","data":"JVBERg=="}}]}},{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}]}`, &req))
	req.GenerationConfig.ResponseModalities = []string{"TEXT"}
	info.ChannelOtherSettings.ToolLossPolicy = types.ConversionLossPolicyStrict
	before := raw(req)
	out, err := convertGeminiRequest(c, info, &req)
	require.NoError(t, err)
	wire := raw(out)
	require.Equal(t, "gpt-4o-mini", out.Model)
	for _, field := range []string{"temperature", "top_p", "max_output_tokens"} {
		require.Equal(t, "0", gjson.GetBytes(wire, field).Raw)
	}
	require.Equal(t, "checking", gjson.GetBytes(wire, "input.0.content.0.text").String())
	require.Equal(t, "call_gemini_0", gjson.GetBytes(wire, "input.1.call_id").String())
	require.Equal(t, "call_gemini_0", gjson.GetBytes(wire, "input.2.call_id").String())
	require.Equal(t, "input_file", gjson.GetBytes(wire, "input.2.output.1.type").String())
	require.Equal(t, "input_image", gjson.GetBytes(wire, "input.3.content.0.type").String())
	require.Equal(t, "object", gjson.GetBytes(wire, "tools.1.parameters.type").String())
	require.Equal(t, "web_search", gjson.GetBytes(wire, "tools.0.type").String())
	require.JSONEq(t, `["boolean","null"]`, gjson.GetBytes(wire, "text.format.schema.properties.ok.type").Raw)
	require.Equal(t, before, raw(req))
}

func TestLossPolicyAndUnsupportedMedia(t *testing.T) {
	for _, policy := range []types.ConversionLossPolicy{"allow", "safe", "strict"} {
		c, _, info := testContext()
		info.ChannelOtherSettings.ToolLossPolicy = policy
		var req dto.GeminiChatRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"parts":[{"text":"hello"},{"inlineData":{"mimeType":"audio/wav","data":"AAAA"}}]}]}`, &req))
		_, err := convertGeminiRequest(c, info, &req)
		require.Equal(t, policy != "allow", err != nil)
		require.Len(t, info.ConversionDiagnostics(), 1)
	}
}

func TestResponsesJSONAndStreamParity(t *testing.T) {
	terminal := `{"id":"resp_1","model":"gpt-4o-mini","status":"completed","output":[{"type":"reasoning","id":"r1","summary":[{"type":"summary_text","text":"think"}]},{"type":"message","id":"m1","content":[{"type":"output_text","text":"hello"}]},{"type":"function_call","id":"f1","call_id":"c1","name":"read","arguments":"{\"x\":1}"},{"type":"web_search_call","id":"w1","status":"completed"}],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":30},"output_tokens_details":{"reasoning_tokens":5}}}`
	for _, stream := range []bool{false, true} {
		c, w, info := testContext()
		info.IsStream = stream
		body := terminal
		if stream {
			body = `data: {"type":"response.reasoning_summary_text.delta","item_id":"r1","summary_index":0,"delta":"think"}` + "\n\n" + `data: {"type":"response.output_text.delta","item_id":"m1","content_index":0,"delta":"hello"}` + "\n\n" + `data: {"type":"response.output_item.added","item":{"id":"f1","type":"function_call","name":"read","call_id":"c1","arguments":""}}` + "\n\n" + `data: {"type":"response.function_call_arguments.delta","item_id":"f1","delta":"{\"x\":1}"}` + "\n\n" + `data: {"type":"response.completed","response":` + terminal + "}\n\n"
		}
		u, err := responsesToGemini(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
		require.Nil(t, err, w.Body.String())
		require.Equal(t, 100, u.PromptTokens)
		require.Equal(t, 20, u.CompletionTokens)
		require.Equal(t, 30, u.PromptTokensDetails.CachedTokens)
		require.Equal(t, dto.BillingUsageSourceOAIResponses, u.BillingUsage.Source)
		require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
		require.Equal(t, 1, strings.Count(w.Body.String(), `"text":"hello"`))
		require.Equal(t, 1, strings.Count(w.Body.String(), `"name":"read"`))
		require.Contains(t, w.Body.String(), `"candidatesTokenCount":15`)
		require.Contains(t, w.Body.String(), `"thoughtsTokenCount":5`)
		require.NotContains(t, w.Body.String(), "[DONE]")
	}
}

func TestStreamFailureDoesNotEmitSuccess(t *testing.T) {
	for _, tail := range []string{"", "data: broken\n\n", `data: {"type":"response.failed","response":{"status":"failed","usage":{"input_tokens":10,"output_tokens":2}}}` + "\n\n"} {
		c, w, info := testContext()
		info.IsStream = true
		body := `data: {"type":"response.output_text.delta","item_id":"m1","content_index":0,"delta":"hello"}` + "\n\n" + tail
		u, err := responsesToGemini(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
		require.NotNil(t, err)
		require.True(t, types.IsSkipRetryError(err))
		require.NotNil(t, u)
		require.NotContains(t, w.Body.String(), "finishReason")
	}
}

func TestUsageSaturationAndExplicitZero(t *testing.T) {
	for _, wire := range []string{`{"input_tokens":0,"output_tokens":0}`, `{"input_tokens":9223372036854775807,"output_tokens":9223372036854775807,"output_tokens_details":{"reasoning_tokens":9223372036854775807}}`} {
		c, _, info := testContext()
		state := &responseState{c: c, info: info}
		require.NoError(t, state.observeUsage(gjson.Parse(`{"usage":`+wire+`}`)))
		u := state.finalUsage()
		require.GreaterOrEqual(t, u.TotalTokens, 0)
		require.LessOrEqual(t, u.TotalTokens, common.MaxQuota)
		require.LessOrEqual(t, u.BillingUsage.OpenAIUsage.InputTokens, common.MaxQuota)
		if strings.Contains(wire, "922") {
			require.NotNil(t, info.QuotaClamp)
		} else {
			require.Zero(t, u.TotalTokens)
		}
	}
}

func TestResponsesUsageIgnoresInjectedBillingFields(t *testing.T) {
	c, _, info := testContext()
	state := &responseState{c: c, info: info}
	require.NoError(t, state.observeUsage(gjson.Parse(`{"usage":{"input_tokens":10,"output_tokens":2,"prompt_tokens":-100000,"completion_tokens":99999999,"prompt_tokens_details":{"cached_tokens":9999999},"billing_usage":{"source":"claude_messages","semantic":"anthropic"}}}`)))
	u := state.finalUsage()
	require.Equal(t, 10, u.BillingUsage.OpenAIUsage.PromptTokens)
	require.Equal(t, 2, u.BillingUsage.OpenAIUsage.CompletionTokens)
	require.Zero(t, u.BillingUsage.OpenAIUsage.PromptTokensDetails.CachedTokens)
	require.Nil(t, u.BillingUsage.OpenAIUsage.BillingUsage)
}

func TestCitationSnapshotsDoNotDuplicateAndUseUTF8Offsets(t *testing.T) {
	c, w, info := testContext()
	info.IsStream = true
	item := `{"type":"message","id":"m","content":[{"type":"output_text","text":"你好","annotations":[{"type":"url_citation","url":"https://example.com","title":"source","start_index":0,"end_index":2}]}]}`
	body := `data: {"type":"response.output_item.done","output_index":0,"item":` + item + "}\n\n" + `data: {"type":"response.completed","response":{"status":"completed","output":[` + item + `],"usage":{"input_tokens":10,"output_tokens":2}}}` + "\n\n"
	_, err := responsesToGemini(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
	require.Nil(t, err)
	frames := strings.Split(strings.TrimSpace(w.Body.String()), "\n\n")
	last := gjson.Parse(strings.TrimPrefix(frames[len(frames)-1], "data: "))
	require.Len(t, last.Get("candidates.0.groundingMetadata.groundingChunks").Array(), 1)
	require.EqualValues(t, 6, last.Get("candidates.0.groundingMetadata.groundingSupports.0.segment.endIndex").Int())
}
