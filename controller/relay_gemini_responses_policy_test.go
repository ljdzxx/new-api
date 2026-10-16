package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiResponsesConversionPolicySettlement(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		for _, stream := range []bool{false, true} {
			for _, policy := range []types.ConversionLossPolicy{"", types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
				t.Run(fmt.Sprintf("%d/stream=%t/%s", channelType, stream, policy), func(t *testing.T) {
					db := setupResponsesRelayBillingTest(t)
					var calls atomic.Int32
					bodies := make(chan string, 1)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						body, _ := io.ReadAll(r.Body)
						bodies <- string(body)
						payload := `{"candidates":[{"content":{"parts":[{"functionCall":{"id":"call_new","name":"fs__read","args":{"path":"file.txt"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120}}`
						if stream {
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprint(w, "data: "+payload+"\n\n")
						} else {
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprint(w, payload)
						}
					}))
					defer upstream.Close()
					service.InitHttpClient()
					c, recorder, _ := newRelayMockContext(stream)
					body := fmt.Sprintf(`{"model":"gpt-4o-mini","stream":%t,"tools":[{"type":"tool_search"}],"input":[{"role":"user","content":"hello"},{"type":"additional_tools","tools":[{"type":"namespace","name":"fs","tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]}]},{"type":"web_search_call","status":"completed","action":{"type":"search","queries":["q"]}},{"role":"assistant","content":"partial answer"}]}`, stream)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
					c.Request.Header.Set("Content-Type", "application/json")
					c.Set("model_mapping", `{"gpt-4o-mini":"gemini-3-flash-preview"}`)
					common.SetContextKey(c, constant.ContextKeyChannelType, channelType)
					common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
					common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{VertexKeyType: dto.VertexKeyTypeAPIKey, ToolLossPolicy: policy})
					common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
					common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
					Relay(c, types.RelayFormatOpenAIResponses)
					var logs []model.Log
					require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
					var user model.User
					var token model.Token
					require.NoError(t, db.First(&user, 42).Error)
					require.NoError(t, db.First(&token, 77).Error)
					if policy != "" {
						require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
						require.Zero(t, calls.Load())
						require.Empty(t, logs)
						assertGeminiResponsesRefund(t, db)
						return
					}
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					require.EqualValues(t, 1, calls.Load())
					converted := <-bodies
					require.Len(t, gjson.Get(converted, "contents").Array(), 1)
					require.Equal(t, "user", gjson.Get(converted, "contents.0.role").String())
					require.Equal(t, "fs__read", gjson.Get(converted, "tools.0.functionDeclarations.0.name").String())
					require.NotContains(t, converted, "partial answer")
					require.NotContains(t, converted, "tool_search")
					result := gjson.Parse(recorder.Body.String())
					if stream {
						for _, line := range strings.Split(recorder.Body.String(), "\n") {
							if strings.HasPrefix(line, "data: ") {
								event := gjson.Parse(strings.TrimPrefix(line, "data: "))
								if event.Get("type").String() == "response.completed" {
									result = event.Get("response")
								}
							}
						}
					}
					require.Equal(t, "fs", result.Get("output.0.namespace").String())
					require.Equal(t, "read", result.Get("output.0.name").String())
					require.Equal(t, "call_new", result.Get("output.0.call_id").String())
					require.Len(t, logs, 1)
					require.Equal(t, 100, logs[0].PromptTokens)
					require.Equal(t, 20, logs[0].CompletionTokens)
					require.Positive(t, logs[0].Quota)
					require.Equal(t, 1000000-logs[0].Quota, user.Quota)
					require.Equal(t, user.Quota, token.RemainQuota)
					diagnostics := gjson.Get(logs[0].Other, "admin_info.conversion_diagnostics").Array()
					require.GreaterOrEqual(t, len(diagnostics), 5)
					require.False(t, gjson.Get(logs[0].Other, "conversion_diagnostics").Exists())
				})
			}
		}
	}
}
