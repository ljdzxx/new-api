package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/groupmonitor"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/alicebob/miniredis/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestMonitorResponsesSSE(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ok         bool
		answer     string
	}{
		{"empty-first", "data:\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"42\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n", true, "42"},
		{"early-eof", "data: {\"type\":\"response.created\"}\n\n", false, ""},
		{"error", "data: {\"type\":\"response.failed\"}\n\n", false, ""},
		{"completed-output", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"content\":[{\"type\":\"output_text\",\"text\":\"42\"}]}]}}\n\n", true, "42"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r MonitorResult
			answer, err := readMonitorSSE(strings.NewReader(tc.body), time.Now(), &r)
			require.Equal(t, tc.ok, err == nil)
			require.NotNil(t, r.TTFT)
			if tc.ok {
				require.Equal(t, tc.answer, answer)
			}
		})
	}
}

func TestMonitorRequestUsesResponsesWithoutOutputLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/responses", r.URL.Path)
		require.Equal(t, "Bearer test", r.Header.Get("Authorization"))
		var request map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &request))
		require.NotContains(t, request, "max_output_tokens")
		require.NotContains(t, request, "max_tokens")
		require.Equal(t, true, request["stream"])
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()
	cfg := monitorconfig.Default()
	cfg.BaseURL = server.URL
	r, _ := runMonitorResponse(context.Background(), cfg, "test", "model", "OK")
	require.True(t, r.OK)
	require.NotNil(t, r.TTFT)
}

func TestMonitorMessagesProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/messages", r.URL.Path)
		require.Equal(t, "test", r.Header.Get("x-api-key"))
		require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		require.Empty(t, r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &body))
		require.NotContains(t, body, "max_tokens")
		require.NotContains(t, body, "max_output_tokens")
		require.Equal(t, []any{map[string]any{"role": "user", "content": "question"}}, body["messages"])
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"content\":[]}}\n\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"text\",\"text\":\"4\"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"2\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()
	cfg := monitorconfig.Default()
	cfg.BaseURL = server.URL + "/v1/responses"
	r, answer := runMonitorResponse(context.Background(), cfg, "test", "claude-model", "question", "messages")
	require.True(t, r.OK, r.Error)
	require.Equal(t, "42", answer)
	require.NotNil(t, r.TTFT)
	for _, body := range []string{`{"type":"error"}`, `{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`, `{"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}`} {
		var result MonitorResult
		_, err := readMonitorSSE(strings.NewReader("data: "+body+"\n\n"), time.Now(), &result, "messages")
		require.Error(t, err)
	}
}

func TestMonitorDedicatedModelsAndPerGroupSwitches(t *testing.T) {
	on, off := true, false
	cfg := monitorconfig.Default()
	g := monitorconfig.Group{Models: []string{"probe-a", "probe-b"}, SVGModel: "svg-only", LogicModel: "logic-only", SVGTest: &on, LogicTest: &on, Active: &off}
	require.Equal(t, []monitorJob{{"probe-a", "probe", 5}, {"probe-b", "probe", 5}, {"svg-only", "svg", 60}, {"logic-only", "logic", 60}}, monitorGroupJobs(cfg, g))
	g.SVGTest = &off
	g.LogicTest = &off
	require.Equal(t, []monitorJob{{"probe-a", "probe", 5}, {"probe-b", "probe", 5}}, monitorGroupJobs(cfg, g))
}

func TestMonitorR2RetentionDeletesPairs(t *testing.T) {
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(204)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>monitor/g/001.html</Key></Contents><Contents><Key>monitor/g/001.png</Key><LastModified>2020-01-01T00:00:00Z</LastModified></Contents><Contents><Key>monitor/g/002.html</Key></Contents><Contents><Key>monitor/g/002.png</Key><LastModified>2020-01-01T00:00:00Z</LastModified></Contents></ListBucketResult>`)
	}))
	defer server.Close()
	client := s3.NewFromConfig(aws.Config{Region: "auto", Credentials: aws.AnonymousCredentials{}}, func(o *s3.Options) { o.BaseEndpoint = aws.String(server.URL); o.UsePathStyle = true })
	_, err := pruneMonitorObjects(context.Background(), client, "bucket", "monitor/g/", 1)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"/bucket/monitor/g/001.html", "/bucket/monitor/g/001.png"}, deleted)
}

func monitorTestRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = old, enabled; _ = client.Close() })
	return r
}

func TestMonitorDistributedTaskDeduplication(t *testing.T) {
	r := monitorTestRedis(t)
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\"}\n\n")
	}))
	defer upstream.Close()
	cfg := monitorconfig.Default()
	cfg.BaseURL = upstream.URL
	first, second := make(chan struct{}, 10), make(chan struct{}, 10)
	tryMonitorJob(first, cfg, "g", "m", "test", "probe", 5)
	tryMonitorJob(second, cfg, "g", "m", "test", "probe", 5)
	require.Eventually(t, func() bool { return len(first) == 0 && len(second) == 0 && requests.Load() > 0 }, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, int32(1), requests.Load())
	require.True(t, r.Exists(groupmonitor.Key("g", "m", "probe:latest")))
	for _, key := range r.Keys() {
		require.Positive(t, r.TTL(key), key)
	}
}
