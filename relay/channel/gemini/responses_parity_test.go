package gemini

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Fixtures follow upstream 718a4e7f97's toolconv extraction, loss policy,
// req_helpers media/history behavior and Gemini -> Chat -> Responses output.
func TestResponsesUpstreamDynamicToolsAndHistory(t *testing.T) {
	c, _, info := responsesTestContext()
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-3-flash-preview","tools":[{"type":"tool_search"}],"input":[{"role":"user","content":"read file"},{"type":"additional_tools","tools":[{"type":"namespace","name":"fs","tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]}]},{"type":"function_call","namespace":"fs","name":"read","id":"call_old","arguments":"{}"},{"type":"function_call_output","call_id":"call_old","output":"false"},{"type":"web_search_call","status":"completed","action":{"type":"search","queries":["q"]}},{"role":"assistant","content":"unfinished answer"}]}`, &req))
	before, _ := common.Marshal(req)
	got, err := ConvertResponsesToGemini(c, info, req)
	require.NoError(t, err)
	require.Len(t, got.Contents, 3)
	require.Equal(t, "user", got.Contents[2].Role)
	require.Equal(t, "fs__read", got.Contents[1].Parts[0].FunctionCall.FunctionName)
	require.Equal(t, "fs__read", got.Contents[2].Parts[0].FunctionResponse.Name)
	require.Equal(t, map[string]any{"content": "false"}, got.Contents[2].Parts[0].FunctionResponse.Response)
	raw, _ := common.Marshal(got)
	require.Equal(t, "fs__read", gjson.GetBytes(raw, "tools.0.functionDeclarations.0.name").String())
	require.NotContains(t, string(raw), "tool_search")
	require.NotContains(t, string(raw), "unfinished answer")
	codes := map[string]bool{}
	for _, d := range info.ConversionDiagnostics() {
		codes[d.Code] = true
	}
	for _, code := range []string{"additional_tools_flattened", "tool_namespace_flattened", "unsupported_opaque_tool", "hosted_tool_history_unsupported", "trailing_assistant_omitted"} {
		require.True(t, codes[code], code)
	}
	after, _ := common.Marshal(req)
	require.Equal(t, before, after)
	// A new attempt/request must not inherit dropped tools or diagnostics.
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-3-flash-preview","input":"hi"}`, &req))
	req.Tools = nil
	_, err = ConvertResponsesToGemini(c, info, req)
	require.NoError(t, err)
	require.Empty(t, info.ConversionDiagnostics())
	value, _ := c.Get(geminiResponsesToolNamesKey)
	require.Empty(t, value)
}

func TestResponsesUpstreamLossPolicies(t *testing.T) {
	for _, test := range []struct {
		name, fields, code string
		semantic           bool
	}{
		{"opaque", `"tools":[{"type":"tool_search"}]`, "unsupported_opaque_tool", true},
		{"parallel", `"tools":[{"type":"function","name":"run"}],"parallel_tool_calls":false`, "unsupported_parallel_tool_control", true},
		{"search_filter", `"tools":[{"type":"web_search","filters":{"allowed_domains":["example.com"]}}]`, "unsupported_search_constraints", true},
		{"external_false", `"tools":[{"type":"web_search","external_web_access":false}]`, "unsupported_search_constraints", true},
		{"search_tuning", `"tools":[{"type":"web_search","search_context_size":"low"}]`, "unsupported_search_tuning", false},
		{"strict_false", `"tools":[{"type":"function","name":"run","strict":false}]`, "unsupported_function_strict", false},
		{"custom", `"tools":[{"type":"custom","name":"patch"}]`, "custom_tool_as_function", false},
		{"opaque_choice", `"tools":[{"type":"function","name":"run"}],"tool_choice":{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"run"}]}`, "unsupported_tool_choice", true},
		{"required_without_tools", `"tool_choice":"required"`, "tool_choice_without_tools", true},
	} {
		for _, policy := range []types.ConversionLossPolicy{"", types.ConversionLossPolicyAllow, types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
			t.Run(test.name+"/"+string(policy), func(t *testing.T) {
				c, _, info := responsesTestContext()
				info.ChannelOtherSettings.ToolLossPolicy = policy
				var req dto.OpenAIResponsesRequest
				require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-3-flash-preview","input":"hi",`+test.fields+`}`, &req))
				_, err := ConvertResponsesToGemini(c, info, req)
				if policy == types.ConversionLossPolicyStrict || policy == types.ConversionLossPolicySafe && test.semantic {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				found := false
				for _, d := range info.ConversionDiagnostics() {
					if d.Code == test.code {
						found = true
					}
				}
				require.True(t, found, test.code)
			})
		}
	}
}

func TestResponsesUpstreamDiagnosticsCapCannotBypassSafe(t *testing.T) {
	c, _, info := responsesTestContext()
	info.ChannelOtherSettings.ToolLossPolicy = types.ConversionLossPolicySafe
	tools := make([]map[string]any, 0, 41)
	for i := 0; i < 40; i++ {
		tools = append(tools, map[string]any{"type": "function", "name": fmt.Sprintf("f%d", i), "strict": false})
	}
	tools = append(tools, map[string]any{"type": "tool_search"})
	raw, _ := common.Marshal(tools)
	_, err := ConvertResponsesToGemini(c, info, dto.OpenAIResponsesRequest{Model: info.UpstreamModelName, Input: []byte(`"hi"`), Tools: raw})
	require.ErrorContains(t, err, "tool_search")
	require.Len(t, info.ConversionDiagnostics(), 32)
	require.True(t, info.ConversionDiagnosticsTruncated())
}

func TestResponsesOpaqueInputUsesLossPolicy(t *testing.T) {
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicyAllow, types.ConversionLossPolicySafe} {
		c, _, info := responsesTestContext()
		info.ChannelOtherSettings.ToolLossPolicy = policy
		var req dto.OpenAIResponsesRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-3-flash-preview","input":[{"role":"user","content":"visible history"},{"type":"compaction","encrypted_content":"opaque"},{"type":"item_reference","id":"remote"}]}`, &req))
		got, err := ConvertResponsesToGemini(c, info, req)
		if policy == types.ConversionLossPolicySafe {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Len(t, got.Contents, 1)
			raw, _ := common.Marshal(got)
			require.NotContains(t, string(raw), "opaque")
		}
		require.Len(t, info.ConversionDiagnostics(), 2)
	}
}

func TestResponsesUpstreamSearchAliasesAndReservedFunctionNames(t *testing.T) {
	for _, kind := range []string{"web_search", "web_search_preview", "web_search_2025_08_26", "web_search_preview_2025_03_11"} {
		t.Run(kind, func(t *testing.T) {
			c, _, info := responsesTestContext()
			var req dto.OpenAIResponsesRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-3-flash-preview","input":"hi","tools":[{"type":"`+kind+`"},{"type":"function","name":"googleSearch"}]}`, &req))
			got, err := ConvertResponsesToGemini(c, info, req)
			require.NoError(t, err)
			raw, _ := common.Marshal(got)
			require.True(t, gjson.GetBytes(raw, "tools.0.googleSearch").Exists())
			require.Equal(t, "googleSearch", gjson.GetBytes(raw, "tools.1.functionDeclarations.0.name").String())
		})
	}
}

func TestResponsesUpstreamNameConflictAndCustomSelection(t *testing.T) {
	for _, body := range []string{
		`[{"type":"custom","name":"run"},{"type":"function","name":"run"}]`,
		`[{"type":"function","name":"run"},{"type":"custom","name":"run"}]`,
		`[{"type":"function","name":"fs__run"},{"type":"namespace","name":"fs","tools":[{"type":"function","name":"run"}]}]`,
	} {
		c, _, info := responsesTestContext()
		got, err := ConvertResponsesToGemini(c, info, dto.OpenAIResponsesRequest{Model: info.UpstreamModelName, Input: []byte(`"hi"`), Tools: []byte(body)})
		require.NoError(t, err)
		raw, _ := common.Marshal(got)
		require.Len(t, gjson.GetBytes(raw, "tools.0.functionDeclarations").Array(), 1)
		state := newGeminiResponsesState(c, info, nil)
		require.Empty(t, state.custom)
		require.NotEmpty(t, info.ConversionDiagnostics())
	}
}

func TestResponsesUpstreamTrailingAssistantKeepsToolCalls(t *testing.T) {
	for _, input := range []string{
		`[{"role":"assistant","content":"standalone"}]`,
		`[{"role":"user","content":"hi"},{"type":"function_call","name":"run","call_id":"c","arguments":"{}"}]`,
	} {
		c, _, info := responsesTestContext()
		got, err := ConvertResponsesToGemini(c, info, dto.OpenAIResponsesRequest{Model: info.UpstreamModelName, Input: []byte(input)})
		require.NoError(t, err)
		require.Equal(t, "model", got.Contents[len(got.Contents)-1].Role)
		require.Empty(t, info.ConversionDiagnostics())
	}
}

func TestResponsesUpstreamMediaAliases(t *testing.T) {
	c, _, info := responsesTestContext()
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-3-flash-preview","input":[{"role":"user","content":["hi",{"type":"input_file","file":{"data":"JVBERi0xLjQK","mime_type":"application/pdf"}},{"type":"input_video","video_url":{"url":"AAAA","mime_type":"video/mp4"}},{"type":"input_audio","data":"AAAA","mime_type":"audio/wav"}]}]}`, &req))
	got, err := ConvertResponsesToGemini(c, info, req)
	require.NoError(t, err)
	require.Len(t, got.Contents[0].Parts, 4)
	for i, mime := range []string{"application/pdf", "video/mp4", "audio/wav"} {
		require.Equal(t, mime, got.Contents[0].Parts[i+1].InlineData.MimeType)
	}
}

func TestResponsesUpstreamToolMediaLossAndScalarText(t *testing.T) {
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicyAllow, types.ConversionLossPolicySafe} {
		c, _, info := responsesTestContext()
		info.ChannelOtherSettings.ToolLossPolicy = policy
		var req dto.OpenAIResponsesRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-3-flash-preview","input":[{"type":"function_call","name":"run","call_id":"c","arguments":"{}"},{"type":"function_call_output","call_id":"c","output":[{"type":"input_text","text":"kept"},{"type":"input_audio","input_audio":{"data":"AAAA","format":"wav"}},{"type":"input_file","file_data":"data:application/not-supported;base64,AAAA"}]}]}`, &req))
		got, err := ConvertResponsesToGemini(c, info, req)
		if policy == types.ConversionLossPolicySafe {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		fr := got.Contents[1].Parts[0].FunctionResponse
		require.Empty(t, fr.Parts)
		require.Equal(t, map[string]any{"content": "kept"}, fr.Response)
		require.Len(t, info.ConversionDiagnostics(), 1)
	}
	for _, text := range []string{"false", "0", "true", "plain text"} {
		require.Equal(t, map[string]any{"content": text}, responsesGeminiResponseMap(text))
	}
	require.Equal(t, map[string]any{"result": []any{float64(1)}}, responsesGeminiResponseMap("[1]"))
}

func TestResponsesUpstreamMediaAndFinishJSONAndSSE(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, finish := range []string{"STOP", "MAX_TOKENS", "OTHER", "FUTURE_REASON"} {
			t.Run(fmt.Sprintf("%t/%s", stream, finish), func(t *testing.T) {
				c, recorder, info := responsesTestContext()
				info.IsStream = stream
				payload := `{"candidates":[{"content":{"parts":[{"text":"answer"},{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}},{"inlineData":{"mimeType":"audio/wav","data":"AAAA"}}]},"finishReason":"` + finish + `"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`
				resp := geminiSSE(payload)
				if !stream {
					resp = &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}
				}
				_, err := (&Adaptor{}).DoResponse(c, resp, info)
				require.Nil(t, err)
				result := gjson.Parse(recorder.Body.String())
				if stream {
					events := responseEvents(t, recorder.Body.String())
					result = events[len(events)-1].Get("response")
				}
				require.Len(t, result.Get("output").Array(), 1)
				require.Equal(t, "message", result.Get("output.0.type").String())
				require.Equal(t, "answer\n![image](data:image/png;base64,aW1hZ2U=)\n[media](data:audio/wav;base64,AAAA)", result.Get("output.0.content.0.text").String())
				if finish == "STOP" {
					require.Equal(t, "completed", result.Get("status").String())
				} else {
					require.Equal(t, "incomplete", result.Get("status").String())
					reason := "content_filter"
					if finish == "MAX_TOKENS" {
						reason = "max_output_tokens"
					}
					require.Equal(t, reason, result.Get("incomplete_details.reason").String())
				}
			})
		}
	}
}
