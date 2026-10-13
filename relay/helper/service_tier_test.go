package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestServiceTierPreConsumeAndChannelChanges(t *testing.T) {
	original := billing_policy.GetConfig()
	t.Cleanup(func() { require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(original))) })
	require.NoError(t, billing_policy.UpdateFromJSON(`{"schema_version":1,"revision":1,"state":"active","policies":{"tier-test":{"version":1,"mode":"per_token","currency":"USD","unit":"per_million_tokens","prices":{"input":"2","output":"10"}}}}`))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	config := &types.ServiceTierBilling{Enabled: true, Rules: []types.ServiceTierBillingRule{
		{Model: "*", Tier: "fast", TagColor: "blue", TagType: "light", Multiplier: 2},
		{Model: "tier-test", Tier: "Ultrafast", TagColor: "green", TagType: "solid", Multiplier: 6},
	}}
	info := &relaycommon.RelayInfo{OriginModelName: "tier-test", UsingGroup: "default", UserGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "mapped-model", ChannelModelRatio: 1,
		ChannelOtherSettings: dto.ChannelOtherSettings{ServiceTierBilling: config},
	}}
	meta := &types.TokenCountMeta{MaxTokens: 100}
	config.Enabled = false
	base, err := ModelPriceHelper(c, info, 500, meta)
	require.NoError(t, err)
	config.Enabled = true
	price, err := ModelPriceHelper(c, info, 500, meta)
	require.NoError(t, err)
	require.Equal(t, base.QuotaToPreConsume*6, price.QuotaToPreConsume)
	require.Equal(t, "Ultrafast", price.ServiceTier.Tier)
	require.Equal(t, "tier-test", price.ServiceTier.Model)
	config.Rules[1].Multiplier = 2
	require.Equal(t, 6.0, price.ServiceTier.Multiplier)
	price, err = ModelPriceHelper(c, info, 500, meta)
	require.NoError(t, err)
	require.Equal(t, base.QuotaToPreConsume*2, price.QuotaToPreConsume)
	info.ChannelType = constant.ChannelTypeAnthropic
	config.Rules[1].Multiplier = -1 // Non-OpenAI ignores even stale invalid settings.
	price, err = ModelPriceHelper(c, info, 500, meta)
	require.NoError(t, err)
	require.Nil(t, price.ServiceTier)
	require.Equal(t, base.QuotaToPreConsume, price.QuotaToPreConsume)
	info.ChannelType = constant.ChannelTypeOpenAI
	_, err = ModelPriceHelper(c, info, 500, meta)
	require.Error(t, err)
	config.Rules[1].Multiplier = 1000
	_, err = ModelPriceHelper(c, info, 1_000_000_000, meta)
	require.Error(t, err, "oversized pre-consume must fail rather than wrap")
}

func TestServiceTierPerCallPreConsume(t *testing.T) {
	original := billing_policy.GetConfig()
	t.Cleanup(func() { require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(original))) })
	require.NoError(t, billing_policy.UpdateFromJSON(`{"schema_version":1,"revision":1,"state":"active","policies":{"tier-task":{"version":1,"mode":"per_request","currency":"USD","unit":"per_request","price":"1"}}}`))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{OriginModelName: "tier-task", UserGroup: "default", UsingGroup: "default", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ChannelModelRatio: 1}}
	base, err := ModelPriceHelperPerCall(c, info)
	require.NoError(t, err)
	info.ChannelOtherSettings.ServiceTierBilling = &types.ServiceTierBilling{Enabled: true, Rules: []types.ServiceTierBillingRule{{Model: "*", Tier: "fast", TagColor: "blue", TagType: "light", Multiplier: 2}}}
	scaled, err := ModelPriceHelperPerCall(c, info)
	require.NoError(t, err)
	require.Equal(t, base.Quota*2, scaled.Quota)
	require.Equal(t, base.ModelPrice, scaled.ModelPrice, "base model prices remain unchanged")
	require.Equal(t, "fast", scaled.ServiceTier.Tier)
}
