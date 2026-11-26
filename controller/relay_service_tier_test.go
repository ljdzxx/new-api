package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestServiceTierRelayBillingPreservesUpstreamRequest(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprint(allow), func(t *testing.T) {
			var baseQuota int
			var baseBody string
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprint(enabled), func(t *testing.T) {
					db := setupResponsesRelayBillingTest(t)
					var upstreamBody string
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, _ := io.ReadAll(r.Body)
						upstreamBody = string(body)
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"id":"resp_tier","output":[],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":10,"cache_write_tokens":5}}}`)
					}))
					defer server.Close()
					service.InitHttpClient()
					c, recorder, _ := newRelayMockContext(false)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"hello","service_tier":"priority"}`))
					c.Request.Header.Set("Content-Type", "application/json")
					common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
					common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
					common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
					common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AllowServiceTier: allow, ServiceTierBilling: &types.ServiceTierBilling{Enabled: enabled, Rules: []types.ServiceTierBillingRule{{Model: "*", Tier: "Ultrafast", TagColor: "purple", TagType: "solid", Multiplier: 6}}}})
					Relay(c, types.RelayFormatOpenAIResponses)
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					var logs []model.Log
					require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
					require.Len(t, logs, 1, recorder.Body.String())
					if !enabled {
						require.False(t, gjson.Get(recorder.Body.String(), "service_tier").Exists())
						baseQuota, baseBody = logs[0].Quota, upstreamBody
						require.False(t, gjson.Get(logs[0].Other, "service_tier_billing").Exists())
					} else {
						require.Equal(t, "Ultrafast", gjson.Get(recorder.Body.String(), "service_tier").String())
						require.JSONEq(t, baseBody, upstreamBody)
						require.InDelta(t, baseQuota*6, logs[0].Quota, 5) // Final integer truncation happens once.
						require.Equal(t, "Ultrafast", gjson.Get(logs[0].Other, "service_tier_billing.tier").String())
						require.Equal(t, "solid", gjson.Get(logs[0].Other, "service_tier_billing.tag_type").String())
					}
					require.Equal(t, allow, gjson.Get(upstreamBody, "service_tier").Exists())
					var user model.User
					require.NoError(t, db.First(&user, 42).Error)
					require.Equal(t, 1000000-logs[0].Quota, user.Quota)
					var token model.Token
					require.NoError(t, db.First(&token, 77).Error)
					require.Equal(t, user.Quota, token.RemainQuota)
					userLogs, err := model.GetLogByTokenId(77)
					require.NoError(t, err)
					require.Equal(t, enabled, gjson.Get(userLogs[0].Other, "service_tier_billing").Exists())
				})
			}
		})
	}
}

func TestServiceTierChannelValidation(t *testing.T) {
	settings := `{"service_tier_billing":{"enabled":true,"force_service_tier":true,"rules":[{"model":"*","tier":"fast","tag_color":"blue","tag_type":"light","multiplier":2}]},"allow_service_tier":true}`
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, OtherSettings: settings}
	require.NoError(t, validateChannel(channel, false))
	require.True(t, channel.GetOtherSettings().ServiceTierBilling.ForceServiceTier)
	roundTrip, err := common.Marshal(channel.GetOtherSettings())
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(roundTrip, "service_tier_billing.force_service_tier").Bool())
	channel.OtherSettings = strings.Replace(settings, `"model":"*"`, `"model":"gpt-*"`, 1)
	require.Error(t, validateChannel(channel, false))
	channel.Type = constant.ChannelTypeAnthropic
	require.NoError(t, validateChannel(channel, false))
	require.False(t, gjson.Get(channel.OtherSettings, "service_tier_billing").Exists())
	require.True(t, gjson.Get(channel.OtherSettings, "allow_service_tier").Bool())
}

func TestForcedServiceTierRelay(t *testing.T) {
	for _, tc := range []struct {
		name, path                                                          string
		stream, convert, global, passthrough, forceFormat, thinking, failed bool
	}{
		{name: "chat", path: "/v1/chat/completions"},
		{name: "chat SSE", path: "/v1/chat/completions", stream: true},
		{name: "chat formatted", path: "/v1/chat/completions", forceFormat: true},
		{name: "chat formatted SSE", path: "/v1/chat/completions", stream: true, forceFormat: true},
		{name: "chat thinking SSE", path: "/v1/chat/completions", stream: true, thinking: true},
		{name: "chat global passthrough", path: "/v1/chat/completions", stream: true, global: true},
		{name: "chat channel passthrough", path: "/v1/chat/completions", passthrough: true},
		{name: "responses", path: "/v1/responses"},
		{name: "responses SSE", path: "/v1/responses", stream: true},
		{name: "responses failed SSE", path: "/v1/responses", stream: true, failed: true},
		{name: "responses global passthrough", path: "/v1/responses", global: true},
		{name: "responses channel passthrough SSE", path: "/v1/responses", stream: true, passthrough: true},
		{name: "compact", path: "/v1/responses/compact"},
		{name: "compact passthrough", path: "/v1/responses/compact", passthrough: true},
		{name: "chat via responses", path: "/v1/chat/completions", convert: true},
		{name: "chat via responses SSE", path: "/v1/chat/completions", convert: true, stream: true},
	} {
		for _, allow := range []bool{false, true} {
			for _, force := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/allow=%t/force=%t", tc.name, allow, force), func(t *testing.T) {
					db := setupResponsesRelayBillingTest(t)
					settings := model_setting.GetGlobalSettings()
					original := *settings
					t.Cleanup(func() { *settings = original })
					settings.PassThroughRequestEnabled = tc.global
					settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: tc.convert, AllChannels: true, ModelPatterns: []string{"^gpt-4o-mini$"}}
					type capturedRequest struct {
						body   []byte
						path   string
						length int64
						err    error
					}
					captured := make(chan capturedRequest, 1)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := io.ReadAll(r.Body)
						captured <- capturedRequest{body, r.URL.Path, r.ContentLength, err}
						if !tc.stream {
							w.Header().Set("Content-Type", "application/json")
							switch r.URL.Path {
							case "/v1/chat/completions":
								fmt.Fprint(w, `{"id":"chat_1","object":"chat.completion","service_tier":"default","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`)
							case "/v1/responses/compact":
								fmt.Fprint(w, `{"id":"compact_1","object":"response.compaction","output":[],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}`)
							default:
								fmt.Fprint(w, `{"id":"resp_1","object":"response","model":"gpt-4o","service_tier":null,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}`)
							}
							return
						}
						w.Header().Set("Content-Type", "text/event-stream")
						var events []string
						if r.URL.Path == "/v1/responses" {
							events = []string{
								`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","model":"gpt-4o","service_tier":null}}`,
								`{"type":"response.in_progress","sequence_number":1,"response":{"id":"resp_1","service_tier":"default"}}`,
								`{"type":"response.output_text.delta","sequence_number":2,"delta":"hello"}`,
								`{"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","model":"gpt-4o","output":[],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}}`,
							}
							if tc.failed {
								events[3] = `{"type":"response.failed","response":{"id":"resp_1","service_tier":"default","error":{"type":"server_error","message":"test failure"},"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}}`
							}
						} else {
							events = []string{
								`{"id":"chat_1","object":"chat.completion.chunk","model":"gpt-4o","service_tier":"default","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"thinking"}}]}`,
								`{"id":"chat_1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
								`{"id":"chat_1","object":"chat.completion.chunk","model":"gpt-4o","service_tier":"default","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`,
								`[DONE]`,
							}
						}
						for _, event := range events {
							fmt.Fprintf(w, "data: %s\n\n", event)
						}
					}))
					defer upstream.Close()
					service.InitHttpClient()
					c, recorder, _ := newRelayMockContext(tc.stream)
					body := fmt.Sprintf(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}],"input":"hello","stream":%t,"service_tier":"priority"}`, tc.stream)
					c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
					c.Request.Header.Set("Content-Type", "application/json")
					common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
					common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
					common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.passthrough, ForceFormat: tc.forceFormat, ThinkingToContent: tc.thinking})
					common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]interface{}{"service_tier": "flex"})
					c.Set("model_mapping", `{"gpt-4o-mini":"gpt-4o"}`)
					common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AllowServiceTier: allow, ServiceTierBilling: &types.ServiceTierBilling{
						Enabled: true, ForceServiceTier: force, Rules: []types.ServiceTierBillingRule{
							{Model: "*", Tier: "fallback", TagColor: "blue", TagType: "light", Multiplier: 2},
							{Model: "gpt-4o-mini", Tier: "Ultrafast", TagColor: "blue", TagType: "light", Multiplier: 6},
						},
					}})
					format := types.RelayFormatOpenAI
					if tc.path == "/v1/responses" {
						format = types.RelayFormatOpenAIResponses
					}
					if tc.path == "/v1/responses/compact" {
						format = types.RelayFormatOpenAIResponsesCompaction
					}
					Relay(c, format)
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					var got capturedRequest
					select {
					case got = <-captured:
					default:
						t.Fatal("request did not reach upstream")
					}
					require.NoError(t, got.err)
					if force {
						require.Equal(t, "Ultrafast", gjson.GetBytes(got.body, "service_tier").String())
					} else if !allow && (tc.global || tc.passthrough || tc.convert) {
						require.False(t, gjson.GetBytes(got.body, "service_tier").Exists())
					} else if tc.global || tc.passthrough {
						require.Equal(t, "priority", gjson.GetBytes(got.body, "service_tier").String())
					} else {
						require.Equal(t, "flex", gjson.GetBytes(got.body, "service_tier").String())
					}
					require.Equal(t, "gpt-4o", gjson.GetBytes(got.body, "model").String())
					require.EqualValues(t, len(got.body), got.length)
					if tc.convert {
						require.Equal(t, "/v1/responses", got.path)
					} else {
						require.Equal(t, tc.path, got.path)
					}
					if !tc.stream {
						require.Equal(t, "Ultrafast", gjson.Get(recorder.Body.String(), "service_tier").String(), recorder.Body.String())
					} else {
						chunks, tierChunks, usageChunks := 0, 0, 0
						for _, line := range strings.Split(recorder.Body.String(), "\n") {
							if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
								continue
							}
							data := strings.TrimPrefix(line, "data: ")
							require.True(t, gjson.Valid(data), data)
							chunks++
							if format == types.RelayFormatOpenAI {
								require.Equal(t, "Ultrafast", gjson.Get(data, "service_tier").String(), data)
								tierChunks++
								if gjson.Get(data, "usage.total_tokens").Int() > 0 {
									usageChunks++
								}
							} else if gjson.Get(data, "response").IsObject() {
								require.Equal(t, "Ultrafast", gjson.Get(data, "response.service_tier").String(), data)
								tierChunks++
							} else {
								require.False(t, gjson.Get(data, "service_tier").Exists(), data)
							}
						}
						require.GreaterOrEqual(t, chunks, 3)
						require.GreaterOrEqual(t, tierChunks, 3)
						if format == types.RelayFormatOpenAI {
							require.Equal(t, 1, usageChunks)
						}
						if tc.failed {
							require.Contains(t, recorder.Body.String(), "event: response.failed")
						}
					}
					var logs []model.Log
					require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
					require.Len(t, logs, 1)
					require.Equal(t, "Ultrafast", gjson.Get(logs[0].Other, "service_tier_billing.tier").String())
					require.Equal(t, int64(6), gjson.Get(logs[0].Other, "service_tier_billing.multiplier").Int())
					require.Positive(t, logs[0].Quota)
				})
			}
		}
	}
}
