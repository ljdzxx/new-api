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

func TestRelayRetryExhaustionPreservesUpstreamError(t *testing.T) {
	for _, status := range []int{503, 502, 429} {
		for _, intercept := range []bool{false, true} {
			t.Run(fmt.Sprintf("status=%d/intercept=%t", status, intercept), func(t *testing.T) {
				db := setupResponsesRelayBillingTest(t)
				require.NoError(t, db.AutoMigrate(&model.Ability{}))
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":0}`))
				oldRetries, oldMemory, oldLogs := common.RetryTimes, common.MemoryCacheEnabled, constant.ErrorLogEnabled
				common.RetryTimes, common.MemoryCacheEnabled, constant.ErrorLogEnabled = 3, false, false
				t.Cleanup(func() {
					common.RetryTimes, common.MemoryCacheEnabled, constant.ErrorLogEnabled = oldRetries, oldMemory, oldLogs
				})
				service.InitHttpClient()
				var calls atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					fmt.Fprint(w, `{"error":{"type":"server_error","code":"upstream_unavailable","message":"Service temporarily unavailable"}}`)
				}))
				defer upstream.Close()
				c, recorder, _ := newRelayMockContext(true)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"hello","stream":true}`))
				c.Request.Header.Set("Content-Type", "application/json")
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{
					ErrorInterceptEnabled: intercept,
					ErrorInterceptMessage: "upstream status={error_code}",
				})
				Relay(c, types.RelayFormatOpenAIResponses)
				require.EqualValues(t, 1, calls.Load())
				require.Equal(t, status, recorder.Code, recorder.Body.String())
				message := gjson.Get(recorder.Body.String(), "error.message").String()
				if intercept {
					require.Equal(t, fmt.Sprintf("upstream status=%d", status), message)
				} else {
					require.Contains(t, message, "Service temporarily unavailable")
					require.Equal(t, "upstream_unavailable", gjson.Get(recorder.Body.String(), "error.code").String())
				}
				require.NotContains(t, recorder.Body.String(), "get_channel_failed")
			})
		}
	}
}

func TestGetChannelNoCandidatesReturns503(t *testing.T) {
	db := setupResponsesRelayBillingTest(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}))
	oldMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
	c, _, info := newRelayMockContext(false)
	info.InitChannelMeta(c)
	param := &service.RetryParam{Ctx: c, TokenGroup: "default", ModelName: "gpt-4o-mini"}
	channel, err := getChannel(c, info, param)
	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.ErrorIs(t, err.Err, errRetryChannelsExhausted)
	require.True(t, types.IsSkipRetryError(err))
}

func TestGetChannelQueryFailureIsNotExhaustion(t *testing.T) {
	setupResponsesRelayBillingTest(t) // No abilities table: selection must report a query failure.
	oldMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
	c, _, info := newRelayMockContext(false)
	info.InitChannelMeta(c)
	param := &service.RetryParam{Ctx: c, TokenGroup: "default", ModelName: "gpt-4o-mini"}
	channel, err := getChannel(c, info, param)
	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusInternalServerError, err.StatusCode)
	require.NotErrorIs(t, err.Err, errRetryChannelsExhausted)
}
