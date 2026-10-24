package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitorMessagesBudgetsOnWire(t *testing.T) {
	settings := model_setting.GetClaudeSettings()
	original := settings.DefaultMaxTokens
	settings.DefaultMaxTokens = map[string]int{"default": 8192, "claude-opus-5-5": 4096}
	t.Cleanup(func() { settings.DefaultMaxTokens = original })
	for _, tc := range []struct {
		name, model, protocol, kind string
		general, svg, want          int
	}{
		{"legacy default", "claude-other", "messages", "svg", 0, 0, 8192},
		{"legacy model override", "claude-opus-5-5", "messages", "svg", 0, 0, 4096},
		{"probe monitor override", "claude-opus-5-5", "messages", "probe", 16384, 32768, 16384},
		{"logic monitor override", "claude-opus-5-5", "messages", "logic", 16384, 32768, 16384},
		{"svg inherits monitor", "claude-opus-5-5", "messages", "svg", 16384, 0, 16384},
		{"svg override", "claude-opus-5-5", "messages", "svg", 16384, 32768, 32768},
		{"svg only", "claude-opus-5-5", "messages", "svg", 0, 32768, 32768},
		{"svg does not affect logic", "claude-opus-5-5", "messages", "logic", 0, 32768, 4096},
		{"responses unchanged", "model", "responses", "svg", 16384, 32768, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if !assert.NoError(t, common.DecodeJson(r.Body, &body)) {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if tc.want == 0 {
					assert.NotContains(t, body, "max_tokens")
					assert.NotContains(t, body, "max_output_tokens")
				} else {
					assert.Equal(t, float64(tc.want), body["max_tokens"])
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if tc.protocol == "messages" {
					fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
				} else {
					fmt.Fprint(w, "data: {\"type\":\"response.completed\"}\n\n")
				}
			}))
			defer server.Close()
			cfg := monitorconfig.Default()
			cfg.BaseURL, cfg.MessagesMaxTokens, cfg.SVGMessagesMaxTokens = server.URL, tc.general, tc.svg
			result, _ := runMonitorResponse(context.Background(), cfg, "test", tc.model, "draw", tc.protocol, tc.kind)
			require.True(t, result.OK, result.Error)
			require.Equal(t, tc.want, result.MaxTokens)
			if tc.protocol == "messages" {
				require.Equal(t, "end_turn", result.StopReason)
			}
		})
	}
	require.Equal(t, 4096, settings.GetDefaultMaxTokens("claude-opus-5-5"))
}

func TestMonitorInvalidBudgetsNeverSendRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid budget reached the model endpoint")
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	for _, value := range []int{-1, common.MaxTokensLimit + 1} {
		for _, svg := range []bool{false, true} {
			cfg := monitorconfig.Default()
			cfg.BaseURL = server.URL
			if svg {
				cfg.SVGMessagesMaxTokens = value
			} else {
				cfg.MessagesMaxTokens = value
			}
			result, _ := runMonitorResponse(context.Background(), cfg, "test", "model", "draw", "messages", "svg")
			require.False(t, result.OK)
			require.Equal(t, "invalid_max_tokens", result.Error)
			require.Zero(t, result.MaxTokens)
		}
	}
}

func TestMonitorTruncatedBudgetPersistsWithPartialAnswer(t *testing.T) {
	monitorTestRedis(t)
	installMonitorR2Test(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !assert.NoError(t, common.DecodeJson(r.Body, &body)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, float64(32768), body["max_tokens"])
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"<svg>partial\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()
	cfg := monitorconfig.Default()
	cfg.BaseURL, cfg.SVGMessagesMaxTokens = server.URL, 32768
	cfg.SVGOutputDir, cfg.SVGPrompt = t.TempDir(), "draw"
	cfg.Groups["g"] = monitorconfig.Group{Protocol: "messages"}
	ctx := context.Background()
	active := make(chan struct{}, 1)
	tryMonitorJob(active, cfg, "g", "model", "test", "svg", 1)
	var history []MonitorResult
	require.Eventually(t, func() bool {
		history, _ = GetMonitorHistory(ctx, "g", "svg", cfg)
		return len(active) == 0 && len(history) == 1
	}, 5*time.Second, 10*time.Millisecond)
	cfg.SVGMessagesMaxTokens = 65536 // Historical requests must retain their own budget.
	saved, err := GetMonitorRecord(ctx, "g", "svg", history[0].ID, cfg)
	require.NoError(t, err)
	require.False(t, saved.OK)
	require.Equal(t, "response_incomplete", saved.Error)
	require.Equal(t, 32768, saved.MaxTokens)
	require.Equal(t, "max_tokens", saved.StopReason)
	require.Equal(t, "request_failed", saved.Status)
	require.Equal(t, "<svg>partial", saved.Answer)
	require.Equal(t, 32768, history[0].MaxTokens)
	require.Equal(t, "max_tokens", history[0].StopReason)
}
