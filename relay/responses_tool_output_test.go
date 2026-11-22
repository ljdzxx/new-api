package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesHTTPCodexContextNormalizedOnWire(t *testing.T) {
	for _, tc := range []struct {
		name        string
		passThrough bool
	}{{"passthrough", true}, {"converted", false}} {
		t.Run(tc.name, func(t *testing.T) {
			service.InitHttpClient()
			t.Cleanup(func() { service.GetHttpClient().CloseIdleConnections() })
			captured := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "read error", 500)
					return
				}
				captured <- data
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, `{"error":{"message":"stop","type":"invalid_request_error"}}`)
			}))
			defer upstream.Close()

			body := `{"model":"test-model","stream":true,"input":[` +
				`{"type":"function_call_output","namespace":"agent","name":"browser_info","output":"tab"},` +
				`{"type":"function_call_output","namespace":"agent","name":"goal_objective","output":"ship it"},` +
				`{"role":"user","content":"hi"}]}`
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(ctx)
			common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(ctx, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.passThrough})
			req, err := helper.GetAndValidateResponsesRequest(ctx)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{Request: req, OriginModelName: req.Model, RelayMode: relayconstant.RelayModeResponses, RequestURLPath: "/v1/responses", RelayFormat: types.RelayFormatOpenAIResponses, IsStream: true, DisablePing: true}
			apiErr := ResponsesHelper(ctx, info)
			require.NotNil(t, apiErr)
			require.Len(t, captured, 1)
			got := <-captured
			require.Equal(t, "message", gjson.GetBytes(got, "input.0.type").String())
			require.Equal(t, "<external_browser_info>tab</external_browser_info>", gjson.GetBytes(got, "input.0.content.0.text").String())
			require.Equal(t, "<external_goal_objective>ship it</external_goal_objective>", gjson.GetBytes(got, "input.1.content.0.text").String())
			require.Equal(t, "hi", gjson.GetBytes(got, "input.2.content").String())
		})
	}
}

func TestResponsesHTTPToolOutputOnWire(t *testing.T) {
	service.InitHttpClient()
	t.Cleanup(func() { service.GetHttpClient().CloseIdleConnections() })
	captured := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", 500)
			return
		}
		captured <- data
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"message":"function_call_output requires call_id on HTTP requests","type":"invalid_request_error"}}`)
	}))
	defer upstream.Close()
	for _, tc := range []struct {
		name, outputID string
		missing        int
	}{{"paired", `"call_id":"call_second",`, 0}, {"missing", "", 1}, {"blank", `"call_id":" ",`, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"model":"test-model","stream":true,"input":[{"type":"function_call","id":"nonstandard","call_id":"call_first","name":"run","arguments":"{}"},{"type":"function_call","call_id":"call_second","name":"run","arguments":"{}"},{"type":"function_call_output",` + tc.outputID + `"output":"second"},{"type":"function_call_output","call_id":"call_first","output":"first"}]}`
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(ctx)
			common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(ctx, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: true})
			req, err := helper.GetAndValidateResponsesRequest(ctx)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{Request: req, OriginModelName: req.Model, RelayMode: relayconstant.RelayModeResponses, RequestURLPath: "/v1/responses", RelayFormat: types.RelayFormatOpenAIResponses, IsStream: true, DisablePing: true}
			apiErr := ResponsesHelper(ctx, info)
			require.NotNil(t, apiErr)
			require.Contains(t, apiErr.Error(), "function_call_output requires call_id")
			require.Len(t, captured, 1)
			got := <-captured
			require.False(t, gjson.GetBytes(got, "input.0.id").Exists())
			require.Equal(t, "call_first", gjson.GetBytes(got, "input.0.call_id").String())
			require.Equal(t, "call_second", gjson.GetBytes(got, "input.1.call_id").String())
			require.Equal(t, gjson.Get(body, "input.2").Raw, gjson.GetBytes(got, "input.2").Raw)
			require.Equal(t, gjson.Get(body, "input.3").Raw, gjson.GetBytes(got, "input.3").Raw)
			shape := toolOutputShape(gjson.GetBytes(got, "input"), false)
			require.Equal(t, 2, shape.Calls)
			require.Equal(t, 2, shape.Outputs)
			require.Equal(t, tc.missing, shape.MissingCallID)
			if tc.missing > 0 {
				require.Equal(t, 2, shape.FirstMissingIndex)
			} else {
				require.Equal(t, -1, shape.FirstMissingIndex)
			}
		})
	}
}
