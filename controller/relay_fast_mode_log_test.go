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
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRelayLogsPreserveDownstreamFastMode(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		for _, tc := range []struct {
			name, tier  string
			allow, want bool
		}{
			{"fast allowed", "fast", true, true},
			{"priority allowed", "priority", true, true},
			{"fast filtered upstream", "fast", false, true},
			{"default", "default", true, false},
			{"absent", "", true, false},
		} {
			t.Run(fmt.Sprintf("%d/%s", status, tc.name), func(t *testing.T) {
				db := setupResponsesRelayBillingTest(t)
				settings := model_setting.GetGlobalSettings()
				oldPassthrough, oldErrorLogs := settings.PassThroughRequestEnabled, constant.ErrorLogEnabled
				settings.PassThroughRequestEnabled, constant.ErrorLogEnabled = false, true
				t.Cleanup(func() {
					settings.PassThroughRequestEnabled, constant.ErrorLogEnabled = oldPassthrough, oldErrorLogs
				})
				var upstreamBody []byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					upstreamBody, _ = io.ReadAll(r.Body)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					if status == http.StatusOK {
						fmt.Fprint(w, `{"id":"resp_fast","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
					} else {
						fmt.Fprint(w, `{"error":{"type":"server_error","message":"upstream failure"}}`)
					}
				}))
				defer server.Close()
				service.InitHttpClient()
				request := &dto.OpenAIResponsesRequest{Model: "gpt-4o-mini", Input: []byte(`"hello"`), ServiceTier: tc.tier}
				body, err := common.Marshal(request)
				require.NoError(t, err)
				c, _, _ := newRelayMockContext(false)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
				c.Request.Header.Set("Content-Type", "application/json")
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
				common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AllowServiceTier: tc.allow})
				info, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIResponses, request, nil)
				require.NoError(t, err)
				info.DisablePing = true
				apiErr := relay.ResponsesHelper(c, info)
				logType := model.LogTypeConsume
				if status != http.StatusOK {
					require.NotNil(t, apiErr)
					processChannelError(c, *types.NewChannelError(88, constant.ChannelTypeOpenAI, "mock-channel", false, "sk-upstream", false), apiErr)
					logType = model.LogTypeError
				} else {
					require.Nil(t, apiErr)
				}
				wantUpstreamTier := tc.tier
				if !tc.allow {
					wantUpstreamTier = ""
				}
				require.Equal(t, wantUpstreamTier, gjson.GetBytes(upstreamBody, "service_tier").String())
				var logs []model.Log
				require.NoError(t, db.Where("type = ?", logType).Find(&logs).Error)
				require.Len(t, logs, 1)
				require.Equal(t, tc.want, gjson.Get(logs[0].Other, "fast_mode").Exists())
				require.Equal(t, tc.want, gjson.Get(logs[0].Other, "fast_mode").Bool())
				userLogs, err := model.GetLogByTokenId(77)
				require.NoError(t, err)
				require.Len(t, userLogs, 1)
				require.Equal(t, tc.want, gjson.Get(userLogs[0].Other, "fast_mode").Bool())
			})
		}
	}
}
