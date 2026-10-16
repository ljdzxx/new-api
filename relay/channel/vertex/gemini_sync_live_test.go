package vertex

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestGeminiSyncLive is opt-in and sends only synthetic prompts to official
// endpoints. Never print keys, request URLs, or upstream error bodies.
// Filter /gemini or /vertex to run one provider. See gemini_sync_261014.md.
func TestGeminiSyncLive(t *testing.T) {
	if os.Getenv("GEMINI_SYNC_LIVE") != "1" {
		t.Skip("set GEMINI_SYNC_LIVE=1 and provider credentials to run real requests")
	}
	for _, provider := range []string{"gemini", "vertex"} {
		t.Run(provider, func(t *testing.T) {
			prefix := strings.ToUpper(provider) + "_SYNC_"
			key, model := os.Getenv(prefix+"API_KEY"), os.Getenv(prefix+"MODEL")
			require.NotEmpty(t, key, "missing %sAPI_KEY", prefix)
			require.Regexp(t, regexp.MustCompile(`^[a-zA-Z0-9._-]+$`), model, "set a valid %sMODEL", prefix)
			for _, scenario := range []string{"schema", "thinking", "partial_tools", "grounding_stream"} {
				t.Run(scenario, func(t *testing.T) {
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
					stream := scenario == "partial_tools" || scenario == "grounding_stream"
					channelType := constant.ChannelTypeGemini
					if provider == "vertex" {
						channelType = constant.ChannelTypeVertexAi
					}
					info := &relaycommon.RelayInfo{OriginModelName: model, IsStream: stream,
						RelayFormat: types.RelayFormatOpenAI, StartTime: time.Now(),
						ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: model, ChannelType: channelType,
							ApiKey: key, ChannelOtherSettings: dto.ChannelOtherSettings{
								VertexKeyType: dto.VertexKeyTypeAPIKey, VertexProjectID: os.Getenv("VERTEX_SYNC_PROJECT_ID"),
							}}}
					request := dto.GeneralOpenAIRequest{Model: model, MaxTokens: common.GetPointer(uint(1024)),
						Messages: []dto.Message{{Role: "user", Content: "Return the integer 3 as JSON: {\"x\":3}."}}}
					switch scenario {
					case "schema":
						request.ResponseFormat = &dto.ResponseFormat{Type: "json_schema", JsonSchema: []byte(`{"name":"result","schema":{"type":"object","properties":{"x":{"type":"integer","const":3}},"required":["x"]}}`)}
					case "thinking":
						request.ReasoningEffort = "low"
					case "partial_tools":
						request.Messages[0].Content = "Call echo with text containing the numbers one through fifty spelled out in English."
						request.Tools = []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "echo", Parameters: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []any{"text"}}}}}
						request.ToolChoice = "required"
					case "grounding_stream":
						request.Messages[0].Content = "Search the web for the latest stable Go release on go.dev and cite the source in one sentence."
						request.Tools = []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "googleSearch"}}}
					}
					converted, err := gemini.CovertOpenAI2Gemini(c, request, info)
					require.NoError(t, err)
					if scenario == "partial_tools" {
						converted.ToolConfig.FunctionCallingConfig.StreamFunctionCallArguments = common.GetPointer(true)
					}
					payload, err := common.Marshal(converted)
					require.NoError(t, err)
					action := "generateContent"
					if stream {
						action = "streamGenerateContent?alt=sse"
					}
					endpoint := "https://generativelanguage.googleapis.com/v1beta/models/" + model + ":" + action
					if provider == "vertex" {
						region := os.Getenv("VERTEX_SYNC_REGION")
						require.Regexp(t, regexp.MustCompile(`^[a-z0-9-]+$`), region, "set VERTEX_SYNC_REGION")
						regionJSON, err := common.Marshal(map[string]string{"default": region})
						require.NoError(t, err)
						info.ApiVersion = string(regionJSON)
						adaptor := &Adaptor{}
						adaptor.Init(info)
						endpoint, err = adaptor.GetRequestURL(info)
						require.NoError(t, err)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
					defer cancel()
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
					if err != nil {
						t.Fatal("invalid request configuration (URL and credentials omitted)")
					}
					req.Header.Set("Content-Type", "application/json")
					if provider == "gemini" {
						req.Header.Set("x-goog-api-key", key)
					}
					client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
					resp, err := client.Do(req)
					if err != nil {
						t.Fatal("upstream transport failed (URL and credentials omitted)")
					}
					defer resp.Body.Close()
					require.Equal(t, http.StatusOK, resp.StatusCode, "upstream rejected %s; body omitted", scenario)
					wire, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
					require.NoError(t, err)
					if scenario == "partial_tools" {
						require.Contains(t, string(wire), "partialArgs", "provider did not emit partial arguments; live coverage not met")
					}
					resp.Body = io.NopCloser(bytes.NewReader(wire))
					var usage *dto.Usage
					var apiErr *types.NewAPIError
					if stream {
						usage, apiErr = gemini.GeminiChatStreamHandler(c, info, resp)
					} else {
						usage, apiErr = gemini.GeminiChatHandler(c, info, resp)
					}
					require.Nil(t, apiErr)
					require.NotNil(t, usage)
					require.Positive(t, usage.TotalTokens)
					require.NotEmpty(t, recorder.Body.String())
					if scenario == "schema" {
						require.EqualValues(t, 3, gjson.Get(gjson.Get(recorder.Body.String(), "choices.0.message.content").String(), "x").Int())
					}
					if scenario == "partial_tools" {
						require.Contains(t, recorder.Body.String(), "tool_calls")
					}
					if scenario == "grounding_stream" {
						require.NotNil(t, info.ResponsesUsageInfo)
						counter := info.ResponsesUsageInfo.BuiltInTools[relaycommon.GeminiGoogleSearchQueryTool]
						require.NotNil(t, counter)
						require.Positive(t, counter.CallCount)
						require.Contains(t, recorder.Body.String(), "url_citation", "provider did not return grounded citations")
					}
				})
			}
		})
	}
}
