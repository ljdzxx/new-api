package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelHealthRelayCountsFinalUpstreamOutcome(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status               int
		mapping, body, input string
		count, calls         int
	}{
		{"ordinary 429", 429, "", `{"error":{"code":"server_error","message":"busy"}}`, `{"role":"user","content":"hi"}`, 1, 1},
		{"mapped local 502", 400, `{"400":"502"}`, `{"error":{"code":"bad_request","message":"bad request"}}`, `{"role":"user","content":"hi"}`, 0, 1},
		{"mapped upstream 502", 502, `{"502":"400"}`, `{"error":{"code":"server_error","message":"busy"}}`, `{"role":"user","content":"hi"}`, 1, 1},
		{"reasoning recovery counts once", 502, "", `{"error":{"code":"server_error","message":"busy"}}`, `{"type":"reasoning","encrypted_content":"gAAAAAB_opaque_reasoning_1234567890"},{"role":"user","content":"hi"}`, 1, 2},
		{"ciphertext is not model failure", 400, "", `{"error":{"code":"invalid_encrypted_content","message":"rejected"}}`, `{"type":"compaction","encrypted_content":"gAAAAAB_opaque_compaction_1234567890"},{"role":"user","content":"hi"}`, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupResponsesRelayBillingTest(t)
			settings := operation_setting.GetMonitorSetting()
			oldSettings := *settings
			oldRetries, oldMemory := common.RetryTimes, common.MemoryCacheEnabled
			t.Cleanup(func() { *settings = oldSettings; common.RetryTimes = oldRetries; common.MemoryCacheEnabled = oldMemory })
			settings.ModelHealthEnabled = true
			settings.ModelHealthThreshold = 2
			settings.ModelHealthStatusCodes = "429,502,503"
			common.RetryTimes = 0
			common.MemoryCacheEnabled = false
			require.NoError(t, model.PruneChannelModelHealth(88, nil))
			if tc.name == "ciphertext is not model failure" {
				ch, err := model.GetChannelById(88, false)
				require.NoError(t, err)
				a, err := model.BeginModelHealthAttempt(ch, "gpt-4o-mini", false)
				require.NoError(t, err)
				require.NoError(t, a.Record(429))
			}
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer upstream.Close()
			service.InitHttpClient()
			c, _, _ := newRelayMockContext(false)
			c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":[`+tc.input+`]}`))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
			c.Set("status_code_mapping", tc.mapping)
			Relay(c, types.RelayFormatOpenAIResponses)
			require.Equal(t, tc.calls, calls)
			ch, err := model.GetChannelById(88, false)
			require.NoError(t, err)
			model.FillChannelModelHealth([]*model.Channel{ch})
			require.Equal(t, tc.count, ch.ModelHealth.Models["gpt-4o-mini"].Count)
		})
	}
}

func TestModelHealthAdminRecoveryAndSelectedChannelGate(t *testing.T) {
	db := setupResponsesRelayBillingTest(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelDailyMark{}))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":1,"gpt-4o-mini-openai-compact":1}`))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "health-admin", AffCode: "health-admin-code", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Quota: 1000000, Group: "default"}).Error)
	settings := operation_setting.GetMonitorSetting()
	oldSettings := *settings
	oldMemory := common.MemoryCacheEnabled
	t.Cleanup(func() { *settings = oldSettings; common.MemoryCacheEnabled = oldMemory })
	settings.ModelHealthEnabled = true
	settings.ModelHealthThreshold = 1
	settings.ModelHealthStatusCodes = "429,502,503"
	common.MemoryCacheEnabled = false
	require.NoError(t, model.PruneChannelModelHealth(88, nil))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/responses/compact" {
			fmt.Fprint(w, `{"id":"cmp_test","object":"response.compaction","output":[{"type":"compaction","encrypted_content":"opaque"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
			return
		}
		fmt.Fprint(w, `{"id":"test","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 88).Updates(map[string]interface{}{"base_url": upstream.URL, "models": "gpt-4o-mini,gpt-4o-mini-openai-compact,other"}).Error)
	ch, err := model.GetChannelById(88, true)
	require.NoError(t, err)
	for _, name := range ch.GetModels() {
		a, err := model.BeginModelHealthAttempt(ch, name, false)
		require.NoError(t, err)
		require.NoError(t, a.Record(503))
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	require.NotNil(t, middleware.SetupContextForSelectedChannel(c, ch, "gpt-4o-mini"))
	service.InitHttpClient()
	recorder := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/api/channel/test/88?model=gpt-4o-mini", nil)
	c.Params = gin.Params{{Key: "id", Value: "88"}}
	c.Set("id", 1)
	TestChannel(c)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	model.FillChannelModelHealth([]*model.Channel{ch})
	require.True(t, ch.ModelHealth.Models["gpt-4o-mini"].Available)
	require.False(t, ch.ModelHealth.Models["other"].Available)
	// Selecting the compact endpoint tests the compact routing model, not base.
	a, err := model.BeginModelHealthAttempt(ch, "gpt-4o-mini", false)
	require.NoError(t, err)
	require.NoError(t, a.Record(503))
	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/api/channel/test/88?model=gpt-4o-mini&endpoint_type=openai-response-compact", nil)
	c.Params = gin.Params{{Key: "id", Value: "88"}}
	c.Set("id", 1)
	TestChannel(c)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	model.FillChannelModelHealth([]*model.Channel{ch})
	require.False(t, ch.ModelHealth.Models["gpt-4o-mini"].Available)
	require.True(t, ch.ModelHealth.Models["gpt-4o-mini-openai-compact"].Available)
	require.False(t, ch.ModelHealth.Models["other"].Available)
}
