package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/groupmonitor"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/stretchr/testify/require"
)

type monitorTimeoutTransport func(*http.Request) (*http.Response, error)

func (f monitorTimeoutTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestMonitorJobUsesIndependentTimeoutAndLease(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want time.Duration
	}{
		{"probe", 30 * time.Second},
		{"logic", 900 * time.Second},
		{"svg", 3600 * time.Second},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			redis := monitorTestRedis(t)
			installMonitorR2Test(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
			})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.completed\"}\n\n")
			}))
			defer upstream.Close()
			cfg := monitorconfig.Default()
			probe, logic, svg := 30, 900, 3600
			cfg.ProbeTimeoutSeconds, cfg.LogicTimeoutSeconds, cfg.SVGTimeoutSeconds = &probe, &logic, &svg
			cfg.BaseURL, cfg.SVGOutputDir = upstream.URL, t.TempDir()
			lockModel := "model"
			if tc.kind == "svg" {
				lockModel = ""
			}
			lease := groupmonitor.Key("g", lockModel, tc.kind+":lock")
			type observation struct {
				timeout, leaseTTL time.Duration
			}
			observed := make(chan observation, 1)
			transport := http.DefaultTransport
			http.DefaultTransport = monitorTimeoutTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() == upstream.URL+"/v1/responses" {
					deadline, _ := req.Context().Deadline()
					observed <- observation{time.Until(deadline), redis.TTL(lease)}
				}
				return transport.RoundTrip(req)
			})
			t.Cleanup(func() { http.DefaultTransport = transport })
			active := make(chan struct{}, 1)
			tryMonitorJob(active, cfg, "g", "model", "test", tc.kind, 1)
			require.Eventually(t, func() bool { return len(active) == 0 }, 5*time.Second, 10*time.Millisecond)
			select {
			case got := <-observed:
				require.InDelta(t, float64(tc.want), float64(got.timeout), float64(time.Second))
				require.Equal(t, tc.want+4*time.Minute, got.leaseTTL)
			default:
				t.Fatal("monitor did not send a request")
			}
			require.False(t, redis.Exists(lease), "completed jobs must release their lease")
		})
	}
}
