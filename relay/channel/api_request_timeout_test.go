package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoRequestRelayTimeoutOnlyNonStream(t *testing.T) {
	oldTimeout := common.RelayTimeout
	common.RelayTimeout = 1
	service.InitHttpClient()
	service.ResetProxyClientCache()
	t.Cleanup(func() {
		service.GetHttpClient().CloseIdleConnections()
		service.ResetProxyClientCache()
		common.RelayTimeout = oldTimeout
		service.InitHttpClient()
	})

	for _, useProxy := range []bool{false, true} {
		name := "direct"
		if useProxy {
			name = "proxy"
		}
		t.Run(name, func(t *testing.T) {
			for _, delayBody := range []bool{false, true} {
				phase := "headers"
				if delayBody {
					phase = "body"
				}
				t.Run(phase, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "text/event-stream")
						if delayBody {
							w.(http.Flusher).Flush()
						}
						timer := time.NewTimer(1200 * time.Millisecond)
						defer timer.Stop()
						select {
						case <-timer.C:
							_, _ = io.WriteString(w, "data: [DONE]\n\n")
						case <-r.Context().Done():
						}
					}))
					defer server.Close()

					// Run streaming first to catch accidental mutation of the shared client.
					for _, isStream := range []bool{true, false} {
						mode := "sync"
						if isStream {
							mode = "stream"
						}
						t.Run(mode, func(t *testing.T) {
							ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
							ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
							info := &relaycommon.RelayInfo{
								IsStream: isStream, DisablePing: true,
								ChannelMeta: &relaycommon.ChannelMeta{},
							}
							url := server.URL
							if useProxy {
								info.ChannelSetting.Proxy = server.URL
								url = "http://upstream.invalid/v1/chat/completions"
							}
							requestCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, strings.NewReader("{}"))
							require.NoError(t, err)
							started := time.Now()
							resp, err := DoRequest(ctx, req, info)
							var body []byte
							if err == nil {
								defer resp.Body.Close()
								body, err = io.ReadAll(resp.Body)
							}
							if isStream {
								require.NoError(t, err)
								require.Equal(t, "data: [DONE]\n\n", string(body))
							} else {
								require.Error(t, err)
								require.NoError(t, requestCtx.Err(), "the test deadline must not cause the timeout")
								require.GreaterOrEqual(t, time.Since(started), time.Second)
								if delayBody {
									require.ErrorIs(t, err, context.DeadlineExceeded)
								} else {
									// DoRequest hides the original error before returning it.
									require.ErrorContains(t, err, "upstream error: do request failed")
								}
							}
						})
					}
				})
			}
		})
	}
}

func TestStreamIdleTimeoutCancelsUpstreamRequest(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 1
	service.InitHttpClient()
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: hello\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}")).WithContext(ctx)
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{IsStream: true, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}
	resp, err := DoRequest(c, req, info)
	require.NoError(t, err)
	result := helper.StreamScannerHandlerWithOptions(c, resp, info, func(data string, sr *helper.StreamResult) {}, helper.StreamScannerOptions{})
	require.Equal(t, helper.StreamScannerTimeout, result.Reason)
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("idle timeout did not cancel the upstream request")
	}
}
