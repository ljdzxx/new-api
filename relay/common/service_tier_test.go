package common

import (
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestServiceTierMatchScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*RelayInfo)
		want   string
	}{
		{"chat", func(*RelayInfo) {}, "Ultrafast"},
		{"responses", func(i *RelayInfo) {
			i.RelayMode = relayconstant.RelayModeResponses
			i.RelayFormat = types.RelayFormatOpenAIResponses
		}, "Ultrafast"},
		{"compact", func(i *RelayInfo) {
			i.RelayMode = relayconstant.RelayModeResponsesCompact
			i.RelayFormat = types.RelayFormatOpenAIResponsesCompaction
		}, "Ultrafast"},
		{"chat converted to responses", func(i *RelayInfo) { i.RelayMode = relayconstant.RelayModeResponses }, "Ultrafast"},
		{"billing off", func(i *RelayInfo) { i.ChannelOtherSettings.ServiceTierBilling.Enabled = false }, ""},
		{"force off", func(i *RelayInfo) { i.ChannelOtherSettings.ServiceTierBilling.ForceServiceTier = false }, "Ultrafast"},
		{"unmatched", func(i *RelayInfo) { i.PriceData.ServiceTier = nil }, ""},
		{"no config", func(i *RelayInfo) { i.ChannelOtherSettings.ServiceTierBilling = nil }, ""},
		{"other channel", func(i *RelayInfo) { i.ChannelType = constant.ChannelTypeAzure }, ""},
		{"claude downstream", func(i *RelayInfo) { i.RelayFormat = types.RelayFormatClaude }, ""},
		{"gemini downstream", func(i *RelayInfo) { i.RelayFormat = types.RelayFormatGemini }, ""},
		{"images", func(i *RelayInfo) { i.RelayMode = relayconstant.RelayModeImagesGenerations }, ""},
		{"audio", func(i *RelayInfo) { i.RelayMode = relayconstant.RelayModeAudioSpeech }, ""},
		{"realtime", func(i *RelayInfo) { i.RelayMode = relayconstant.RelayModeRealtime }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &RelayInfo{
				RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions,
				ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ChannelOtherSettings: dto.ChannelOtherSettings{
					ServiceTierBilling: &types.ServiceTierBilling{Enabled: true, ForceServiceTier: true},
				}},
				PriceData: types.PriceData{ServiceTier: &types.ServiceTierBillingRule{Tier: "Ultrafast"}},
			}
			tc.change(info)
			require.Equal(t, tc.want, info.MatchedServiceTier())
		})
	}
	require.Empty(t, (*RelayInfo)(nil).MatchedServiceTier())
	require.Empty(t, (&RelayInfo{}).MatchedServiceTier())
}

func TestServiceTierJSONPreservesExtensions(t *testing.T) {
	for _, fields := range []string{
		``, `"service_tier":null,`, `"service_tier":"",`,
		`"service_tier":"default","service_tier":"priority",`,
		`"service_tier":"default","service_\u0074ier":"priority",`,
	} {
		body := []byte(`{` + fields + `"extension":{"service_tier":"nested","large":18446744073709551615,"precise":1e-25,"zero":0,"enabled":false}}`)
		original := string(body)
		patched, err := SetServiceTierJSON(body, "service_tier", "Ultrafast")
		require.NoError(t, err)
		require.Equal(t, original, string(body))
		require.Equal(t, "Ultrafast", gjson.GetBytes(patched, "service_tier").String())
		require.Equal(t, gjson.Get(original, "extension").Raw, gjson.GetBytes(patched, "extension").Raw)
		count := 0
		gjson.ParseBytes(patched).ForEach(func(key, value gjson.Result) bool {
			if key.String() == "service_tier" {
				count++
				require.Equal(t, "Ultrafast", value.String())
			}
			return true
		})
		require.Equal(t, 1, count)
	}
	for _, body := range []string{`{broken`, `[]`, `null`} {
		_, err := SetServiceTierJSON([]byte(body), "service_tier", "fast")
		require.Error(t, err)
	}
}

func TestServiceTierResponsePatch(t *testing.T) {
	for _, tc := range []struct {
		name       string
		format     types.RelayFormat
		body, path string
	}{
		{"chat missing", types.RelayFormatOpenAI, `{"choices":[],"usage":{"total_tokens":120}}`, "service_tier"},
		{"chat existing", types.RelayFormatOpenAI, `{"service_tier":"default","choices":[]}`, "service_tier"},
		{"responses", types.RelayFormatOpenAIResponses, `{"id":"resp_1","service_tier":null}`, "service_tier"},
		{"successful response with null error", types.RelayFormatOpenAIResponses, `{"id":"resp_1","error":null,"service_tier":null}`, "service_tier"},
		{"compact", types.RelayFormatOpenAIResponsesCompaction, `{"object":"response.compaction"}`, "service_tier"},
		{"created", types.RelayFormatOpenAIResponses, `{"type":"response.created","response":{"id":"resp_1"}}`, "response.service_tier"},
		{"completed", types.RelayFormatOpenAIResponses, `{"type":"response.completed","response":{"service_tier":"default"}}`, "response.service_tier"},
		{"failed", types.RelayFormatOpenAIResponses, `{"type":"response.failed","response":{"error":{"message":"failed"}}}`, "response.service_tier"},
		{"delta", types.RelayFormatOpenAIResponses, `{"type":"response.output_text.delta","delta":"service_tier"}`, ""},
		{"heartbeat", types.RelayFormatOpenAIResponses, `{"type":"response.keepalive"}`, ""},
		{"error", types.RelayFormatOpenAIResponses, `{"error":{"message":"failed"}}`, ""},
		{"chat error", types.RelayFormatOpenAI, `{"error":{"message":"failed"}}`, ""},
		{"other protocol", types.RelayFormatClaude, `{"type":"message"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &RelayInfo{RelayFormat: tc.format, ForcedServiceTier: "Ultrafast"}
			patched, err := PatchForcedServiceTierResponse([]byte(tc.body), info)
			require.NoError(t, err)
			if tc.path == "" {
				require.Equal(t, tc.body, string(patched))
			} else {
				require.Equal(t, "Ultrafast", gjson.GetBytes(patched, tc.path).String())
			}
			info.ForcedServiceTier = ""
			patched, err = PatchForcedServiceTierResponse([]byte(tc.body), info)
			require.NoError(t, err)
			require.Equal(t, tc.body, string(patched))
		})
	}
}

func TestServiceTierResetOnChannelRetry(t *testing.T) {
	info := &RelayInfo{ForcedServiceTier: "previous"}
	c, _ := gin.CreateTestContext(nil)
	basecommon.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	info.InitChannelMeta(c)
	require.Empty(t, info.ForcedServiceTier)
}
