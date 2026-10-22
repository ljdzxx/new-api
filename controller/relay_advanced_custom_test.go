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

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type geminiCancelWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
}

func (w *geminiCancelWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if strings.Contains(string(data), `"candidates"`) {
		w.cancel()
	}
	return n, err
}
func (w *geminiCancelWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }

func TestAdvancedCustomGeminiRelaySettlement(t *testing.T) {
	for _, mode := range []string{"json", "completed", "incomplete", "partial_failure", "early_failure", "missing_terminal", "client_disconnect", "global_passthrough", "safe_reject", "explicit_zero"} {
		t.Run(mode, func(t *testing.T) {
			db := setupResponsesRelayBillingTest(t)
			var calls atomic.Int32
			var requestBody string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				data, _ := io.ReadAll(r.Body)
				requestBody = string(data)
				if r.URL.Path != "/v1/responses" {
					w.WriteHeader(404)
					return
				}
				require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
				terminal := `{"id":"r","status":"completed","output":[{"type":"message","id":"m","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}`
				if mode == "explicit_zero" {
					terminal = strings.Replace(terminal, `"input_tokens":100,"output_tokens":20,"total_tokens":120`, `"input_tokens":0,"output_tokens":0,"total_tokens":0`, 1)
				}
				if mode == "json" {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, terminal)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if mode == "early_failure" {
					fmt.Fprint(w, `data: {"type":"response.failed","response":{"status":"failed"}}`+"\n\n")
					return
				}
				fmt.Fprint(w, `data: {"type":"response.output_text.delta","item_id":"m","content_index":0,"delta":"hello"}`+"\n\n")
				if mode == "missing_terminal" || mode == "client_disconnect" {
					return
				}
				if mode == "partial_failure" {
					fmt.Fprint(w, `data: {"type":"response.failed","response":{"status":"failed","usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}}`+"\n\n")
					return
				}
				event := "response.completed"
				if mode == "incomplete" {
					event = "response.incomplete"
					terminal = strings.Replace(terminal, `"status":"completed"`, `"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}`, 1)
				}
				fmt.Fprint(w, `data: {"type":"`+event+`","response":`+terminal+"}\n\n")
			}))
			defer upstream.Close()
			service.InitHttpClient()
			settings := model_setting.GetGlobalSettings()
			old := settings.PassThroughRequestEnabled
			settings.PassThroughRequestEnabled = mode == "global_passthrough"
			t.Cleanup(func() { settings.PassThroughRequestEnabled = old })
			c, recorder, _ := newRelayMockContext(mode != "json")
			action := "generateContent"
			if mode != "json" {
				action = "streamGenerateContent"
			}
			body := `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`
			if mode == "safe_reject" {
				body = `{"contents":[{"parts":[{"text":"hello"},{"inlineData":{"mimeType":"audio/wav","data":"AAAA"}}]}]}`
			}
			c.Request = httptest.NewRequest("POST", "/v1beta/models/gpt-4o-mini:"+action, strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAdvancedCustom)
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: true})
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
			other := dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1beta/models/{model}:generateContent", UpstreamPath: "/v1/responses", Converter: dto.AdvancedCustomGeminiToResponses}}}}
			if mode == "safe_reject" {
				other.ToolLossPolicy = types.ConversionLossPolicySafe
			}
			common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, other)
			common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
			if mode == "client_disconnect" {
				ctx, cancel := context.WithCancel(c.Request.Context())
				defer cancel()
				c.Request = c.Request.WithContext(ctx)
				c.Writer = &geminiCancelWriter{ResponseWriter: c.Writer, cancel: cancel}
			}
			Relay(c, types.RelayFormatGemini)
			if mode == "safe_reject" {
				require.Zero(t, calls.Load())
				require.Equal(t, 400, recorder.Code)
			} else {
				require.EqualValues(t, 1, calls.Load())
				require.Equal(t, "hello", gjson.Get(requestBody, "input.0.content.0.text").String())
				require.False(t, gjson.Get(requestBody, "contents").Exists())
			}
			var logs []model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			if mode == "early_failure" || mode == "safe_reject" {
				assertGeminiResponsesRefund(t, db)
				require.Empty(t, logs)
				return
			}
			require.Len(t, logs, 1, recorder.Body.String())
			if mode == "explicit_zero" {
				require.Zero(t, logs[0].Quota)
				assertGeminiResponsesRefund(t, db)
				return
			}
			require.Positive(t, logs[0].Quota)
			require.Equal(t, mode == "client_disconnect", gjson.Get(logs[0].Other, "stream_canceled_by_downstream").Bool())
			var user model.User
			var token model.Token
			require.NoError(t, db.First(&user, 42).Error)
			require.NoError(t, db.First(&token, 77).Error)
			require.Equal(t, 1000000-logs[0].Quota, user.Quota)
			require.Equal(t, user.Quota, token.RemainQuota)
			if mode == "partial_failure" || mode == "missing_terminal" || mode == "client_disconnect" {
				require.NotContains(t, recorder.Body.String(), "finishReason")
				if mode != "client_disconnect" {
					frames := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n\n")
					for _, frame := range frames {
						require.True(t, strings.HasPrefix(frame, "data: "), frame)
						require.True(t, gjson.Valid(strings.TrimPrefix(frame, "data: ")), frame)
					}
					require.True(t, gjson.Get(strings.TrimPrefix(frames[len(frames)-1], "data: "), "error").Exists())
				}
			} else {
				require.Contains(t, recorder.Body.String(), "finishReason")
				require.Equal(t, 100, logs[0].PromptTokens)
				require.Equal(t, 20, logs[0].CompletionTokens)
			}
		})
	}
}

func TestAdvancedCustomChannelValidationAndAdminProbe(t *testing.T) {
	require.Error(t, validateChannel(nil, false))
	ch := &model.Channel{Type: constant.ChannelTypeAdvancedCustom, Name: "advanced", Key: "test-key"}
	ch.BaseURL = common.GetPointer("https://example.com")
	require.Error(t, validateChannel(ch, true))
	ch.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/models/{model}:generateContent", UpstreamPath: "/v1/responses", Converter: dto.AdvancedCustomGeminiToResponses}}}})
	require.NoError(t, validateChannel(ch, true))
	require.Equal(t, string(constant.EndpointTypeGemini), normalizeChannelTestEndpoint(ch, "alias", ""))
	_, ok := buildTestRequest("alias", string(constant.EndpointTypeGemini), ch, true).(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.NotEqual(t, constant.ChannelTypeXiaomi, constant.ChannelTypeAdvancedCustom)
}
