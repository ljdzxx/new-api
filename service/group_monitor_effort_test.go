package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitorScheduledEffortOnWire(t *testing.T) {
	for _, protocol := range []string{"responses", "messages"} {
		for _, tc := range []struct {
			kind, logic, svg, want string
		}{
			{"logic", "high", "medium", "high"},
			{"svg", "high", "medium", "medium"},
			{"probe", "high", "medium", ""},
			{"logic", "", "high", ""},
			{"svg", "high", "", ""},
			{"logic", " low ", "", "low"},
		} {
			t.Run(protocol+"/"+tc.kind+"/"+tc.want, func(t *testing.T) {
				monitorTestRedis(t)
				installMonitorR2Test(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
				})
				var requests atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					assert.Equal(t, "/v1/"+protocol, r.URL.Path)
					var body map[string]any
					if !assert.NoError(t, common.DecodeJson(r.Body, &body)) {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					field, other := "reasoning", "output_config"
					if protocol == "messages" {
						field, other = other, field
					}
					assert.NotContains(t, body, other)
					assert.NotContains(t, body, "reasoning_effort")
					assert.NotContains(t, body, "thinking")
					if tc.want == "" {
						assert.NotContains(t, body, field)
					} else {
						assert.Equal(t, map[string]any{"effort": tc.want}, body[field])
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if protocol == "messages" {
						fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
					} else {
						fmt.Fprint(w, "data: {\"type\":\"response.completed\"}\n\n")
					}
				}))
				defer upstream.Close()
				cfg := monitorconfig.Default()
				cfg.BaseURL, cfg.SVGOutputDir = upstream.URL, t.TempDir()
				cfg.Groups["g"] = monitorconfig.Group{Protocol: protocol, LogicEffort: tc.logic, SVGEffort: tc.svg}
				active := make(chan struct{}, 1)
				tryMonitorJob(active, cfg, "g", "model", "test", tc.kind, 1)
				require.Eventually(t, func() bool { return len(active) == 0 }, 5*time.Second, 10*time.Millisecond)
				require.Equal(t, int32(1), requests.Load())
			})
		}
	}
}
