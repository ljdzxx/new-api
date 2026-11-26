package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForcedServiceTierOutboundAttempts(t *testing.T) {
	service.InitHttpClient()
	t.Cleanup(func() { service.GetHttpClient().CloseIdleConnections() })
	captured := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer upstream.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions,
		RequestURLPath: "/v1/chat/completions",
		ChannelMeta:    &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ChannelBaseUrl: upstream.URL},
	}
	adaptor := &Adaptor{}
	adaptor.Init(info)
	for _, tc := range []struct {
		name, tier                string
		enabled, force, websocket bool
		want                      string
	}{
		{"first channel", "Ultrafast", true, true, false, "Ultrafast"},
		{"retry channel", "flex", true, true, false, "flex"},
		{"unmatched retry", "", true, true, false, ""},
		{"billing disabled", "fast", false, true, false, ""},
		{"force disabled", "fast", true, false, false, "fast"},
		{"unmatched retry force disabled", "", true, false, false, ""},
		{"matched retry force disabled", "flex", true, false, false, "flex"},
		{"websocket excluded", "fast", true, true, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info.ChannelId++
			info.ChannelOtherSettings.ServiceTierBilling = &types.ServiceTierBilling{Enabled: tc.enabled, ForceServiceTier: tc.force}
			info.PriceData.ServiceTier = nil
			if tc.tier != "" {
				info.PriceData.ServiceTier = &types.ServiceTierBillingRule{Tier: tc.tier}
			}
			c.Set("responses_websocket_request", tc.websocket)
			body := `{"model":"test","service_tier":"priority","extension":{"large":18446744073709551615,"enabled":false}}`
			info.UpstreamRequestBodySize = int64(len(body))
			resp, err := adaptor.DoRequest(c, info, strings.NewReader(body))
			require.NoError(t, err)
			require.NoError(t, resp.(*http.Response).Body.Close())
			got := <-captured
			require.Equal(t, tc.want, info.ForcedServiceTier)
			if tc.want == "" || !tc.force {
				require.Equal(t, body, string(got))
			} else {
				require.Equal(t, tc.want, gjson.GetBytes(got, "service_tier").String())
			}
			if tc.want != "" {
				info.PriceData.ServiceTier.Tier = "changed after send"
				info.ChannelOtherSettings.ServiceTierBilling.Enabled = false
				clientBody, err := relaycommon.PatchForcedServiceTierResponse([]byte(`{"service_tier":"default"}`), info)
				require.NoError(t, err)
				require.Equal(t, tc.want, gjson.GetBytes(clientBody, "service_tier").String())
			} else {
				clientBody, err := relaycommon.PatchForcedServiceTierResponse([]byte(`{"service_tier":"default"}`), info)
				require.NoError(t, err)
				require.JSONEq(t, `{"service_tier":"default"}`, string(clientBody))
			}
			require.Equal(t, gjson.Get(body, "extension").Raw, gjson.GetBytes(got, "extension").Raw)
		})
	}
}

func TestForcedServiceTierSyntheticUsage(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, ForcedServiceTier: "Ultrafast", ShouldIncludeUsage: true}
	usage := &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}
	HandleFinalResponse(c, info, "", "chat_1", 1, "test", "", usage, false)
	lines := strings.Split(recorder.Body.String(), "\n")
	require.Equal(t, "Ultrafast", gjson.Get(strings.TrimPrefix(lines[0], "data: "), "service_tier").String())
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
	require.Equal(t, 120, usage.TotalTokens)
}
