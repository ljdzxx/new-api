package controller

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResponsesWebSocketInjectionAndContinuation(t *testing.T) {
	db := setupResponsesRelayBillingTest(t)
	oldMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
	require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.ChannelDailyMark{}))
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 77).Update("key", "wstesttoken").Error)
	var handshakes atomic.Int32
	received := make(chan []byte, 8)
	serverErrors := make(chan error, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handshakes.Add(1)
		if r.Header.Get("Authorization") != "Bearer sk-upstream" || r.Header.Get("OpenAI-Beta") != "responses_multi_agent=v1" || r.Header.Get("X-OpenAI-Subagent") != "worker" {
			serverErrors <- fmt.Errorf("incorrect upstream headers: %v", r.Header)
			http.Error(w, "headers", 400)
			return
		}
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer ws.Close()
		_ = ws.SetReadDeadline(time.Now().Add(15 * time.Second))
		for n := 0; n < 2; n++ {
			_, body, err := ws.ReadMessage()
			if err != nil {
				serverErrors <- err
				return
			}
			received <- body
			id := fmt.Sprintf("resp_ws_audit_%d", n)
			_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"response.created","response":{"id":%q}}`, id)))
			if n == 0 {
				_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_item.done","agent":{"agent_name":"/root/worker"},"item":{"type":"function_call","id":"fc_probe","call_id":"call_probe","name":"inspect","arguments":"{}"}}`))
				_, injection, err := ws.ReadMessage()
				if err != nil {
					serverErrors <- err
					return
				}
				received <- injection
				_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"response.inject.created","response_id":%q}`, id)))
			}
			_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"status":"completed","output":[{"type":"agent_message","agent":{"agent_name":"/root"},"content":[{"type":"encrypted_content","encrypted_content":"opaque"}]}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`, id)))
		}
		// Keep the persistent connection open until the client closes it.
		_, _, _ = ws.ReadMessage()
	}))
	defer upstream.Close()
	settings, _ := common.Marshal(dto.ChannelSettings{ResponsesWebSocketEnabled: true, ModelHealthEnabled: common.GetPointer(false)})
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 88).Updates(map[string]any{"base_url": upstream.URL, "setting": string(settings)}).Error)
	var channel model.Channel
	require.NoError(t, db.First(&channel, 88).Error)
	require.NoError(t, channel.AddAbilities(nil))
	service.InitHttpClient()
	engine := gin.New()
	engine.GET("/v1/responses", middleware.TokenAuth(), ResponsesWebSocket)
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	headers := http.Header{"Authorization": []string{"Bearer wstesttoken"}, "OpenAI-Beta": []string{"responses_multi_agent=v1"}, "X-OpenAI-Subagent": []string{"worker"}}
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/v1/responses", headers)
	require.NoError(t, err)
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(20 * time.Second))
	create := []byte(`{"type":"response.create","model":"gpt-4o-mini","multi_agent":{"enabled":true},"input":[{"role":"user","content":"inspect"}],"max_output_tokens":32}`)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, create))
	accepted := false
	for {
		_, body, err := client.ReadMessage()
		require.NoError(t, err)
		kind := gjson.GetBytes(body, "type").String()
		require.NotEqual(t, "error", kind, string(body))
		if kind == "response.output_item.done" {
			require.Equal(t, "/root/worker", gjson.GetBytes(body, "agent.agent_name").String())
			require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.inject","response_id":"resp_ws_audit_0","input":[{"type":"function_call_output","call_id":"call_probe","output":{"integer":9007199254740993,"metadata":[{"encrypted_content":"plain tool data"}]}}]}`)))
		}
		if kind == "response.inject.created" {
			accepted = true
		}
		if kind == "response.completed" {
			require.True(t, accepted)
			break
		}
	}
	createBody := <-received
	require.True(t, gjson.GetBytes(createBody, "multi_agent.enabled").Bool())
	require.False(t, gjson.GetBytes(createBody, "stream").Exists())
	injection := <-received
	require.Equal(t, "response.inject", gjson.GetBytes(injection, "type").String())
	require.Contains(t, string(injection), "9007199254740993")
	require.Contains(t, string(injection), "plain tool data")
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-4o-mini","previous_response_id":"resp_ws_audit_0","multi_agent":{"enabled":true},"input":[{"role":"user","content":"continue"}]}`)))
	for {
		_, body, err := client.ReadMessage()
		require.NoError(t, err)
		kind := gjson.GetBytes(body, "type").String()
		require.NotEqual(t, "error", kind, string(body))
		if kind == "response.completed" {
			break
		}
	}
	require.Equal(t, "resp_ws_audit_0", gjson.GetBytes(<-received, "previous_response_id").String())
	require.EqualValues(t, 1, handshakes.Load())
	var logs int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
	require.EqualValues(t, 2, logs)
	// Revocation is checked on the next frame, not only on the HTTP handshake.
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 77).Update("status", common.TokenStatusDisabled).Error)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, create))
	_, rejection, err := client.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "error", gjson.GetBytes(rejection, "type").String())
	require.EqualValues(t, 401, gjson.GetBytes(rejection, "status").Int())
	select {
	case err := <-serverErrors:
		require.NoError(t, err)
	default:
	}
}

func TestResponsesWebSocketCreateBodyPreservesOpaqueValues(t *testing.T) {
	raw := []byte(`{"type":"response.create","event_id":"evt","model":"m","multi_agent":{"enabled":false},"input":[{"type":"agent_message","id":"opaque","content":[{"encrypted_content":"short"}]}],"tools":[{"const":9007199254740993}]}`)
	body, err := responsesWSRequestBody(raw)
	require.NoError(t, err)
	require.Contains(t, string(body), "9007199254740993")
	require.Contains(t, string(body), "short")
	require.False(t, gjson.GetBytes(body, "multi_agent.enabled").Bool())
	require.True(t, gjson.GetBytes(body, "stream").Bool())
}

func TestResponsesWebSocketTerminalLifecycle(t *testing.T) {
	for _, scenario := range []string{"complete_close", "failed_close", "disconnect", "disconnect_usage", "cancel", "late_ack_duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupResponsesRelayBillingTest(t)
			oldMemory := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = false
			t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
			require.NoError(t, db.AutoMigrate(&model.Ability{}, &model.ChannelDailyMark{}))
			require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 77).Update("key", "wslifecycle").Error)
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			serverErrors := make(chan error, 4)
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					serverErrors <- err
					return
				}
				defer ws.Close()
				_ = ws.SetReadDeadline(time.Now().Add(10 * time.Second))
				if _, _, err = ws.ReadMessage(); err != nil {
					serverErrors <- err
					return
				}
				requests.Add(1)
				send := func(body string) bool {
					if err := ws.WriteMessage(websocket.TextMessage, []byte(body)); err != nil {
						serverErrors <- err
						return false
					}
					return true
				}
				if !send(`{"type":"response.created","response":{"id":"resp_ws_lifecycle"}}`) {
					return
				}
				if scenario == "disconnect_usage" {
					if !send(`{"type":"response.in_progress","response":{"id":"resp_ws_lifecycle","usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`) {
						return
					}
				}
				if !send(`{"type":"response.output_text.delta","delta":"Hello world"}`) {
					return
				}
				<-release
				kind, status := "response.completed", "completed"
				if strings.HasPrefix(scenario, "disconnect") {
					return
				}
				if scenario == "cancel" {
					_, frame, err := ws.ReadMessage()
					if err != nil {
						serverErrors <- err
						return
					}
					if gjson.GetBytes(frame, "type").String() != "response.cancel" {
						serverErrors <- fmt.Errorf("expected cancel, got %s", frame)
						return
					}
					kind, status = "response.cancelled", "cancelled"
				}
				if scenario == "failed_close" {
					kind, status = "response.failed", "failed"
				}
				terminal := fmt.Sprintf(`{"type":%q,"agent":{"agent_name":"/root/worker"},"response":{"id":"resp_ws_lifecycle","status":%q,"output":[],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}`, kind, status)
				if !send(terminal) {
					return
				}
				if scenario == "late_ack_duplicate" {
					if !send(terminal) {
						return
					}
					_, injection, err := ws.ReadMessage()
					if err != nil {
						serverErrors <- err
						return
					}
					if gjson.GetBytes(injection, "type").String() != "response.inject" {
						serverErrors <- fmt.Errorf("expected injection, got %s", injection)
						return
					}
					send(`{"type":"response.inject.created","response_id":"resp_ws_lifecycle"}`)
				}
			}))
			defer upstream.Close()
			settings, _ := common.Marshal(dto.ChannelSettings{ResponsesWebSocketEnabled: true, ModelHealthEnabled: common.GetPointer(false)})
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 88).Updates(map[string]any{"base_url": upstream.URL, "setting": string(settings)}).Error)
			var channel model.Channel
			require.NoError(t, db.First(&channel, 88).Error)
			require.NoError(t, channel.AddAbilities(nil))
			service.InitHttpClient()
			finished := make(chan struct{})
			engine := gin.New()
			engine.GET("/v1/responses", middleware.TokenAuth(), func(c *gin.Context) { defer close(finished); ResponsesWebSocket(c) })
			gateway := httptest.NewServer(engine)
			defer gateway.Close()
			client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+"/v1/responses", http.Header{"Authorization": []string{"Bearer wslifecycle"}})
			require.NoError(t, err)
			defer client.Close()
			_ = client.SetReadDeadline(time.Now().Add(15 * time.Second))
			create := []byte(`{"type":"response.create","model":"gpt-4o-mini","input":"hello","max_output_tokens":32}`)
			require.NoError(t, client.WriteMessage(websocket.TextMessage, create))
			expected := []string{"response.created", "response.output_text.delta"}
			if scenario == "disconnect_usage" {
				expected = []string{"response.created", "response.in_progress", "response.output_text.delta"}
			}
			for _, want := range expected {
				_, frame, err := client.ReadMessage()
				require.NoError(t, err)
				require.Equal(t, want, gjson.GetBytes(frame, "type").String(), string(frame))
			}
			require.NoError(t, client.WriteMessage(websocket.TextMessage, create))
			_, frame, err := client.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, "error", gjson.GetBytes(frame, "type").String())
			require.EqualValues(t, 409, gjson.GetBytes(frame, "status").Int())
			if scenario == "cancel" {
				require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.cancel","response_id":"resp_ws_lifecycle"}`)))
			}
			unblock()
			_, frame, err = client.ReadMessage()
			require.NoError(t, err)
			want := "response.completed"
			switch scenario {
			case "cancel":
				want = "response.cancelled"
			case "failed_close", "disconnect", "disconnect_usage":
				want = "response.failed"
			}
			require.Equal(t, want, gjson.GetBytes(frame, "type").String(), string(frame))
			if !strings.HasPrefix(scenario, "disconnect") {
				require.Equal(t, "/root/worker", gjson.GetBytes(frame, "agent.agent_name").String())
			}
			if scenario == "late_ack_duplicate" {
				require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.inject","response_id":"resp_ws_lifecycle","input":[{"type":"function_call_output","call_id":"late_call","output":"done"}]}`)))
				_, frame, err = client.ReadMessage()
				require.NoError(t, err)
				require.Equal(t, "response.inject.created", gjson.GetBytes(frame, "type").String(), string(frame))
			}
			_, _, err = client.ReadMessage()
			require.Error(t, err, "upstream close must close the client without another terminal")
			select {
			case <-finished:
			case <-time.After(10 * time.Second):
				t.Fatal("gateway workers did not finish")
			}
			require.EqualValues(t, 1, requests.Load())
			var logs []model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			require.Len(t, logs, 1)
			if scenario == "disconnect" {
				require.GreaterOrEqual(t, logs[0].Quota, 0)
			} else {
				require.Positive(t, logs[0].Quota)
			}
			require.False(t, gjson.Get(logs[0].Other, "stream_canceled_by_downstream").Bool(), logs[0].Other)
			if scenario != "disconnect" {
				require.Equal(t, 10, logs[0].PromptTokens)
				require.Equal(t, 5, logs[0].CompletionTokens)
			}
			var user model.User
			var token model.Token
			require.NoError(t, db.First(&user, 42).Error)
			require.NoError(t, db.First(&token, 77).Error)
			require.Equal(t, 1000000-logs[0].Quota, user.Quota)
			require.Equal(t, user.Quota, token.RemainQuota)
			select {
			case err := <-serverErrors:
				require.NoError(t, err)
			default:
			}
		})
	}
}
