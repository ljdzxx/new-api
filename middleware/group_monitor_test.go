package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMonitorFirstEmptySSEEvent(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	w := &monitorWriter{ResponseWriter: c.Writer, started: time.Now().Add(-time.Second)}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = w.WriteString(": ping\n\n")
	require.Nil(t, w.first)
	_, _ = w.WriteString("da")
	require.Nil(t, w.first)
	_, _ = w.WriteString("ta:\n\n")
	require.NotNil(t, w.first)
	first := *w.first
	require.GreaterOrEqual(t, first, 1000.0)
	_, _ = w.WriteString("data: {\"text\":\"later\"}\n\n")
	require.Equal(t, first, *w.first)
}

func TestMonitorNonSSEIgnored(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	w := &monitorWriter{ResponseWriter: c.Writer, started: time.Now()}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.WriteString("data: ignored")
	require.Nil(t, w.first)
}
