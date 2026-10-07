package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/groupmonitor"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/QuantumNous/new-api/setting/image_storage_setting"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"
)

func TestMonitorLogicMatchModes(t *testing.T) {
	for _, tc := range []struct {
		answer, expected, mode string
		ok                     bool
	}{
		{" 21\n", "21", "exact", true}, {"Answer: 21", "21", "exact", false},
		{"Answer: 21", "21", "contains", true}, {"Answer: 12", "21", "contains", false},
		{"ABC", "abc", "contains", false}, {"anything", " ", "contains", false},
	} {
		require.Equal(t, tc.ok, monitorLogicMatches(tc.answer, tc.expected, tc.mode), tc)
	}
}

func TestMonitorPartialReplySurvivesFailedStream(t *testing.T) {
	for _, protocol := range []string{"responses", "messages"} {
		body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"<svg>partial\"}\n\n"
		if protocol == "messages" {
			body = "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"<svg>partial\"}}\n\n"
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, body)
		}))
		cfg := monitorconfig.Default()
		cfg.BaseURL = server.URL
		result, answer := runMonitorResponse(context.Background(), cfg, "test", "model", "draw", protocol)
		server.Close()
		require.False(t, result.OK)
		require.Equal(t, "response_not_completed", result.Error)
		require.Equal(t, "<svg>partial", answer)
	}
}

func TestMonitorHTMLPassivePreview(t *testing.T) {
	doc, err := monitorHTML("```html\n" + `<!doctype html><html><head><meta http-equiv="refresh" content="0;url=https://example.com"><style>svg{color:red}</style></head><body onload="evil()"><svg viewBox="0 0 20 20"><filter><feTurbulence/></filter><foreignObject><div>Drawing</div></foreignObject></svg><script>evil()</script><iframe srcdoc="evil"></iframe><a href="javascript:evil()">link</a></body></html>` + "\n```")
	require.NoError(t, err)
	for _, allowed := range []string{"Content-Security-Policy", "feTurbulence", "foreignObject", "Drawing", "svg{color:red}"} {
		require.Contains(t, doc, allowed)
	}
	for _, denied := range []string{"evil()", "refresh", "<iframe", "srcdoc", "```"} {
		require.NotContains(t, doc, denied)
	}
	require.Equal(t, 1, strings.Count(doc, "<script>"))
	require.Contains(t, doc, "script-src 'sha256-")
}

func installMonitorR2Test(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	s := image_storage_setting.GetImageStorageSetting()
	common.OptionMapRWMutex.Lock()
	old := *s
	s.R2Endpoint, s.R2Bucket, s.R2AccessKeyID, s.R2SecretAccessKey = server.URL, "bucket", "access", "secret"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() { common.OptionMapRWMutex.Lock(); *s = old; common.OptionMapRWMutex.Unlock() })
}

func TestMonitorLocalArchiveBeforeR2AndHTMLLinks(t *testing.T) {
	monitorTestRedis(t)
	cfg := monitorconfig.Default()
	cfg.SVGOutputDir = t.TempDir()
	result := MonitorResult{ID: "1790651160696-00000000-0000-0000-0000-000000000001", At: time.Now().UnixMilli(), OK: true, Status: "success", Kind: "svg", Model: "draw-model", Prompt: "draw a tree", Answer: `<svg><text>tree</text></svg>`}
	dir, err := monitorLocalDir(cfg, "../../group", result.ID)
	require.NoError(t, err)
	objects := map[string][]byte{}
	installMonitorR2Test(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			for _, name := range []string{"prompt.txt", "response.txt", "result.json", "artwork.html"} {
				require.FileExists(t, filepath.Join(dir, name))
			}
			data, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			objects[strings.TrimPrefix(r.URL.Path, "/bucket/")] = data
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, "<ListBucketResult><IsTruncated>false</IsTruncated>")
			for key := range objects {
				fmt.Fprintf(w, "<Contents><Key>%s</Key></Contents>", key)
			}
			fmt.Fprint(w, "</ListBucketResult>")
		default:
			t.Errorf("unexpected request %s", r.Method)
		}
	})
	require.NoError(t, storeMonitorArtwork(context.Background(), cfg, "../../group", &result))
	require.Len(t, objects, 4)
	require.True(t, strings.HasPrefix(dir, cfg.SVGOutputDir+string(os.PathSeparator)))
	raw, err := os.ReadFile(filepath.Join(dir, "response.txt"))
	require.NoError(t, err)
	require.Equal(t, result.Answer, string(raw))
	arts, err := GetMonitorArtworks(context.Background(), cfg, "../../group")
	require.NoError(t, err)
	require.Len(t, arts, 1)
	require.Equal(t, result.ID, arts[0].ID)
	require.Contains(t, arts[0].HTMLURL, "/artwork.html?")
	require.Empty(t, arts[0].HTMLKey)
	public, err := common.Marshal(arts)
	require.NoError(t, err)
	require.NotContains(t, string(public), "image_url")
}

func TestMonitorFailedUploadKeepsLocalReplyAndModelOutcome(t *testing.T) {
	for _, completed := range []bool{true, false} {
		t.Run(fmt.Sprint(completed), func(t *testing.T) {
			cfg := monitorconfig.Default()
			cfg.SVGOutputDir = t.TempDir()
			installMonitorR2Test(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(403)
				fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
			})
			result := MonitorResult{ID: "1790651160696-00000000-0000-0000-0000-000000000002", OK: completed, Status: "request_failed", Prompt: "draw", Answer: "partial reply"}
			if completed {
				result.Status = "success"
			}
			err := storeMonitorArtwork(context.Background(), cfg, "g", &result)
			require.Equal(t, "r2_upload_failed", monitorErrorCode(err, "unknown"))
			require.Equal(t, completed, result.OK)
			dir, err := monitorLocalDir(cfg, "g", result.ID)
			require.NoError(t, err)
			raw, err := os.ReadFile(filepath.Join(dir, "result.json"))
			require.NoError(t, err)
			var saved MonitorResult
			require.NoError(t, common.Unmarshal(raw, &saved))
			require.Equal(t, "partial reply", saved.Answer)
			require.Equal(t, result.Status, saved.Status)
			require.Equal(t, "r2_upload_failed", saved.ArtifactError)
			if completed {
				require.FileExists(t, filepath.Join(dir, "artwork.html"))
			} else {
				require.NoFileExists(t, filepath.Join(dir, "artwork.html"))
			}
		})
	}
}

func TestMonitorRetentionRemovesWholeRuns(t *testing.T) {
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(204)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated>`)
		for _, key := range []string{"001.html", "001.png", "002/prompt.txt", "002/response.txt", "002/result.json", "002/artwork.html", "003/prompt.txt", "003/response.txt", "003/result.json"} {
			fmt.Fprintf(w, "<Contents><Key>monitor/g/%s</Key></Contents>", key)
		}
		fmt.Fprint(w, `</ListBucketResult>`)
	}))
	defer server.Close()
	client := s3.NewFromConfig(aws.Config{Region: "auto", Credentials: aws.AnonymousCredentials{}}, func(o *s3.Options) { o.BaseEndpoint = aws.String(server.URL); o.UsePathStyle = true })
	retained, err := pruneMonitorObjects(context.Background(), client, "bucket", "monitor/g/", 1)
	require.NoError(t, err)
	require.Equal(t, map[string]bool{"003": true}, retained)
	require.ElementsMatch(t, []string{"/bucket/monitor/g/001.html", "/bucket/monitor/g/001.png", "/bucket/monitor/g/002/prompt.txt", "/bucket/monitor/g/002/response.txt", "/bucket/monitor/g/002/result.json", "/bucket/monitor/g/002/artwork.html"}, deleted)
}

func TestMonitorLocalDirectoryFailurePreventsUpload(t *testing.T) {
	cfg := monitorconfig.Default()
	cfg.SVGOutputDir = filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(cfg.SVGOutputDir, []byte("not a directory"), 0600))
	result := MonitorResult{ID: "1790651160696-00000000-0000-0000-0000-000000000003", OK: true, Status: "success"}
	err := storeMonitorArtwork(context.Background(), cfg, "g", &result)
	require.Equal(t, "local_output_failed", monitorErrorCode(err, "unknown"))
	require.True(t, result.OK)
	_, err = monitorLocalDir(cfg, "g", "../../outside")
	require.Error(t, err)
}

func TestMonitorHistoryOrderDetailsAndRetention(t *testing.T) {
	r := monitorTestRedis(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	// Concurrent jobs may finish out of order; display order follows start time.
	for i := 50; i >= 0; i-- {
		result := MonitorResult{ID: fmt.Sprint(i), At: now + int64(i), Model: "m", Kind: "logic", Prompt: "question", Answer: "answer", Expected: "answer", MatchMode: "contains", Status: "success", OK: true}
		require.NoError(t, saveMonitorTestResult(ctx, "g", result, monitorconfig.Default()))
	}
	history, err := GetMonitorHistory(ctx, "g", "logic", monitorconfig.Default())
	require.NoError(t, err)
	require.Len(t, history, monitorconfig.Default().HistoryKeep)
	require.Equal(t, "3", history[0].ID)
	require.Equal(t, "50", history[len(history)-1].ID)
	require.Empty(t, history[0].Answer)
	record, err := GetMonitorRecord(ctx, "g", "logic", "3", monitorconfig.Default())
	require.NoError(t, err)
	require.Equal(t, "question", record.Prompt)
	require.Equal(t, "answer", record.Answer)
	for _, key := range r.Keys() {
		require.Positive(t, r.TTL(key))
	}
	r.FastForward(monitorRecordTTL + time.Second)
	_, err = GetMonitorRecord(ctx, "g", "logic", "3", monitorconfig.Default())
	require.Error(t, err)
}

func TestMonitorHistoryConfigTrimsDetailsAndOldRecords(t *testing.T) {
	r := monitorTestRedis(t)
	ctx := context.Background()
	cfg := monitorconfig.Default()
	cfg.HistoryKeep, cfg.HistoryDays = 5, 7
	for i := 0; i < 5; i++ {
		at := time.Now().Add(-time.Duration(5-i) * 24 * time.Hour).UnixMilli()
		require.NoError(t, saveMonitorTestResult(ctx, "g", MonitorResult{ID: fmt.Sprint(i), At: at, Kind: "logic", Model: "m", Answer: "reply"}, cfg))
	}
	cfg.HistoryKeep, cfg.HistoryDays = 2, 4
	history, err := GetMonitorHistory(ctx, "g", "logic", cfg)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "3", history[0].ID)
	for _, id := range []string{"0", "1", "2"} {
		require.False(t, r.Exists(groupmonitor.Key("g", id, "logic:record")), "trimmed replies must be removed")
	}
	_, err = GetMonitorRecord(ctx, "g", "logic", "3", cfg)
	require.NoError(t, err)
	cfg.HistoryDays = 1
	history, err = GetMonitorHistory(ctx, "g", "logic", cfg)
	require.NoError(t, err)
	require.Empty(t, history)
	require.False(t, r.Exists(groupmonitor.Key("g", "3", "logic:record")))
}

func TestMonitorPublicArtworkURLAndLegacyPreview(t *testing.T) {
	r := monitorTestRedis(t)
	ctx := context.Background()
	var reads int
	installMonitorR2Test(t, func(w http.ResponseWriter, req *http.Request) {
		reads++
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><main style="width:1000px;height:900px">legacy</main><script>evil()</script></body></html>`)
	})
	cfg := monitorconfig.Default()
	cfg.SVGPublicBaseURL = "https://preview.example.com/assets"
	for _, art := range []MonitorArtwork{
		{ID: "new", HTMLKey: "monitor-svg/a/new/artwork.html", PreviewVersion: 2},
		{ID: "old", HTMLKey: "monitor-svg/a/old.html"},
	} {
		raw, err := common.Marshal(art)
		require.NoError(t, err)
		r.Lpush(groupmonitor.Key("g", "", "artworks"), string(raw))
	}
	arts, err := GetMonitorArtworks(ctx, cfg, "g")
	require.NoError(t, err)
	require.Len(t, arts, 2)
	require.Equal(t, "/api/monitor/preview?group=g&id=old", arts[0].PreviewURL)
	require.Equal(t, "https://preview.example.com/assets/monitor-svg/a/new/artwork.html", arts[1].HTMLURL)
	require.Empty(t, arts[1].PreviewURL)
	require.Zero(t, reads, "URL construction must not fetch objects")
	doc, err := GetMonitorLegacyPreview(ctx, cfg, "g", "old")
	require.NoError(t, err)
	require.Contains(t, doc, "ResizeObserver")
	require.NotContains(t, doc, "evil()")
	require.Equal(t, 1, reads)
	_, err = GetMonitorLegacyPreview(ctx, cfg, "other", "old")
	require.Error(t, err)
	require.Equal(t, 1, reads, "unlisted objects cannot be read")
}

func TestMonitorTaskResultsAndPartialArchives(t *testing.T) {
	for _, tc := range []struct {
		kind, answer, mode, status string
		complete                   bool
	}{
		{"logic", "Answer: 21", "exact", "test_failed", true},
		{"logic", "Answer: 21", "contains", "success", true},
		{"logic", "part", "exact", "request_failed", false},
		{"svg", "not SVG, but a completed answer", "exact", "success", true},
		{"svg", "<svg>unfinished", "exact", "request_failed", false},
	} {
		t.Run(tc.kind+tc.status, func(t *testing.T) {
			monitorTestRedis(t)
			installMonitorR2Test(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(403)
				fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
			})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &body))
				require.NotContains(t, body, "max_output_tokens")
				w.Header().Set("Content-Type", "text/event-stream")
				raw, _ := common.Marshal(map[string]string{"type": "response.output_text.delta", "delta": tc.answer})
				fmt.Fprintf(w, "data: %s\n\n", raw)
				if tc.complete {
					fmt.Fprint(w, "data: {\"type\":\"response.completed\"}\n\n")
				}
			}))
			defer upstream.Close()
			cfg := monitorconfig.Default()
			cfg.BaseURL, cfg.SVGOutputDir, cfg.LogicAnswer, cfg.LogicMatchMode = upstream.URL, t.TempDir(), "21", tc.mode
			cfg.LogicPrompt, cfg.SVGPrompt = "logic question", "SVG question"
			active := make(chan struct{}, 10)
			tryMonitorJob(active, cfg, "g", "model", "secret", tc.kind, 1)
			var history []MonitorResult
			require.Eventually(t, func() bool {
				history, _ = GetMonitorHistory(context.Background(), "g", tc.kind, cfg)
				return len(active) == 0 && len(history) == 1
			}, 5*time.Second, 10*time.Millisecond)
			record, err := GetMonitorRecord(context.Background(), "g", tc.kind, history[0].ID, cfg)
			require.NoError(t, err)
			require.Equal(t, tc.status, record.Status)
			require.Equal(t, tc.answer, record.Answer)
			require.NotEmpty(t, record.Prompt)
			require.Positive(t, record.FinishedAt)
			if tc.kind == "svg" {
				dir, err := monitorLocalDir(cfg, "g", record.ID)
				require.NoError(t, err)
				require.FileExists(t, filepath.Join(dir, "response.txt"))
			}
		})
	}
}

func TestMonitorLogicGroupOverridesInRequestAndHistory(t *testing.T) {
	for _, protocol := range []string{"responses", "messages"} {
		for _, tc := range []struct {
			name, groupJSON, prompt, expected, mode, reply, status string
		}{
			{"custom", `{"logic_prompt":"group question","logic_answer":"32","logic_match_mode":"contains"}`, "group question", "32", "contains", "Answer: 32", "success"},
			{"inherited", `{}`, "global question", "21", "exact", "Answer: 32", "test_failed"},
			{"mode only", `{"logic_match_mode":"contains"}`, "global question", "21", "contains", "Answer: 21", "success"},
		} {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				monitorTestRedis(t)
				requests := make(chan map[string]any, 1)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body map[string]any
					if err := common.DecodeJson(r.Body, &body); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					requests <- body
					w.Header().Set("Content-Type", "text/event-stream")
					if protocol == "messages" {
						raw, _ := common.Marshal(map[string]any{"type": "content_block_delta", "delta": map[string]string{"type": "text_delta", "text": tc.reply}})
						fmt.Fprintf(w, "data: %s\n\ndata: {\"type\":\"message_stop\"}\n\n", raw)
					} else {
						raw, _ := common.Marshal(map[string]string{"type": "response.output_text.delta", "delta": tc.reply})
						fmt.Fprintf(w, "data: %s\n\ndata: {\"type\":\"response.completed\"}\n\n", raw)
					}
				}))
				defer upstream.Close()
				cfg := monitorconfig.Default()
				cfg.BaseURL = upstream.URL
				cfg.LogicPrompt, cfg.LogicAnswer, cfg.LogicMatchMode = "global question", "21", "exact"
				var group monitorconfig.Group
				require.NoError(t, common.UnmarshalJsonStr(tc.groupJSON, &group))
				group.Protocol = protocol
				cfg.Groups["g"] = group
				active := make(chan struct{}, 1)
				tryMonitorJob(active, cfg, "g", "reasoning", "secret", "logic", 1)
				var history []MonitorResult
				require.Eventually(t, func() bool {
					history, _ = GetMonitorHistory(context.Background(), "g", "logic", cfg)
					return len(active) == 0 && len(history) == 1
				}, 5*time.Second, 10*time.Millisecond)
				require.Len(t, requests, 1)
				request := <-requests
				require.Equal(t, "reasoning", request["model"])
				if protocol == "messages" {
					require.Equal(t, []any{map[string]any{"role": "user", "content": tc.prompt}}, request["messages"])
				} else {
					require.Equal(t, tc.prompt, request["input"])
				}
				// A later configuration change must not rewrite this run's details.
				cfg.LogicPrompt, cfg.LogicAnswer, cfg.LogicMatchMode = "changed", "99", "contains"
				cfg.Groups["g"] = monitorconfig.Group{}
				record, err := GetMonitorRecord(context.Background(), "g", "logic", history[0].ID, cfg)
				require.NoError(t, err)
				require.Equal(t, tc.prompt, record.Prompt)
				require.Equal(t, tc.expected, record.Expected)
				require.Equal(t, tc.mode, record.MatchMode)
				require.Equal(t, tc.reply, record.Answer)
				require.Equal(t, tc.status, record.Status)
			})
		}
	}
}
