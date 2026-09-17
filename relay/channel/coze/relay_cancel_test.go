package coze

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCozePollingStopsWhenClientDisconnects(t *testing.T) {
	oldTimeout := common.RelayTimeout
	common.RelayTimeout = 0
	service.InitHttpClient()
	t.Cleanup(func() {
		service.GetHttpClient().CloseIdleConnections()
		common.RelayTimeout = oldTimeout
		service.InitHttpClient()
	})
	polling, canceled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/v3/chat" {
			_, _ = io.WriteString(w, `{"code":0,"data":{"id":"chat","conversation_id":"conversation"}}`)
			return
		}
		close(polling)
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}")).WithContext(ctx)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}}
	done := make(chan error, 1)
	go func() {
		_, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader("{}"))
		done <- err
	}()
	select {
	case <-polling:
	case <-time.After(3 * time.Second):
		t.Fatal("Coze did not start polling")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("Coze polling did not stop")
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("Coze upstream was not canceled")
	}
	require.EqualValues(t, 2, calls.Load())
}
