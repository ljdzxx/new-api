package claude

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaudePassThroughFlushesBeforeNextUpstreamWrite(t *testing.T) {
	for _, tc := range []struct {
		name       string
		lineEnding string
		fragmented bool
	}{
		{name: "LF", lineEnding: "\n"},
		{name: "CRLF", lineEnding: "\r\n"},
		{name: "CR", lineEnding: "\r"},
		{name: "fragmented LF", lineEnding: "\n", fragmented: true},
		{name: "fragmented CRLF", lineEnding: "\r\n", fragmented: true},
		{name: "fragmented CR", lineEnding: "\r", fragmented: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := []string{
				"event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg_latency","model":"claude-test","usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":3}}}` + "\n\n",
				"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"首字"}}` + "\n\n",
				": keepalive\n\n",
				"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\n",
				"event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n",
			}
			var chunks []string
			for _, frame := range frames {
				frame = strings.ReplaceAll(frame, "\n", tc.lineEnding)
				if !tc.fragmented {
					chunks = append(chunks, frame)
					continue
				}
				// Split inside SSE fields, JSON, UTF-8 and CRLF pairs.
				for len(frame) > 0 {
					n := min(7, len(frame))
					chunks = append(chunks, frame[:n])
					frame = frame[n:]
				}
			}
			next := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Request-Id", "req_latency")
				for _, chunk := range chunks {
					_, _ = io.WriteString(w, chunk)
					w.(http.Flusher).Flush()
					select {
					case <-next:
					case <-r.Context().Done():
						return
					}
				}
			}))
			defer upstream.Close()
			type result struct {
				usage *dto.Usage
				err   *types.NewAPIError
			}
			finished := make(chan result, 1)
			downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream.URL, nil)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
				c, _ := gin.CreateTestContext(w)
				c.Request = r
				info := &relaycommon.RelayInfo{
					RelayFormat: types.RelayFormatClaude,
					ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
				}
				usage, apiErr := ClaudeStreamPassThroughHandler(c, resp, info)
				finished <- result{usage, apiErr}
			}))
			defer downstream.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, downstream.URL, nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err, "first bytes must be flushed before the upstream writes more")
			defer resp.Body.Close()
			require.Equal(t, "req_latency", resp.Header.Get("Request-Id"))
			require.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))
			for _, chunk := range chunks {
				got := make([]byte, len(chunk))
				_, err := io.ReadFull(resp.Body, got)
				require.NoError(t, err, "already received bytes must not wait for a newline or another event")
				require.Equal(t, chunk, string(got))
				select {
				case next <- struct{}{}:
				case <-ctx.Done():
					t.Fatal("upstream did not resume")
				}
			}
			tail, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Empty(t, tail, "passthrough must preserve the exact upstream bytes")
			select {
			case got := <-finished:
				require.Nil(t, got.err)
				require.Equal(t, 10, got.usage.PromptTokens)
				require.Equal(t, 2, got.usage.CompletionTokens)
				require.Equal(t, 12, got.usage.TotalTokens)
				require.Equal(t, 3, got.usage.PromptTokensDetails.CachedTokens)
			case <-ctx.Done():
				t.Fatal("relay did not finish")
			}
		})
	}
}
