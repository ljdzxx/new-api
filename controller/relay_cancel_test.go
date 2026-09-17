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
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestRelayNonStreamDisconnectDoesNotRetryOrRecordChannelError(t *testing.T) {
	for _, responses := range []bool{false, true} {
		for _, headers := range []bool{false, true} {
			t.Run(fmt.Sprintf("responses=%t/headers=%t", responses, headers), func(t *testing.T) {
				db := setupResponsesRelayBillingTest(t)
				// This test covers cancellation routing, not asynchronous refunds. A free
				// model prevents refund workers from outliving the per-case DB fixture.
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":0}`))
				oldRetries, oldLogs := common.RetryTimes, constant.ErrorLogEnabled
				common.RetryTimes, constant.ErrorLogEnabled = 3, true
				service.InitHttpClient()
				t.Cleanup(func() { common.RetryTimes, constant.ErrorLogEnabled = oldRetries, oldLogs })
				started, stopped := make(chan struct{}, 1), make(chan struct{}, 1)
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					calls.Add(1)
					if headers {
						w.Header().Set("Content-Type", "application/json")
						w.(http.Flusher).Flush()
					}
					started <- struct{}{}
					<-r.Context().Done()
					stopped <- struct{}{}
				}))
				defer upstream.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				c, recorder, _ := newRelayMockContext(true)
				path, body, format := "/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}],"stream":false}`, types.RelayFormatOpenAI
				if responses {
					path, body, format = "/v1/responses", `{"model":"gpt-4o-mini","input":"hello","stream":false}`, types.RelayFormatOpenAIResponses
				}
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
				c.Request.Header.Set("Content-Type", "application/json")
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
				common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
				go func() {
					select {
					case <-started:
						cancel()
					case <-ctx.Done():
					}
				}()
				Relay(c, format)
				require.EqualValues(t, 1, calls.Load())
				require.Empty(t, recorder.Body.String(), "do not write errors to a disconnected client")
				var errorCount int64
				require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeError).Count(&errorCount).Error)
				require.Zero(t, errorCount)
				select {
				case <-stopped:
				case <-time.After(2 * time.Second):
					t.Fatal("relay did not cancel upstream")
				}
			})
		}
	}
}
