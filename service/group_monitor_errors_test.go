package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/stretchr/testify/require"
)

type monitorBrokenReader struct{ err error }

func (r monitorBrokenReader) Read([]byte) (int, error) { return 0, r.err }

func TestMonitorStreamFailurePreservesCause(t *testing.T) {
	cause := errors.New("connection reset")
	var result MonitorResult
	_, err := readMonitorSSE(monitorBrokenReader{cause}, time.Now(), &result)
	require.Equal(t, "sse_read_failed", monitorErrorCode(err, "unknown"))
	require.ErrorIs(t, err, cause)
}

func TestMonitorResponseTimeoutIsDistinct(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	cfg := monitorconfig.Default()
	cfg.BaseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, _ := runMonitorResponse(ctx, cfg, "secret", "m", "test")
	require.False(t, result.OK)
	require.Equal(t, "probe_timeout", result.Error)
	require.ErrorIs(t, result.diagnostic, context.DeadlineExceeded)
	raw, err := common.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(raw), "context deadline"))
}
