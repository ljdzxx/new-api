package relay

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const reasoningRecoveryBody = `{"model":"test-model","store":false,"input":[{"role":"user","content":"do it"},{"type":"reasoning","id":"rs_old","encrypted_content":"gAAAAAB_old_account_reasoning_ciphertext"},{"type":"function_call","id":"fc_old","call_id":"call_1","name":"run","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"done"}]}`
const encryptedErrorJSON = `{"error":{"type":"invalid_request_error","code":"invalid_encrypted_content","message":"ciphertext rejected"}}`
const genericUpstreamErrorJSON = `{"error":{"type":"server_error","code":"server_error","message":"upstream unavailable"}}`

type reasoningRecoveryAdaptor struct {
	relaychannel.Adaptor
	request func(*gin.Context, *relaycommon.RelayInfo, io.Reader) (any, error)
}

func (a *reasoningRecoveryAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	return a.request(c, info, body)
}

func TestResponsesReasoningRecoveryBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, response, body            string
		status, calls                   int
		stream, cancel, repeat, compact bool
		allowRetry, started, billable   bool
	}{
		{name: "HTTP rejection", status: 400, response: encryptedErrorJSON, calls: 2},
		{name: "HTTP 502 with reasoning", status: 502, response: genericUpstreamErrorJSON, calls: 2},
		{name: "HTTP 503 with reasoning", status: 503, response: genericUpstreamErrorJSON, calls: 2},
		{name: "HTTP 502 HTML body", status: 502, response: "<html>Bad Gateway</html>", calls: 2},
		{name: "HTTP 503 empty body", status: 503, calls: 2},
		{name: "HTTP 502 streaming request", status: 502, response: genericUpstreamErrorJSON, stream: true, calls: 2},
		{name: "HTTP 503 streaming request", status: 503, response: genericUpstreamErrorJSON, stream: true, calls: 2},
		{name: "HTTP 500 excluded", status: 500, response: genericUpstreamErrorJSON, calls: 1, allowRetry: true},
		{name: "HTTP 504 excluded", status: 504, response: genericUpstreamErrorJSON, calls: 1, allowRetry: true},
		{name: "HTTP 502 without reasoning", status: 502, response: genericUpstreamErrorJSON, body: `{"input":[{"role":"user","content":"hi"}]}`, calls: 1, allowRetry: true},
		{name: "HTTP 503 plaintext reasoning", status: 503, response: genericUpstreamErrorJSON, body: `{"input":[{"type":"reasoning","summary":[]},{"role":"user","content":"hi"}]}`, calls: 1, allowRetry: true},
		{name: "HTTP 502 empty ciphertext", status: 502, response: genericUpstreamErrorJSON, body: `{"input":[{"type":"reasoning","encrypted_content":""},{"role":"user","content":"hi"}]}`, calls: 1, allowRetry: true},
		{name: "HTTP 503 null ciphertext", status: 503, response: genericUpstreamErrorJSON, body: `{"input":[{"type":"reasoning","encrypted_content":null},{"role":"user","content":"hi"}]}`, calls: 1, allowRetry: true},
		{name: "HTTP 502 non-string ciphertext", status: 502, response: genericUpstreamErrorJSON, body: `{"input":[{"type":"reasoning","encrypted_content":123},{"role":"user","content":"hi"}]}`, calls: 1, allowRetry: true},
		{name: "HTTP 503 metadata excluded", status: 503, response: genericUpstreamErrorJSON, body: `{"metadata":{"type":"reasoning","encrypted_content":"opaque"},"input":"hi"}`, calls: 1, allowRetry: true},
		{name: "HTTP 502 nested tool data excluded", status: 502, response: genericUpstreamErrorJSON, body: `{"input":[{"type":"function_call_output","output":{"type":"reasoning","encrypted_content":"opaque"}}]}`, calls: 1, allowRetry: true},
		{name: "HTTP 503 compaction only excluded", status: 503, response: genericUpstreamErrorJSON, body: `{"input":[{"type":"compaction","encrypted_content":"opaque"},{"role":"user","content":"hi"}]}`, calls: 1, allowRetry: true},
		{name: "HTTP 502 compaction is not dropped", status: 502, response: genericUpstreamErrorJSON, body: `{"input":[{"type":"compaction","encrypted_content":"opaque"},{"type":"reasoning","encrypted_content":"reasoning"},{"role":"user","content":"hi"}]}`, calls: 1},
		{name: "HTTP 503 server history excluded", status: 503, response: genericUpstreamErrorJSON, body: `{"previous_response_id":"resp_old","input":[{"type":"reasoning","encrypted_content":"reasoning"},{"role":"user","content":"hi"}]}`, calls: 1},
		{name: "HTTP 502 already streaming", status: 502, response: genericUpstreamErrorJSON, started: true, calls: 1},
		{name: "HTTP 503 billable output", status: 503, response: genericUpstreamErrorJSON, billable: true, calls: 1},
		{name: "HTTP 502 client canceled", status: 502, response: genericUpstreamErrorJSON, cancel: true, calls: 1},
		{name: "HTTP 502 only one recovery", status: 502, response: genericUpstreamErrorJSON, repeat: true, calls: 2},
		{name: "HTTP 503 only one recovery", status: 503, response: genericUpstreamErrorJSON, repeat: true, calls: 2},
		{name: "HTTP 503 compact endpoint excluded", status: 503, response: genericUpstreamErrorJSON, compact: true, calls: 1},
		{name: "200 JSON rejection", status: 200, response: encryptedErrorJSON, calls: 2},
		{name: "SSE first event", status: 200, response: `data: {"type":"error","code":"invalid_encrypted_content","message":"ciphertext rejected"}` + "\n\n", stream: true, calls: 2},
		{name: "SSE nested error", status: 200, response: `data: {"type":"error","error":{"code":"invalid_encrypted_content","message":"ciphertext rejected"}}` + "\n\n", stream: true, calls: 2},
		{name: "SSE failed first event", status: 200, response: `data: {"type":"response.failed","response":{"error":{"code":"invalid_encrypted_content","message":"ciphertext rejected"}}}` + "\n\n", stream: true, calls: 2},
		{name: "SSE generic failure is not HTTP 502", status: 200, response: `data: {"type":"error","code":"server_error","message":"upstream unavailable"}` + "\n\n", stream: true, calls: 1, allowRetry: true},
		{name: "SSE parse failure is not HTTP 502", status: 200, response: "data: {invalid JSON}\n\n", stream: true, calls: 1, allowRetry: true},
		{name: "already streaming", status: 200, response: "data: {\"type\":\"response.created\"}\n\n" + `data: {"type":"error","code":"invalid_encrypted_content","message":"ciphertext rejected"}` + "\n\n", stream: true, calls: 1},
		{name: "failed with usage", status: 200, response: `data: {"type":"response.failed","response":{"error":{"code":"invalid_encrypted_content","message":"ciphertext rejected"},"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}` + "\n\n", stream: true, calls: 1},
		{name: "client canceled", status: 400, response: encryptedErrorJSON, cancel: true, calls: 1},
		{name: "only one recovery", status: 400, response: encryptedErrorJSON, repeat: true, calls: 2},
		{name: "missing field is not decryption error", status: 400, response: `{"error":{"type":"invalid_request_error","code":"missing_required_parameter","message":"Missing input[0].encrypted_content"}}`, calls: 1, allowRetry: true},
		{name: "compaction is not dropped", status: 400, response: encryptedErrorJSON, body: `{"input":[{"type":"compaction","encrypted_content":"opaque"},{"type":"reasoning","encrypted_content":"reasoning"},{"role":"user","content":"hi"}]}`, calls: 1},
		{name: "compact endpoint excluded", status: 400, response: encryptedErrorJSON, compact: true, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, IsStream: tc.stream, DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 7, ApiKey: "same-key", ChannelMultiKeyIndex: 2}}
			if tc.compact {
				info.RelayMode = relayconstant.RelayModeResponsesCompact
			}
			if tc.started {
				require.NoError(t, helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.created"}, `{"type":"response.created"}`))
			}
			if tc.billable {
				common.SetContextKey(c, constant.ContextKeyResponsesBillableStreamOutput, true)
			}
			if tc.cancel {
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			body := tc.body
			if body == "" {
				body = reasoningRecoveryBody
			}
			calls := 0
			adaptor := &reasoningRecoveryAdaptor{Adaptor: &openai.Adaptor{}, request: func(_ *gin.Context, gotInfo *relaycommon.RelayInfo, reader io.Reader) (any, error) {
				calls++
				require.Same(t, info, gotInfo)
				require.Equal(t, "same-key", gotInfo.ApiKey)
				require.Equal(t, 2, gotInfo.ChannelMultiKeyIndex)
				require.Equal(t, 7, gotInfo.ChannelId)
				payload, err := io.ReadAll(reader)
				require.NoError(t, err)
				if calls == 1 {
					require.Equal(t, body, string(payload))
				} else {
					require.NotContains(t, string(payload), "rs_old")
					require.NotContains(t, string(payload), "fc_old")
					require.NotContains(t, string(payload), "encrypted_content")
					require.Equal(t, "call_1", gjson.GetBytes(payload, "input.1.call_id").String())
					require.Equal(t, "call_1", gjson.GetBytes(payload, "input.2.call_id").String())
					require.EqualValues(t, len(payload), gotInfo.UpstreamRequestBodySize)
				}
				status, response := tc.status, tc.response
				if calls > 1 && !tc.repeat {
					status, response = 200, `{"id":"resp_ok","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`
					if tc.stream {
						response = "data: {\"type\":\"response.completed\",\"response\":" + response + "}\n\n"
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			}}
			usage, apiErr := doResponsesRequestWithReasoningRecovery(c, info, adaptor, bytes.NewReader([]byte(body)), []byte(body))
			require.Equal(t, tc.calls, calls)
			require.Equal(t, !tc.allowRetry, common.GetContextKeyBool(c, constant.ContextKeyResponsesRecoveryNoRetry))
			if tc.calls == 2 {
				require.Equal(t, []string{"R1"}, common.GetContextKeyStringSlice(c, constant.ContextKeyResponsesLogBadges))
			} else {
				require.Empty(t, common.GetContextKeyStringSlice(c, constant.ContextKeyResponsesLogBadges))
			}
			if tc.calls == 2 && !tc.repeat {
				require.Nil(t, apiErr)
				require.Equal(t, 12, usage.(*dto.Usage).TotalTokens)
			} else {
				require.NotNil(t, apiErr)
				require.Equal(t, !tc.allowRetry, types.IsSkipRetryError(apiErr))
			}
		})
	}
}

func TestResponsesReasoningRecoveryStopsOnSecondTransportFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{}}
	calls := 0
	a := &reasoningRecoveryAdaptor{request: func(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (any, error) {
		calls++
		if calls == 2 {
			return nil, fmt.Errorf("transport failed")
		}
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(encryptedErrorJSON))}, nil
	}}
	_, apiErr := doResponsesRequestWithReasoningRecovery(c, info, a, strings.NewReader(reasoningRecoveryBody), []byte(reasoningRecoveryBody))
	require.Equal(t, 2, calls)
	require.True(t, types.IsSkipRetryError(apiErr))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesRecoveryNoRetry))
	require.False(t, helper.ResponsesStreamStarted(c))
}
