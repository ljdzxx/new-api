package controller

import (
	"context"
	"errors"
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
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResponsesRetryBoundary(t *testing.T) {
	monitor := operation_setting.GetMonitorSetting()
	original := monitor.GlobalQuotaInsufficientKeywords
	monitor.GlobalQuotaInsufficientKeywords = []string{"upstream unavailable"}
	t.Cleanup(func() { monitor.GlobalQuotaInsufficientKeywords = original })
	for _, eventType := range []string{"response.keepalive", "response.created", "response.in_progress", "response.output_text.delta", "response.output_item.added", "response.function_call_arguments.delta"} {
		t.Run(eventType, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			helper.SetEventStreamHeaders(c)
			require.NoError(t, helper.PingData(c))
			err := types.NewOpenAIError(errors.New("upstream unavailable"), types.ErrorCodeBadResponse, http.StatusBadGateway)
			require.True(t, shouldRetry(c, err, 1), "comment heartbeats must allow retry")
			force, finalErr := shouldForceRetryForGlobalQuotaInsufficient(c, 1, err)
			require.True(t, force, "configured quota failures can retry before real output")
			require.Nil(t, finalErr)
			require.NoError(t, helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: eventType}, fmt.Sprintf(`{"type":%q}`, eventType)))
			require.Equal(t, eventType == "response.keepalive", shouldRetry(c, err, 1))
			if eventType != "response.keepalive" {
				channelErr := types.NewError(errors.New("channel failed"), types.ErrorCodeChannelNoAvailableKey)
				require.False(t, shouldRetry(c, channelErr, 10), "channel-error shortcut must not bypass the boundary")
				force, finalErr := shouldForceRetryForGlobalQuotaInsufficient(c, 1, err)
				require.False(t, force)
				require.Nil(t, finalErr)
			}
		})
	}
}

func setupResponsesRelayBillingTest(t *testing.T) *gorm.DB {
	t.Helper()
	originalPolicy := billing_policy.GetConfig()
	originalRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(originalPolicy)))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalRatios))
	})
	require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(billing_policy.NewConfig())))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":1}`))
	// Initialize database-specific column quoting through the production entry
	// point, using an isolated in-memory DB before installing the test fixtures.
	t.Setenv("SQL_DSN", "local")
	oldPath, oldMaster := common.SQLitePath, common.IsMasterNode
	common.SQLitePath, common.IsMasterNode = ":memory:", false
	t.Cleanup(func() { common.SQLitePath, common.IsMasterNode = oldPath, oldMaster })
	require.NoError(t, model.InitDB())
	bootstrapDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, bootstrapDB.Close())
	db := setupRelayMockTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.UserSubscription{}))
	seedRelayMockBillingRows(t, db)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 42).Update("quota", 1000000).Error)
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 77).Update("remain_quota", 1000000).Error)
	return db
}

func TestResponsesRelayFailureKeepsSingleAttemptAndSettlesUsage(t *testing.T) {
	db := setupResponsesRelayBillingTest(t)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: "+`{"type":"response.created","sequence_number":0,"response":{"id":"resp_attempt_one"}}`+"\n\n")
		fmt.Fprint(w, "data: "+`{"type":"response.output_item.added","sequence_number":1,"item":{"id":"call_one","type":"function_call","name":"test_tool","call_id":"call_one","arguments":""}}`+"\n\n")
		fmt.Fprint(w, "data: "+`{"type":"response.failed","response":{"id":"resp_attempt_one","error":{"code":"server_error","message":"upstream failed"},"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}}`+"\n\n")
	}))
	defer upstream.Close()
	oldTimeout, oldRetries := common.RelayTimeout, common.RetryTimes
	common.RelayTimeout, common.RetryTimes = 5, 3
	service.InitHttpClient()
	t.Cleanup(func() { common.RelayTimeout, common.RetryTimes = oldTimeout, oldRetries; service.InitHttpClient() })
	c, recorder, _ := newRelayMockContext(true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"hello","stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
	helper.SetEventStreamHeaders(c)
	require.NoError(t, helper.PingData(c))
	Relay(c, types.RelayFormatOpenAIResponses)
	require.EqualValues(t, 1, calls.Load(), recorder.Body.String())
	require.Equal(t, 200, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"id":"resp_attempt_one"`)
	require.Contains(t, recorder.Body.String(), `"code":"invalid_prompt"`)
	require.Contains(t, recorder.Body.String(), "upstream failed")
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.failed\n"))
	require.NotContains(t, recorder.Body.String(), "[DONE]")
	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Equal(t, 100, logs[0].PromptTokens)
	require.Equal(t, 20, logs[0].CompletionTokens)
	require.Greater(t, logs[0].Quota, 0)
	var user model.User
	require.NoError(t, db.First(&user, 42).Error)
	require.Equal(t, 1000000-logs[0].Quota, user.Quota)
}

type responsesCancelWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
}

func (w *responsesCancelWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if strings.Contains(string(data), "response.reasoning_summary_text.delta") {
		w.cancel()
	}
	return n, err
}

func TestResponsesRelayClientDisconnectPreservesSettlementWithoutChannelError(t *testing.T) {
	for _, output := range []bool{false, true} {
		t.Run(fmt.Sprintf("output=%t", output), func(t *testing.T) {
			db := setupResponsesRelayBillingTest(t)
			oldErrorLogs := constant.ErrorLogEnabled
			constant.ErrorLogEnabled = true
			t.Cleanup(func() { constant.ErrorLogEnabled = oldErrorLogs })
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: "+`{"type":"response.created","response":{"id":"resp_cancel"}}`+"\n\n")
				if output {
					fmt.Fprint(w, "data: "+`{"type":"response.output_text.delta","delta":"Hello world"}`+"\n\n")
				}
				fmt.Fprint(w, "data: "+`{"type":"response.reasoning_summary_text.delta","delta":"thinking"}`+"\n\n")
			}))
			defer upstream.Close()
			oldTimeout, oldRetries := common.RelayTimeout, common.RetryTimes
			common.RelayTimeout, common.RetryTimes = 5, 3
			service.InitHttpClient()
			t.Cleanup(func() { common.RelayTimeout, common.RetryTimes = oldTimeout, oldRetries; service.InitHttpClient() })
			c, recorder, _ := newRelayMockContext(true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"hello","stream":true}`)).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Writer = &responsesCancelWriter{ResponseWriter: c.Writer, cancel: cancel}
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
			c.Set("status_code_mapping", `{"499":"502"}`)
			Relay(c, types.RelayFormatOpenAIResponses)
			require.Equal(t, context.Canceled, ctx.Err())
			require.EqualValues(t, 1, calls.Load())
			require.NotContains(t, recorder.Body.String(), "event: response.failed")
			require.NotContains(t, recorder.Body.String(), "invalid_prompt")
			var errorsCount int64
			require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeError).Count(&errorsCount).Error)
			require.Zero(t, errorsCount, "downstream cancellation must not create an upstream error log")
			var logs []model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			require.Len(t, logs, 1)
			if output {
				require.Positive(t, logs[0].CompletionTokens)
				require.Positive(t, logs[0].Quota)
			} else {
				require.Zero(t, logs[0].Quota)
			}
			var user model.User
			require.NoError(t, db.First(&user, 42).Error)
			require.Equal(t, 1000000-logs[0].Quota, user.Quota, "settlement must survive the deferred refund")
		})
	}
}
