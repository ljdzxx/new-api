package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

// This exercises the HTTP relay and billing boundary with a local upstream.
// The Codex client owns agent execution; the gateway forwards tools and history.
// It does not prove that a remote provider supports hosted multi_agent mode.
func TestCodexSubagentResponsesRelayRoundTrip(t *testing.T) {
	tools := json.RawMessage(`[{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent","parameters":{"type":"object","properties":{"message":{"type":"string"}},"additionalProperties":false}},{"type":"function","name":"followup_task","parameters":{"type":"object"}},{"type":"function","name":"send_message","parameters":{"type":"object"}}]},{"type":"custom","name":"exec","format":{"type":"text"}}]`)
	spawn := json.RawMessage(`{"type":"function_call","id":"fc_spawn","call_id":"call_spawn","namespace":"collaboration","name":"spawn_agent","arguments":"{\"task_name\":\"worker\",\"message\":\"inspect the relay\"}","agent":{"agent_name":"/root"}}`)
	followup := json.RawMessage(`{"type":"function_call","id":"fc_followup","call_id":"call_followup","namespace":"collaboration","name":"followup_task","arguments":"{\"target\":\"worker\",\"message\":\"verify again\"}","agent":{"agent_name":"/root"}}`)
	execCall := json.RawMessage(`{"type":"custom_tool_call","id":"ctc_child","call_id":"call_child","name":"exec","input":"inspect()","agent":{"agent_name":"/root/worker"}}`)
	message := json.RawMessage(`{"type":"message","id":"msg_child","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"probe_followup_ok","annotations":[]}],"agent":{"agent_name":"/root/worker"}}`)
	stages := []struct {
		name, session string
		input, output json.RawMessage
	}{
		{"spawn", "parent-session", json.RawMessage(`[{"role":"user","content":"Delegate relay inspection"}]`), spawn},
		{"child request", "child-session", json.RawMessage(`[{"role":"user","content":"inspect the relay"},{"type":"additional_tools","tools":[{"type":"function","name":"inspect","parameters":{"type":"object"}}]}]`), execCall},
		{"parent followup", "parent-session", json.RawMessage(`[` + string(spawn) + `,{"type":"function_call_output","call_id":"call_spawn","output":"{\"task_name\":\"/root/worker\"}"},{"role":"user","content":"Ask the same worker to verify again"}]`), followup},
		{"child continuation", "child-session", json.RawMessage(`[` + string(execCall) + `,{"type":"custom_tool_call_output","call_id":"call_child","output":[{"type":"input_text","text":"probe_created_ok"}]},{"role":"user","content":"verify again"}]`), message},
		{"followup result", "parent-session", json.RawMessage(`[` + string(followup) + `,{"type":"function_call_output","call_id":"call_followup","output":"probe_followup_ok"}]`), message},
	}
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeCodex} {
		for _, stream := range []bool{false, true} {
			for _, mode := range []string{"normal", "channel_passthrough", "global_passthrough"} {
				t.Run(fmt.Sprintf("channel=%d/stream=%t/%s", channelType, stream, mode), func(t *testing.T) {
					db := setupResponsesRelayBillingTest(t)
					settings := model_setting.GetGlobalSettings()
					oldPassthrough := settings.PassThroughRequestEnabled
					settings.PassThroughRequestEnabled = mode == "global_passthrough"
					t.Cleanup(func() { settings.PassThroughRequestEnabled = oldPassthrough })
					oldTimeout, oldRetries := common.RelayTimeout, common.RetryTimes
					common.RelayTimeout, common.RetryTimes = 5, 0
					service.InitHttpClient()
					t.Cleanup(func() { common.RelayTimeout, common.RetryTimes = oldTimeout, oldRetries; service.InitHttpClient() })
					var calls atomic.Int32
					captured := make(chan struct {
						body   []byte
						header http.Header
						path   string
					}, len(stages))
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						index := int(calls.Add(1)) - 1
						if index >= len(stages) {
							http.Error(w, "unexpected retry", http.StatusBadRequest)
							return
						}
						body, err := io.ReadAll(r.Body)
						if err != nil {
							t.Error(err)
							return
						}
						captured <- struct {
							body   []byte
							header http.Header
							path   string
						}{body, r.Header.Clone(), r.URL.Path}
						response := fmt.Sprintf(`{"id":"resp_%d","object":"response","status":"completed","output":[%s],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`, index, stages[index].output)
						if stream {
							w.Header().Set("Content-Type", "text/event-stream")
							fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"agent\":{\"agent_name\":\"/root/worker\"},\"item\":%s}\n\n", stages[index].output)
							fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":%s}\n\n", response)
						} else {
							w.Header().Set("Content-Type", "application/json")
							fmt.Fprint(w, response)
						}
					}))
					defer upstream.Close()
					for index, stage := range stages {
						request := map[string]any{"model": "gpt-4o-mini", "stream": stream, "store": false, "parallel_tool_calls": false, "multi_agent": json.RawMessage(`{"enabled":true,"max_concurrent_subagents":3}`), "tools": tools, "input": stage.input, "prompt_cache_key": stage.session, "client_metadata": map[string]any{"parent_thread_id": "parent-session"}}
						// The OpenAI endpoint may also continue stored responses. This
						// checks forwarding, not the remote account's storage support.
						if channelType == constant.ChannelTypeOpenAI && index >= 2 {
							request["store"] = true
							request["previous_response_id"] = fmt.Sprintf("resp_%d", index-2)
						}
						encoded, err := common.Marshal(request)
						require.NoError(t, err)
						c, recorder, _ := newRelayMockContext(stream)
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(encoded)))
						c.Request.Header.Set("Content-Type", "application/json")
						c.Request.Header.Set("OpenAI-Beta", "responses_multi_agent=v1")
						c.Request.Header.Set("Session_id", stage.session)
						c.Request.Header.Set("X-Codex-Parent-Thread-Id", "parent-session")
						c.Request.Header.Set("X-OpenAI-Subagent", "worker")
						// Collaboration headers work without affinity or explicit overrides.
						common.SetContextKey(c, constant.ContextKeyChannelType, channelType)
						if channelType == constant.ChannelTypeCodex {
							common.SetContextKey(c, constant.ContextKeyChannelKey, `{"access_token":"fake-upstream","account_id":"fake-account"}`)
						}
						common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: mode == "channel_passthrough"})
						common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
						common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
						Relay(c, types.RelayFormatOpenAIResponses)
						require.EqualValues(t, index+1, calls.Load(), "%s: %s", stage.name, recorder.Body.String())
						require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
						actual := <-captured
						var forwarded map[string]json.RawMessage
						require.NoError(t, common.Unmarshal(actual.body, &forwarded))
						for _, key := range []string{"multi_agent", "tools", "input", "store", "parallel_tool_calls", "prompt_cache_key", "client_metadata"} {
							expected, err := common.Marshal(request[key])
							require.NoError(t, err)
							require.JSONEq(t, string(expected), string(forwarded[key]), "%s: %s", stage.name, key)
						}
						if previous, exists := request["previous_response_id"]; exists {
							expected, err := common.Marshal(previous)
							require.NoError(t, err)
							require.JSONEq(t, string(expected), string(forwarded["previous_response_id"]))
						}
						require.Equal(t, "responses_multi_agent=v1", actual.header.Get("OpenAI-Beta"))
						require.Equal(t, stage.session, actual.header.Get("Session_id"))
						require.Equal(t, "parent-session", actual.header.Get("X-Codex-Parent-Thread-Id"))
						require.Equal(t, "worker", actual.header.Get("X-OpenAI-Subagent"))
						path, auth := "/v1/responses", "Bearer sk-upstream"
						if channelType == constant.ChannelTypeCodex {
							path, auth = "/backend-api/codex/responses", "Bearer fake-upstream"
						}
						require.Equal(t, path, actual.path)
						require.Equal(t, auth, actual.header.Get("Authorization"))
						var response map[string]json.RawMessage
						if stream {
							for _, line := range strings.Split(recorder.Body.String(), "\n") {
								if !strings.HasPrefix(line, "data: ") {
									continue
								}
								var event struct {
									Type     string                     `json:"type"`
									Response map[string]json.RawMessage `json:"response"`
									Item     json.RawMessage            `json:"item"`
									Agent    json.RawMessage            `json:"agent"`
								}
								require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(line, "data: "), &event))
								if event.Type == "response.output_item.done" {
									require.JSONEq(t, string(stage.output), string(event.Item))
									require.NotEmpty(t, event.Agent)
								}
								if event.Type == "response.completed" {
									response = event.Response
								}
							}
						} else {
							require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
						}
						require.JSONEq(t, "["+string(stage.output)+"]", string(response["output"]))
					}
					var logs []model.Log
					require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
					require.Len(t, logs, len(stages), "each parent/child request is independently settled")
					for _, log := range logs {
						require.Positive(t, log.Quota)
						require.Equal(t, 10, log.PromptTokens)
						require.Equal(t, 5, log.CompletionTokens)
					}
				})
			}
		}
	}
}
