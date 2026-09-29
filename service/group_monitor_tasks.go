package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/groupmonitor"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

type MonitorResult struct {
	Group         string   `json:"group,omitempty"`
	diagnostic    error    // Never serialized into the public monitor API.
	ID            string   `json:"id,omitempty"`
	Model         string   `json:"model,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Prompt        string   `json:"prompt,omitempty"`
	Status        string   `json:"status,omitempty"`
	FinishedAt    int64    `json:"finished_at,omitempty"`
	DurationMS    int64    `json:"duration_ms,omitempty"`
	MatchMode     string   `json:"match_mode,omitempty"`
	ArtifactError string   `json:"artifact_error,omitempty"`
	At            int64    `json:"at"`
	OK            bool     `json:"ok"`
	Error         string   `json:"error,omitempty"`
	TTFT          *float64 `json:"ttft,omitempty"`
	Answer        string   `json:"answer,omitempty"`
	Expected      string   `json:"expected,omitempty"`
}

func monitorResponsesURL(base string) string {
	return monitorEndpoint(base, "responses")
}

func monitorEndpoint(base, protocol string) string {
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(strings.TrimSuffix(base, "/responses"), "/messages")
	if strings.HasSuffix(base, "/v1") {
		return base + "/" + protocol
	}
	return base + "/v1/" + protocol
}

// Responses emits lifecycle events before text. These count as first events,
// consistently with the configured real-request TTFT definition.
func runMonitorResponse(ctx context.Context, cfg monitorconfig.Config, key, model, prompt string, protocols ...string) (MonitorResult, string) {
	result := MonitorResult{At: time.Now().UnixMilli()}
	protocol := "responses"
	if len(protocols) > 0 && protocols[0] != "" {
		protocol = protocols[0]
	}
	if protocol != "responses" && protocol != "messages" {
		result.Error = "unsupported_protocol"
		return result, ""
	}
	payload := map[string]any{"model": model, "input": prompt, "stream": true, "store": false}
	if protocol == "messages" {
		payload = map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": prompt}}, "stream": true}
	}
	body, err := common.Marshal(payload)
	if err != nil {
		result.Error = "request_encoding_failed"
		return result, ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, monitorEndpoint(cfg.BaseURL, protocol), bytes.NewReader(body))
	if err != nil {
		result.Error = "invalid_probe_url"
		return result, ""
	}
	req.Header.Set("Content-Type", "application/json")
	if protocol == "messages" {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		result.Error = "probe_connection_failed"
		result.diagnostic = err
		if errors.Is(err, context.DeadlineExceeded) {
			result.Error = "probe_timeout"
		}
		if errors.Is(err, context.Canceled) {
			result.Error = "probe_cancelled"
		}
		return result, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		return result, ""
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		result.Error = "expected_sse_response"
		return result, ""
	}
	text, err := readMonitorSSE(resp.Body, start, &result, protocol)
	if err != nil {
		result.Error = monitorErrorCode(err, err.Error())
		result.diagnostic = err
		if errors.Is(err, context.DeadlineExceeded) {
			result.Error = "probe_timeout"
		}
		if errors.Is(err, context.Canceled) {
			result.Error = "probe_cancelled"
		}
		return result, text
	}
	result.OK = true
	return result, text
}

func readMonitorSSE(reader io.Reader, start time.Time, result *MonitorResult, protocols ...string) (string, error) {
	protocol := "responses"
	if len(protocols) > 0 && protocols[0] != "" {
		protocol = protocols[0]
	}
	scanner := bufio.NewScanner(io.LimitReader(reader, 8<<20))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	var output strings.Builder
	var data []string
	completed := false
	process := func() error {
		if len(data) == 0 {
			return nil
		}
		raw := strings.Join(data, "\n")
		data = nil
		if strings.TrimSpace(raw) == "" || raw == "[DONE]" {
			return nil
		}
		var event struct {
			Type         string          `json:"type"`
			Delta        json.RawMessage `json:"delta"`
			ContentBlock struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content_block"`
			Response struct {
				Status string `json:"status"`
				Output []struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"output"`
			} `json:"response"`
		}
		if common.UnmarshalJsonStr(raw, &event) != nil {
			return fmt.Errorf("invalid_sse_json")
		}
		if protocol == "messages" {
			var delta struct {
				Type       string `json:"type"`
				Text       string `json:"text"`
				StopReason string `json:"stop_reason"`
			}
			if len(event.Delta) > 0 && common.Unmarshal(event.Delta, &delta) != nil {
				return fmt.Errorf("invalid_sse_json")
			}
			text := ""
			switch event.Type {
			case "error":
				return fmt.Errorf("messages_error")
			case "content_block_start":
				if event.ContentBlock.Type == "text" {
					text = event.ContentBlock.Text
				}
			case "content_block_delta":
				if delta.Type == "text_delta" {
					text = delta.Text
				}
			case "message_delta":
				if delta.StopReason == "max_tokens" {
					return fmt.Errorf("response_incomplete")
				}
			case "message_stop":
				completed = true
			}
			if output.Len()+len(text) > 1<<20 {
				return fmt.Errorf("response_too_large")
			}
			output.WriteString(text)
			return nil
		}
		switch event.Type {
		case "error", "response.failed", "response.incomplete":
			return fmt.Errorf("%s", event.Type)
		case "response.output_text.delta":
			var delta string
			if common.Unmarshal(event.Delta, &delta) != nil {
				return fmt.Errorf("invalid_sse_json")
			}
			if output.Len()+len(delta) > 1<<20 {
				return fmt.Errorf("response_too_large")
			}
			output.WriteString(delta)
		case "response.completed":
			if event.Response.Status != "" && event.Response.Status != "completed" {
				return fmt.Errorf("response_not_completed")
			}
			completed = true
			if output.Len() == 0 {
				for _, item := range event.Response.Output {
					for _, content := range item.Content {
						if content.Type == "output_text" {
							if output.Len()+len(content.Text) > 1<<20 {
								return fmt.Errorf("response_too_large")
							}
							output.WriteString(content.Text)
						}
					}
				}
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if result.TTFT == nil && (strings.HasPrefix(line, "data:") || strings.HasPrefix(line, "event:")) {
			v := float64(time.Since(start).Microseconds()) / 1000
			result.TTFT = &v
		}
		if line == "" {
			if err := process(); err != nil {
				return output.String(), err
			}
			if completed {
				return output.String(), nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return output.String(), monitorError("sse_read_failed", err)
	}
	if err := process(); err != nil {
		return output.String(), err
	}
	if !completed {
		return output.String(), fmt.Errorf("response_not_completed")
	}
	return output.String(), nil
}

var releaseMonitorLease = redis.NewScript(`if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`)

type monitorJob struct {
	model, kind string
	minutes     int
}

func monitorGroupJobs(cfg monitorconfig.Config, g monitorconfig.Group) []monitorJob {
	if !g.Visible() {
		return nil
	}
	jobs := make([]monitorJob, 0, len(g.Models)+2)
	for _, m := range g.Models {
		jobs = append(jobs, monitorJob{m, "probe", cfg.ProbeMinutes})
	}
	if g.TestSVG() {
		jobs = append(jobs, monitorJob{g.SVGModel, "svg", cfg.SVGMinutes})
	}
	if g.TestLogic() {
		jobs = append(jobs, monitorJob{g.LogicModel, "logic", cfg.LogicMinutes})
	}
	return jobs
}

func StartGroupMonitorTasks() {
	if !common.IsMasterNode {
		return
	}
	go func() {
		active := make(chan struct{}, 10)
		var lastMaintenance time.Time
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			cfg := monitorconfig.Get()
			if !cfg.Enabled || !groupmonitor.Ready() {
				continue
			}
			if err := monitorconfig.Validate(cfg); err != nil {
				continue
			}
			if time.Since(lastMaintenance) >= time.Minute {
				lastMaintenance = time.Now()
				go maintainMonitorArtworks(cfg)
			}
			for group, g := range cfg.Groups {
				for _, job := range monitorGroupJobs(cfg, g) {
					tryMonitorJob(active, cfg, group, job.model, g.Key, job.kind, job.minutes)
				}
			}
		}
	}()
}

func tryMonitorJob(active chan struct{}, cfg monitorconfig.Config, group, model, key, kind string, minutes int) {
	if !cfg.Groups[group].Visible() || len(active) >= cfg.Concurrency {
		return
	}
	// Due jobs alone consume worker slots; otherwise the first two configured
	// models would starve the remaining models on every scheduler tick.
	checkCtx, checkDone := context.WithTimeout(context.Background(), time.Second)
	configBytes, _ := common.Marshal(cfg)
	fingerprint := sha256.Sum256(configBytes)
	dueKey := groupmonitor.Key(group, model, fmt.Sprintf("%s:due:%x", kind, fingerprint[:8]))
	due, checkErr := common.RDB.Exists(checkCtx, dueKey).Result()
	checkDone()
	if checkErr != nil || due != 0 {
		return
	}
	select {
	case active <- struct{}{}:
	default:
		return
	}
	go func() {
		defer func() { <-active }()
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second+3*time.Minute)
		defer cancel()
		lockModel := model
		if kind == "svg" {
			lockModel = ""
		}
		lease := groupmonitor.Key(group, lockModel, kind+":lock")
		owner := uuid.NewString()
		ok, err := common.RDB.SetNX(ctx, lease, owner, time.Duration(cfg.TimeoutSeconds)*time.Second+4*time.Minute).Result()
		if err != nil || !ok {
			return
		}
		defer func() {
			releaseCtx, done := context.WithTimeout(context.Background(), time.Second)
			defer done()
			_ = releaseMonitorLease.Run(releaseCtx, common.RDB, []string{lease}, owner).Err()
		}()
		ok, err = common.RDB.SetNX(ctx, dueKey, "1", time.Duration(minutes)*time.Minute).Result()
		if err != nil || !ok {
			return
		}
		prompt := "Reply with OK."
		if kind == "logic" {
			prompt = cfg.LogicPrompt
		}
		if kind == "svg" {
			prompt = cfg.SVGPrompt + "\nReturn a self-contained HTML document or SVG drawing with inline styles, without scripts or external resources."
		}
		probeCtx, done := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
		result, answer := runMonitorResponse(probeCtx, cfg, key, model, prompt, cfg.Groups[group].Protocol)
		done()
		result.Group = group
		result.ID = fmt.Sprintf("%013d-%s", result.At, owner)
		result.Model, result.Kind, result.Prompt, result.Answer = model, kind, prompt, answer
		result.FinishedAt = time.Now().UnixMilli()
		result.DurationMS = result.FinishedAt - result.At
		result.Status = "request_failed"
		if result.OK {
			result.Status = "success"
		}
		if kind == "logic" {
			result.Expected, result.MatchMode = cfg.LogicAnswer, cfg.LogicMatchMode
			if result.OK && !monitorLogicMatches(answer, cfg.LogicAnswer, cfg.LogicMatchMode) {
				result.OK, result.Status, result.Error = false, "test_failed", "answer_mismatch"
			}
		}
		if kind == "svg" {
			if err := storeMonitorArtwork(ctx, cfg, group, &result); err != nil {
				result.ArtifactError = monitorErrorCode(err, "artwork_processing_failed")
				common.SysError(fmt.Sprintf("group monitor artifact: job=%q group=%q model=%q code=%q detail=%q", owner, group, model, result.ArtifactError, err.Error()))
			}
		}
		if !result.OK {
			detail := result.Error
			if result.diagnostic != nil {
				detail = result.diagnostic.Error()
			}
			if key != "" {
				detail = strings.ReplaceAll(detail, key, "[redacted]")
			}
			// Quote diagnostic fields to keep each failure on one log line.
			common.SysError(fmt.Sprintf("group monitor failure: job=%q group=%q model=%q kind=%q code=%q detail=%q", owner, group, model, kind, result.Error, detail))
		}
		resultCtx, resultDone := context.WithTimeout(context.Background(), 3*time.Second)
		defer resultDone()
		if kind == "probe" {
			err = groupmonitor.Write(resultCtx, group, model, "probe", groupmonitor.Sample{ID: owner, At: result.At, Success: result.OK, TTFT: result.TTFT})
			if err != nil {
				common.SysError("group monitor probe write failed: " + err.Error())
			}
		}
		if kind == "probe" {
			raw, marshalErr := common.Marshal(result)
			err = marshalErr
			if err == nil {
				err = common.RDB.Set(resultCtx, groupmonitor.Key(group, model, kind+":latest"), raw, monitorRecordTTL).Err()
			}
		} else {
			err = saveMonitorTestResult(resultCtx, group, result, cfg)
		}
		if err != nil {
			common.SysError("group monitor result write failed: " + err.Error())
		}
	}()
}
