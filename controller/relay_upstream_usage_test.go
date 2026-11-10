package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type chatUsageCancelWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
}

func (w *chatUsageCancelWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if strings.Contains(string(data), "chatcmpl_usage") {
		w.cancel()
	}
	return n, err
}

func (w *chatUsageCancelWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }

func TestChatRelayRequiresUpstreamUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, reported := range []bool{false, true} {
			for _, canceled := range []bool{false, true} {
				if canceled && !stream {
					continue
				}
				t.Run(fmt.Sprintf("stream=%t/usage=%t/canceled=%t", stream, reported, canceled), func(t *testing.T) {
					db := setupResponsesRelayBillingTest(t)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						usage := ""
						if reported {
							usage = `,"usage":{"prompt_tokens":100,"completion_tokens":0,"total_tokens":100}`
						}
						if stream {
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprint(w, `data: {"id":"chatcmpl_usage","choices":[{"index":0,"delta":{"content":"hello"}}]`+usage+"}\n\n")
							w.(http.Flusher).Flush()
							if canceled {
								<-r.Context().Done()
								return
							}
							fmt.Fprint(w, "data: [DONE]\n\n")
						} else {
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprint(w, `{"id":"chatcmpl_usage","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]`+usage+`}`)
						}
					}))
					defer upstream.Close()
					oldTimeout, oldRetries := common.RelayTimeout, common.RetryTimes
					common.RelayTimeout, common.RetryTimes = 5, 0
					service.InitHttpClient()
					t.Cleanup(func() { common.RelayTimeout, common.RetryTimes = oldTimeout, oldRetries; service.InitHttpClient() })
					c, recorder, _ := newRelayMockContext(stream)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}],"stream":%t}`, stream)))
					c.Request.Header.Set("Content-Type", "application/json")
					common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
					common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
					common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
					if canceled {
						ctx, cancel := context.WithCancel(c.Request.Context())
						defer cancel()
						c.Request = c.Request.WithContext(ctx)
						c.Writer = &chatUsageCancelWriter{ResponseWriter: c.Writer, cancel: cancel}
					}
					Relay(c, types.RelayFormatOpenAI)
					var logs []model.Log
					require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
					require.Len(t, logs, 1, recorder.Body.String())
					require.Zero(t, logs[0].CompletionTokens)
					require.Equal(t, reported, gjson.Get(logs[0].Other, "admin_info.upstream_usage_reported").Bool())
					if reported {
						require.Equal(t, 100, logs[0].PromptTokens)
						require.Positive(t, logs[0].Quota)
					} else {
						require.Zero(t, logs[0].PromptTokens)
						require.Zero(t, logs[0].Quota)
					}
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
}
