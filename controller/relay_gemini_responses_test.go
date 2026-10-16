package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

// Exercise the real controller -> request conversion -> HTTP -> Responses SSE
// -> BillingSession chain against an isolated DB and a local fake upstream.
func TestGeminiResponsesRelaySettlement(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		for _, mode := range []string{"completed", "partial_failure", "client_disconnect", "channel_passthrough", "global_passthrough", "namespaced_tools"} {
			t.Run(fmt.Sprintf("%d/%s", channelType, mode), func(t *testing.T) {
				db := setupResponsesRelayBillingTest(t)
				var calls atomic.Int32
				bodies := make(chan string, 4)
				paths := make(chan string, 4)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					body, _ := io.ReadAll(r.Body)
					bodies <- string(body)
					paths <- r.URL.Path
					w.Header().Set("Content-Type", "text/event-stream")
					if mode == "client_disconnect" {
						fmt.Fprint(w, "data: "+`{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true}]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":15,"thoughtsTokenCount":5,"totalTokenCount":120}}`+"\n\n")
						return
					}
					fmt.Fprint(w, "data: "+`{"candidates":[{"content":{"parts":[{"text":"answer"}]}}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120}}`+"\n\n")
					if mode == "partial_failure" {
						fmt.Fprint(w, "data: {broken\n\n")
					} else {
						fmt.Fprint(w, "data: "+`{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120}}`+"\n\n")
					}
				}))
				defer upstream.Close()
				oldTimeout, oldRetries := common.RelayTimeout, common.RetryTimes
				common.RelayTimeout, common.RetryTimes = 5, 3
				service.InitHttpClient()
				t.Cleanup(func() { common.RelayTimeout, common.RetryTimes = oldTimeout, oldRetries; service.InitHttpClient() })
				settings := model_setting.GetGlobalSettings()
				oldPass := settings.PassThroughRequestEnabled
				settings.PassThroughRequestEnabled = mode == "global_passthrough"
				t.Cleanup(func() { settings.PassThroughRequestEnabled = oldPass })
				c, recorder, _ := newRelayMockContext(true)
				requestBody := `{"model":"gpt-4o-mini","input":"hello","stream":true}`
				if mode == "namespaced_tools" {
					requestBody = `{"model":"gpt-4o-mini","input":"hello","stream":true,"tools":[{"type":"namespace","name":"fs","tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]}]}`
				}
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(requestBody))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Set("model_mapping", `{"gpt-4o-mini":"gemini-3-flash-preview"}`)
				common.SetContextKey(c, constant.ContextKeyChannelType, channelType)
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: mode == "channel_passthrough"})
				common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{VertexKeyType: dto.VertexKeyTypeAPIKey})
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
				common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
				if mode == "client_disconnect" {
					ctx, cancel := context.WithCancel(c.Request.Context())
					defer cancel()
					c.Request = c.Request.WithContext(ctx)
					c.Writer = &responsesCancelWriter{ResponseWriter: c.Writer, cancel: cancel}
				}
				Relay(c, types.RelayFormatOpenAIResponses)
				require.EqualValues(t, 1, calls.Load(), recorder.Body.String())
				upstreamBody := <-bodies
				require.Equal(t, "hello", gjson.Get(upstreamBody, "contents.0.parts.0.text").String())
				if mode == "namespaced_tools" {
					require.Equal(t, "fs__read", gjson.Get(upstreamBody, "tools.0.functionDeclarations.0.name").String())
				}
				require.Contains(t, <-paths, ":streamGenerateContent")
				require.Equal(t, http.StatusOK, recorder.Code)
				if mode == "client_disconnect" {
					require.NotContains(t, recorder.Body.String(), "response.completed")
					require.NotContains(t, recorder.Body.String(), "response.failed")
				} else if mode == "partial_failure" {
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed\n"))
					require.NotContains(t, recorder.Body.String(), "response.completed")
				} else {
					require.Contains(t, recorder.Body.String(), "event: response.completed\n")
				}
				var logs []model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				require.Len(t, logs, 1)
				require.Equal(t, 100, logs[0].PromptTokens)
				require.Equal(t, 20, logs[0].CompletionTokens)
				require.Positive(t, logs[0].Quota)
				require.Equal(t, mode == "client_disconnect", gjson.Get(logs[0].Other, "stream_canceled_by_downstream").Bool())
				var user model.User
				var token model.Token
				require.NoError(t, db.First(&user, 42).Error)
				require.NoError(t, db.First(&token, 77).Error)
				require.Equal(t, 1000000-logs[0].Quota, user.Quota)
				require.Equal(t, user.Quota, token.RemainQuota)
			})
		}
	}
}

func TestGeminiResponsesInvalidRequestRefunds(t *testing.T) {
	for _, input := range []string{`"previous_response_id":"resp_old"`, `"max_output_tokens":18446744073686646784`} {
		t.Run(input, func(t *testing.T) {
			db := setupResponsesRelayBillingTest(t)
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
			defer upstream.Close()
			c, recorder, _ := newRelayMockContext(false)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"hello",`+input+`}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("model_mapping", `{"gpt-4o-mini":"gemini-3-flash-preview"}`)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeGemini)
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
			Relay(c, types.RelayFormatOpenAIResponses)
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			require.Zero(t, calls.Load())
			assertGeminiResponsesRefund(t, db)
			var count int64
			require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestGeminiResponsesBlockedBeforeFirstEvent(t *testing.T) {
	db := setupResponsesRelayBillingTest(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: "+`{"promptFeedback":{"blockReason":"SAFETY"}}`+"\n\n")
	}))
	defer upstream.Close()
	service.InitHttpClient()
	c, recorder, _ := newRelayMockContext(true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"hello","stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("model_mapping", `{"gpt-4o-mini":"gemini-3-flash-preview"}`)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeGemini)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
	Relay(c, types.RelayFormatOpenAIResponses)
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.True(t, gjson.Valid(recorder.Body.String()))
	require.NotContains(t, recorder.Body.String(), "response.completed")
	assertGeminiResponsesRefund(t, db)
}

func assertGeminiResponsesRefund(t *testing.T, db *gorm.DB) {
	t.Helper()
	// BillingSession.Refund deliberately schedules wallet and token updates.
	// Wait for both before asserting and before test cleanup replaces the DB.
	require.Eventually(t, func() bool {
		var user model.User
		var token model.Token
		return db.First(&user, 42).Error == nil && db.First(&token, 77).Error == nil && user.Quota == 1000000 && token.RemainQuota == 1000000
	}, 2*time.Second, 10*time.Millisecond)
}
