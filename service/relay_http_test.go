package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestRelayHTTPRequestCancelsUpstream(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 1
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, http2 := range []bool{false, true} {
		for _, tc := range []struct {
			name                                   string
			stream, headers, disconnect, closeBody bool
		}{
			{name: "sync_header_timeout"},
			{name: "sync_body_timeout", headers: true},
			{name: "stream_header_timeout", stream: true},
			{name: "sync_disconnect_before_headers", disconnect: true},
			{name: "sync_disconnect_during_body", headers: true, disconnect: true},
			{name: "stream_disconnect_before_headers", stream: true, disconnect: true},
			{name: "stream_disconnect_during_body", stream: true, headers: true, disconnect: true},
			{name: "stream_close_body", stream: true, headers: true, closeBody: true},
		} {
			t.Run(fmt.Sprintf("http2=%t/%s", http2, tc.name), func(t *testing.T) {
				started, canceled, bodyReady := make(chan struct{}), make(chan struct{}), make(chan struct{})
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tc.headers {
						w.(http.Flusher).Flush()
					}
					close(started)
					<-r.Context().Done()
					close(canceled)
				}))
				server.EnableHTTP2 = http2
				server.StartTLS()
				defer server.Close()
				client := server.Client()
				if !tc.stream && !tc.disconnect {
					client.Timeout = 100 * time.Millisecond
				}
				downstream, cancel := context.WithCancel(context.Background())
				defer cancel()
				req, err := http.NewRequest(http.MethodGet, server.URL, nil)
				require.NoError(t, err)
				result := make(chan error, 1)
				go func() {
					resp, err := DoRelayHTTPRequest(downstream, client, req, tc.stream)
					if err == nil {
						close(bodyReady)
						if tc.closeBody {
							err = resp.Body.Close()
						} else {
							_, err = io.ReadAll(resp.Body)
							_ = resp.Body.Close()
						}
					}
					result <- err
				}()
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					cancel()
					t.Fatal("upstream request did not start")
				}
				if tc.disconnect {
					if tc.headers {
						select {
						case <-bodyReady:
						case <-time.After(3 * time.Second):
							cancel()
							t.Fatal("relay did not receive response headers")
						}
					}
					cancel()
				}
				select {
				case err := <-result:
					if tc.closeBody {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
				case <-time.After(3 * time.Second):
					cancel()
					t.Fatal("relay did not stop")
				}
				select {
				case <-canceled:
				case <-time.After(3 * time.Second):
					t.Fatal("upstream did not observe cancellation")
				}
			})
		}
	}
}

func TestRelayHTTPRequestHeaderTimeoutDoesNotLimitStreamBody(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 1
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for range 5 {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_, _ = io.WriteString(w, "data\n")
				w.(http.Flusher).Flush()
			}
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	resp, err := DoRelayHTTPRequest(ctx, server.Client(), req, true)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "data\ndata\ndata\ndata\ndata\n", string(body))
}
