package middleware

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/groupmonitor"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Observe the client-facing SSE stream, including empty data events. The prefix
// buffer is bounded and never retains prompts or generated content.
type monitorWriter struct {
	gin.ResponseWriter
	started time.Time
	first   *float64
	line    string
	mu      sync.Mutex
}

func (w *monitorWriter) observe(data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.first != nil || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
		return
	}
	for _, b := range data {
		if b == '\n' {
			w.line = ""
			continue
		}
		if len(w.line) < 6 {
			w.line += string(b)
		}
		if strings.HasPrefix(w.line, "data:") || strings.HasPrefix(w.line, "event:") {
			v := float64(time.Since(w.started).Microseconds()) / 1000
			w.first = &v
			return
		}
	}
}
func (w *monitorWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if n > 0 {
		w.observe(b[:n])
	}
	return n, err
}
func (w *monitorWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

type monitorRecord struct {
	group, model string
	sample       groupmonitor.Sample
}

var monitorQueue = make(chan monitorRecord, 4096)
var monitorWorkers sync.Once

func GroupMonitor() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := monitorconfig.Get()
		if !cfg.Enabled || !groupmonitor.Ready() {
			c.Next()
			return
		}
		start := time.Now()
		w := &monitorWriter{ResponseWriter: c.Writer, started: start}
		c.Writer = w
		c.Next()
		group := c.GetString(groupmonitor.GroupKey)
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
		}
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		}
		name := c.GetString(groupmonitor.ModelKey)
		// Rate limiting can reject an authenticated request before Distribute.
		// Reuse the bounded request body parser so those failures still count.
		if name == "" && group != "" && c.Request.Method == "POST" {
			if request, _, err := getModelRequest(c); err == nil && request != nil {
				name = request.Model
			}
		}
		if !cfg.Contains(group, name) {
			return
		}
		w.mu.Lock()
		first := w.first
		w.mu.Unlock()
		s := groupmonitor.Sample{ID: uuid.NewString(), At: time.Now().UnixMilli(), Success: c.GetBool(groupmonitor.SuccessKey), TTFT: first}
		if value, ok := c.Get(groupmonitor.CacheKey); ok {
			if v, ok := value.(float64); ok && v > 0 && v <= 1 {
				s.Cache = &v
			}
		}
		monitorWorkers.Do(func() {
			for i := 0; i < 4; i++ {
				go func() {
					for r := range monitorQueue {
						ctx, cancel := context.WithTimeout(context.Background(), time.Second)
						err := groupmonitor.Write(ctx, r.group, r.model, "requests", r.sample)
						cancel()
						if err != nil {
							common.SysError("group monitor sample write failed: " + err.Error())
						}
					}
				}()
			}
		})
		select {
		case monitorQueue <- monitorRecord{group, name, s}:
		default:
			common.SysError("group monitor sample queue full; sample dropped")
		}
	}
}
