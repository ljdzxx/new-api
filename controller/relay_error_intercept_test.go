package controller

import (
	"fmt"
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
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesRelayErrorInterceptPreservesTemplate(t *testing.T) {
	const replayMessage = "The response could not be completed and this request must not be replayed automatically. "
	const upstreamMessage = replayMessage + replayMessage + "upstream failed (request id: upstream-request)"
	for _, stream := range []bool{false, true} {
		for _, testCase := range []struct {
			name     string
			enabled  bool
			template string
			expected string
		}{
			{"template variables", true, "REQUEST ID:{request_id} - ERROR_CODE:{error_code} - RESPONSE_CODE:{response_code}, 请联系客服处理。", "REQUEST ID:local-request - ERROR_CODE:502 - RESPONSE_CODE:502, 请联系客服处理。"},
			{"plain template", true, "请联系客服处理。", "请联系客服处理。"},
			{"template includes replay message", true, replayMessage + "请联系客服处理。", replayMessage + "请联系客服处理。"},
			{"disabled", false, "请联系客服处理。", ""},
			{"empty template", true, "  ", ""},
		} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, testCase.name), func(t *testing.T) {
				db := setupResponsesRelayBillingTest(t)
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":0}`))
				oldRetries, oldLogs := common.RetryTimes, constant.ErrorLogEnabled
				common.RetryTimes, constant.ErrorLogEnabled = 0, true
				service.InitHttpClient()
				t.Cleanup(func() { common.RetryTimes, constant.ErrorLogEnabled = oldRetries, oldLogs })
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					calls.Add(1)
					upstreamError := types.OpenAIError{Type: "server_error", Code: "server_error", Message: upstreamMessage}
					if stream {
						writer.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(writer, "data: "+`{"type":"response.created","sequence_number":0,"response":{"id":"resp_intercept"}}`+"\n\n")
						data, marshalErr := common.Marshal(map[string]any{
							"type":     "response.failed",
							"response": map[string]any{"id": "resp_intercept", "error": upstreamError},
						})
						if marshalErr != nil {
							t.Error(marshalErr)
							return
						}
						fmt.Fprintf(writer, "data: %s\n\n", data)
						return
					}
					writer.Header().Set("Content-Type", "application/json")
					writer.WriteHeader(http.StatusBadGateway)
					data, marshalErr := common.Marshal(map[string]any{"error": upstreamError})
					if marshalErr != nil {
						t.Error(marshalErr)
						return
					}
					_, _ = writer.Write(data)
				}))
				defer upstream.Close()
				ctx, recorder, _ := newRelayMockContext(stream)
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"gpt-4o-mini","input":"hello","stream":%t}`, stream)))
				ctx.Request.Header.Set("Content-Type", "application/json")
				ctx.Set(common.RequestIdKey, "local-request")
				common.SetContextKey(ctx, constant.ContextKeyChannelSetting, dto.ChannelSettings{
					ErrorInterceptEnabled: testCase.enabled,
					ErrorInterceptMessage: testCase.template,
				})
				common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
				Relay(ctx, types.RelayFormatOpenAIResponses)
				require.EqualValues(t, 1, calls.Load(), recorder.Body.String())
				var message string
				if stream {
					require.Equal(t, http.StatusOK, recorder.Code)
					require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed\n"))
					parts := strings.SplitN(recorder.Body.String(), "event: response.failed\ndata: ", 2)
					require.Len(t, parts, 2)
					event := strings.TrimSpace(parts[1])
					require.True(t, gjson.Valid(event))
					require.Equal(t, "server_error", gjson.Get(event, "response.error.code").String())
					require.Equal(t, "server_error", gjson.Get(event, "response.error.type").String())
					require.Empty(t, recorder.Header().Get("x-should-retry"))
					message = gjson.Get(event, "response.error.message").String()
				} else {
					require.Equal(t, http.StatusBadGateway, recorder.Code)
					message = gjson.Get(recorder.Body.String(), "error.message").String()
				}
				if testCase.expected != "" {
					require.Equal(t, testCase.expected, message)
				} else {
					expected := common.MessageWithRequestId(upstreamMessage, "local-request")
					require.Equal(t, expected, message)
				}
				var errorLogs []model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeError).Find(&errorLogs).Error)
				require.Len(t, errorLogs, 1)
				require.Contains(t, errorLogs[0].Content, upstreamMessage)
			})
		}
	}
}
