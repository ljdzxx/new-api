package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestChatUpstreamUsageExcludesLocalEstimates(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, tc := range []struct {
			name, usage   string
			reported      bool
			input, output int
		}{
			{name: "missing"},
			{name: "null", usage: `,"usage":null`},
			{name: "zero", usage: `,"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`, reported: true},
			{name: "input only", usage: `,"usage":{"prompt_tokens":100,"completion_tokens":0,"total_tokens":100}`, reported: true, input: 100},
			{name: "output only", usage: `,"usage":{"prompt_tokens":0,"completion_tokens":20,"total_tokens":20}`, reported: true, output: 20},
			{name: "complete", usage: `,"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}`, reported: true, input: 100, output: 20},
		} {
			name := "sync/" + tc.name
			if stream {
				name = "stream/" + tc.name
			}
			t.Run(name, func(t *testing.T) {
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
				info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "gpt-4o-mini"}, RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, IsStream: stream}
				info.SetEstimatePromptTokens(200653)
				body := `{"id":"chatcmpl_usage","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]` + tc.usage + `}`
				if stream {
					body = "data: " + `{"id":"chatcmpl_usage","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"}}]` + tc.usage + "}\n\ndata: [DONE]\n\n"
				}
				resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
				if stream {
					_, err := OaiStreamHandler(ctx, info, resp)
					require.Nil(t, err)
				} else {
					_, err := OpenaiHandler(ctx, info, resp)
					require.Nil(t, err)
				}
				require.Equal(t, tc.reported, info.UpstreamUsageReported)
				actual := info.GetUpstreamUsage()
				if !tc.reported {
					require.Nil(t, actual)
				} else {
					require.Equal(t, tc.input, actual.PromptTokens)
					require.Equal(t, tc.output, actual.CompletionTokens)
				}
			})
		}
	}
}

func TestRealtimeUpstreamUsageDoesNotChargeLocalEvents(t *testing.T) {
	pair := func(t *testing.T) (*websocket.Conn, *websocket.Conn) {
		accepted := make(chan *websocket.Conn, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
			if err == nil {
				accepted <- conn
			}
		}))
		t.Cleanup(server.Close)
		client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
		require.NoError(t, err)
		peer := <-accepted
		t.Cleanup(func() { client.Close(); peer.Close() })
		return client, peer
	}
	for _, reported := range []bool{false, true} {
		name := "missing"
		if reported {
			name = "explicit zero"
		}
		t.Run(name, func(t *testing.T) {
			client, gatewayClient := pair(t)
			gatewayTarget, upstream := pair(t)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
			info := &relaycommon.RelayInfo{ClientWs: gatewayClient, TargetWs: gatewayTarget, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o-realtime-preview"}}
			finished := make(chan *dto.RealtimeUsage, 1)
			go func() { _, usage := OpenaiRealtimeHandler(ctx, info); finished <- usage }()
			require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello world"}]}}`)))
			require.NoError(t, upstream.SetReadDeadline(time.Now().Add(5*time.Second)))
			_, _, err := upstream.ReadMessage()
			require.NoError(t, err)
			usageJSON := ""
			if reported {
				usageJSON = `,"usage":{"total_tokens":0,"input_tokens":0,"output_tokens":0}`
			}
			require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"output":[{"type":"message","content":[{"type":"text","text":"hello"}]}]`+usageJSON+`}}`)))
			require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
			_, _, err = client.ReadMessage()
			require.NoError(t, err)
			client.Close()
			select {
			case usage := <-finished:
				require.Zero(t, usage.TotalTokens)
				require.Zero(t, info.WssConsumedQuota)
				require.Equal(t, reported, info.UpstreamUsageReported)
			case <-time.After(5 * time.Second):
				t.Fatal("realtime relay did not finish")
			}
		})
	}
}
