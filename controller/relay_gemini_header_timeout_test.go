package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestGeminiResponsesHeaderTimeoutBeforeOutput(t *testing.T) {
	for _, heartbeat := range []bool{false, true} {
		t.Run(map[bool]string{false: "json_504", true: "sse_failed_after_keepalive"}[heartbeat], func(t *testing.T) {
			db := setupResponsesRelayBillingTest(t)
			oldTimeout, oldRetries := constant.StreamingTimeout, common.RetryTimes
			constant.StreamingTimeout, common.RetryTimes = 1, 0
			t.Cleanup(func() { constant.StreamingTimeout, common.RetryTimes = oldTimeout, oldRetries })
			release := make(chan struct{})
			paths := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths <- r.URL.Path
				_, _ = io.Copy(io.Discard, r.Body)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			service.InitHttpClient()
			c, recorder, _ := newRelayMockContext(true)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"hi","stream":true}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("model_mapping", `{"gpt-4o-mini":"gemini-test-medium"}`)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeGemini)
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
			common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
			if heartbeat {
				helper.SetEventStreamHeaders(c)
				require.NoError(t, helper.PingData(c))
			}
			Relay(c, types.RelayFormatOpenAIResponses)
			require.Contains(t, <-paths, "/models/gemini-test-medium:streamGenerateContent")
			require.Contains(t, recorder.Body.String(), string(types.ErrorCodeUpstreamResponseHeaderTimeout))
			require.Contains(t, recorder.Body.String(), "upstream response headers timed out after 1s")
			require.NotContains(t, recorder.Body.String(), "response.completed")
			if heartbeat {
				require.Equal(t, 200, recorder.Code)
				require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed\n"))
			} else {
				require.Equal(t, http.StatusGatewayTimeout, recorder.Code)
			}
			assertGeminiResponsesRefund(t, db)
			var count int64
			require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}
