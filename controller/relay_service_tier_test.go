package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestServiceTierRelayBillingPreservesUpstreamRequest(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(fmt.Sprint(allow), func(t *testing.T) {
			var baseQuota int
			var baseBody string
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprint(enabled), func(t *testing.T) {
					db := setupResponsesRelayBillingTest(t)
					var upstreamBody string
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, _ := io.ReadAll(r.Body)
						upstreamBody = string(body)
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"id":"resp_tier","output":[],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":10,"cache_write_tokens":5}}}`)
					}))
					defer server.Close()
					service.InitHttpClient()
					c, recorder, _ := newRelayMockContext(false)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"hello","service_tier":"priority"}`))
					c.Request.Header.Set("Content-Type", "application/json")
					common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
					common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
					common.SetContextKey(c, constant.ContextKeyUserQuota, 1000000)
					common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AllowServiceTier: allow, ServiceTierBilling: &types.ServiceTierBilling{Enabled: enabled, Rules: []types.ServiceTierBillingRule{{Model: "*", Tier: "Ultrafast", TagColor: "purple", TagType: "solid", Multiplier: 6}}}})
					Relay(c, types.RelayFormatOpenAIResponses)
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					var logs []model.Log
					require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
					require.Len(t, logs, 1, recorder.Body.String())
					if !enabled {
						baseQuota, baseBody = logs[0].Quota, upstreamBody
						require.False(t, gjson.Get(logs[0].Other, "service_tier_billing").Exists())
					} else {
						require.JSONEq(t, baseBody, upstreamBody)
						require.InDelta(t, baseQuota*6, logs[0].Quota, 5) // Final integer truncation happens once.
						require.Equal(t, "Ultrafast", gjson.Get(logs[0].Other, "service_tier_billing.tier").String())
						require.Equal(t, "solid", gjson.Get(logs[0].Other, "service_tier_billing.tag_type").String())
					}
					require.Equal(t, allow, gjson.Get(upstreamBody, "service_tier").Exists())
					var user model.User
					require.NoError(t, db.First(&user, 42).Error)
					require.Equal(t, 1000000-logs[0].Quota, user.Quota)
					var token model.Token
					require.NoError(t, db.First(&token, 77).Error)
					require.Equal(t, user.Quota, token.RemainQuota)
					userLogs, err := model.GetLogByTokenId(77)
					require.NoError(t, err)
					require.Equal(t, enabled, gjson.Get(userLogs[0].Other, "service_tier_billing").Exists())
				})
			}
		})
	}
}

func TestServiceTierChannelValidation(t *testing.T) {
	settings := `{"service_tier_billing":{"enabled":true,"rules":[{"model":"*","tier":"fast","tag_color":"blue","tag_type":"light","multiplier":2}]},"allow_service_tier":true}`
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, OtherSettings: settings}
	require.NoError(t, validateChannel(channel, false))
	channel.OtherSettings = strings.Replace(settings, `"model":"*"`, `"model":"gpt-*"`, 1)
	require.Error(t, validateChannel(channel, false))
	channel.Type = constant.ChannelTypeAnthropic
	require.NoError(t, validateChannel(channel, false))
	require.False(t, gjson.Get(channel.OtherSettings, "service_tier_billing").Exists())
	require.True(t, gjson.Get(channel.OtherSettings, "allow_service_tier").Bool())
}
