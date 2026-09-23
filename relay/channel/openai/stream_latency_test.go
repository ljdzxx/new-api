package openai

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The upstream cannot send its next event until the client has received the
// current one. This checks actual HTTP flushing without relying on short sleeps.
func TestStreamForwardsContentBeforeNextUpstreamEvent(t *testing.T) {
	for _, tc := range []struct {
		name            string
		format          types.RelayFormat
		forceFormat     bool
		responses       bool
		deltaTemplate   string
		thinking        bool
		terminalContent bool
		completions     bool
	}{
		{name: "chat", format: types.RelayFormatOpenAI},
		{name: "chat force format", format: types.RelayFormatOpenAI, forceFormat: true},
		{name: "reasoning", format: types.RelayFormatOpenAI, deltaTemplate: `{"reasoning_content":%q}`},
		{name: "thinking to content", format: types.RelayFormatOpenAI, deltaTemplate: `{"reasoning_content":%q}`, thinking: true},
		{name: "tool arguments", format: types.RelayFormatOpenAI, deltaTemplate: `{"tool_calls":[{"index":0,"function":{"arguments":%q}}]}`},
		{name: "audio", format: types.RelayFormatOpenAI, deltaTemplate: `{"audio":{"data":%q}}`},
		{name: "legacy completions", format: types.RelayFormatOpenAI, completions: true},
		{name: "chat to claude", format: types.RelayFormatClaude},
		{name: "terminal content to claude", format: types.RelayFormatClaude, terminalContent: true},
		{name: "chat to gemini", format: types.RelayFormatGemini},
		{name: "responses", format: types.RelayFormatOpenAIResponses, responses: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contents := []string{"first", "second"}
			if tc.terminalContent {
				contents = contents[:1]
			}
			next := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, content := range contents {
					deltaTemplate := tc.deltaTemplate
					if deltaTemplate == "" {
						deltaTemplate = `{"content":%q}`
					}
					finishReason := "null"
					if tc.terminalContent {
						finishReason = `"stop"`
					}
					data := fmt.Sprintf(`{"id":"chatcmpl_latency","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":%s,"finish_reason":%s}]}`, fmt.Sprintf(deltaTemplate, content), finishReason)
					if tc.completions {
						data = fmt.Sprintf(`{"id":"cmpl_latency","choices":[{"text":%q,"finish_reason":null}]}`, content)
					}
					if tc.responses {
						data = fmt.Sprintf(`{"type":"response.output_text.delta","delta":%q}`, content)
					}
					fmt.Fprintf(w, "data: %s\n\n", data)
					w.(http.Flusher).Flush()
					select {
					case <-next:
					case <-r.Context().Done():
						return
					}
				}
				if tc.responses {
					fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n")
				} else {
					fmt.Fprint(w, "data: {\"id\":\"chatcmpl_latency\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
					fmt.Fprint(w, "data: {\"id\":\"chatcmpl_latency\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\ndata: [DONE]\n\n")
				}
			}))
			defer upstream.Close()

			type result struct {
				usage *dto.Usage
				err   *types.NewAPIError
			}
			finished := make(chan result, 1)
			downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream.URL, nil)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
				c, _ := gin.CreateTestContext(w)
				c.Request = r
				info := &relaycommon.RelayInfo{
					RelayFormat:        tc.format,
					RelayMode:          relayconstant.RelayModeChatCompletions,
					DisablePing:        true,
					ShouldIncludeUsage: true,
					PriceData:          types.PriceData{GlobalModelRatio: 1},
					ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o", ChannelSetting: dto.ChannelSettings{ForceFormat: tc.forceFormat}},
					ClaudeConvertInfo:  &relaycommon.ClaudeConvertInfo{},
				}
				info.ChannelSetting.ThinkingToContent = tc.thinking
				info.ThinkingContentInfo.IsFirstThinkingContent = true
				if tc.completions {
					info.RelayMode = relayconstant.RelayModeCompletions
				}
				var got result
				if tc.responses {
					got.usage, got.err = OaiResponsesStreamHandler(c, info, resp)
				} else {
					got.usage, got.err = OaiStreamHandler(c, info, resp)
				}
				finished <- got
			}))
			defer downstream.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, downstream.URL, nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err, "first event must be flushed while upstream waits for the client")
			defer resp.Body.Close()
			require.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))
			scanner := bufio.NewScanner(resp.Body)
			var body strings.Builder
			for _, content := range contents {
				found := false
				for scanner.Scan() {
					line := scanner.Text()
					body.WriteString(line + "\n")
					if strings.Contains(line, content) {
						found = true
						break
					}
				}
				require.NoError(t, scanner.Err())
				require.True(t, found, "%s must arrive before the next upstream event", content)
				select {
				case next <- struct{}{}:
				case <-ctx.Done():
					t.Fatal("upstream did not resume after client received content")
				}
			}
			for scanner.Scan() {
				body.WriteString(scanner.Text() + "\n")
			}
			require.NoError(t, scanner.Err())
			for _, content := range contents {
				require.Equal(t, 1, strings.Count(body.String(), content))
			}
			if tc.format == types.RelayFormatClaude {
				require.Equal(t, 1, strings.Count(body.String(), "event: message_start\n"))
				require.Equal(t, 1, strings.Count(body.String(), "event: message_stop\n"))
				var finalUsage gjson.Result
				for _, line := range strings.Split(body.String(), "\n") {
					if strings.HasPrefix(line, "data:") {
						payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
						if gjson.Get(payload, "type").String() == "message_delta" {
							finalUsage = gjson.Get(payload, "usage")
						}
					}
				}
				require.EqualValues(t, 10, finalUsage.Get("input_tokens").Int())
				require.EqualValues(t, 2, finalUsage.Get("output_tokens").Int())
			}
			select {
			case got := <-finished:
				require.Nil(t, got.err)
				require.Equal(t, 12, got.usage.TotalTokens)
			case <-ctx.Done():
				t.Fatal("relay did not finish")
			}
		})
	}
}

func TestOaiStreamUsageAndTerminalContent(t *testing.T) {
	for _, includeUsage := range []bool{false, true} {
		t.Run(fmt.Sprintf("include_usage=%t", includeUsage), func(t *testing.T) {
			for _, tail := range []string{
				`{"id":"chatcmpl_usage","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
				`{"id":"chatcmpl_usage","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
			} {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				body := "data: " + tail + "\n\ndata: [DONE]\n\n"
				info := &relaycommon.RelayInfo{
					RelayFormat:        types.RelayFormatOpenAI,
					RelayMode:          relayconstant.RelayModeChatCompletions,
					ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"},
					DisablePing:        true,
					ShouldIncludeUsage: includeUsage,
					PriceData:          types.PriceData{GlobalModelRatio: 1},
				}
				usage, apiErr := OaiStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
				require.Nil(t, apiErr)
				require.Equal(t, 12, usage.TotalTokens)
				if includeUsage || strings.Contains(tail, "tool_calls") {
					require.Equal(t, 1, strings.Count(recorder.Body.String(), tail))
				} else {
					require.NotContains(t, recorder.Body.String(), tail)
				}
				require.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
			}
		})
	}
}

func TestOaiStreamRetainsAudioUsageBeforeFinalChunk(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := "data: " + `{"id":"chatcmpl_audio","model":"gpt-4o-audio-preview","choices":[{"index":0,"delta":{"audio":{"data":"audio_data"}}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"prompt_tokens_details":{"audio_tokens":7},"completion_tokens_details":{"audio_tokens":2}}}` + "\n\n" +
		"data: " + `{"id":"chatcmpl_audio","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
	info := &relaycommon.RelayInfo{
		RelayFormat:        types.RelayFormatOpenAI,
		RelayMode:          relayconstant.RelayModeChatCompletions,
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o-audio-preview"},
		DisablePing:        true,
		ShouldIncludeUsage: true,
		PriceData:          types.PriceData{GlobalModelRatio: 2},
	}
	usage, apiErr := OaiStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
	require.Nil(t, apiErr)
	require.Equal(t, 10, usage.PromptTokens)
	require.Equal(t, 2, usage.CompletionTokens)
	require.Equal(t, 7, usage.PromptTokensDetails.AudioTokens)
	require.Equal(t, 2, usage.CompletionTokenDetails.AudioTokens)
	require.NotNil(t, usage.BillingUsage)
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "audio_data"))
	require.Equal(t, 1, strings.Count(recorder.Body.String(), `"usage"`))
	firstData := strings.TrimPrefix(strings.Split(recorder.Body.String(), "\n")[0], "data: ")
	require.EqualValues(t, 20, gjson.Get(firstData, "usage.prompt_tokens").Int())
	require.EqualValues(t, 4, gjson.Get(firstData, "usage.completion_tokens").Int())
}

func TestOaiStreamConvertedTerminalContentIsNotReplayed(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
		for _, upstreamUsage := range []string{"null", `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}`} {
			t.Run(string(format)+"/usage="+upstreamUsage, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				body := fmt.Sprintf("data: {\"id\":\"chatcmpl_terminal\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"terminal_content\"},\"finish_reason\":\"stop\"}],\"usage\":%s}\n\ndata: [DONE]\n\n", upstreamUsage)
				info := &relaycommon.RelayInfo{
					RelayFormat:       format,
					RelayMode:         relayconstant.RelayModeChatCompletions,
					ChannelMeta:       &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"},
					ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{},
					DisablePing:       true,
					PriceData:         types.PriceData{GlobalModelRatio: 1},
				}
				_, apiErr := OaiStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
				require.Nil(t, apiErr)
				require.Equal(t, 1, strings.Count(recorder.Body.String(), "terminal_content"))
				if format == types.RelayFormatClaude {
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: message_start\n"))
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: message_stop\n"))
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: message_delta\n"))
				} else {
					require.Equal(t, 1, strings.Count(recorder.Body.String(), `"finishReason":"STOP"`))
				}
			})
		}
	}
}
