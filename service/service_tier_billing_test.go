package service

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestServiceTierSettlementModelOnly(t *testing.T) {
	for _, active := range []bool{false, true} {
		for _, perRequest := range []bool{false, true} {
			t.Run(fmt.Sprintf("active=%t/per-request=%t", active, perRequest), func(t *testing.T) {
				backup := billing_policy.GetConfig()
				t.Cleanup(func() { require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(backup))) })
				require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(billing_policy.NewConfig())))
				if active {
					policy := fullTextBillingPolicy()
					if perRequest {
						policy.Mode, policy.Unit, policy.Price = "per_request", "per_request", "0.01"
						policy.Prices = billing_policy.Prices{}
					}
					policy.Tools = map[string]billing_policy.ToolPrice{billing_policy.ToolFileSearch: {Unit: "per_thousand_calls", Price: "2"}}
					installActiveBillingPolicyForTest(t, "service-tier-test", policy)
				}
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				info := &relaycommon.RelayInfo{OriginModelName: "service-tier-test", ChannelMeta: &relaycommon.ChannelMeta{}, StartTime: time.Now(), PriceData: types.PriceData{
					ModelRatio: 1, CompletionRatio: 2, CacheRatio: 0.5, CacheCreationRatio: 2, ImageRatio: 1,
					GlobalModelRatio: 1, GlobalRatioConfigSnapshot: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
					UsePrice: perRequest, ModelPrice: 0.01,
				}}
				usage := &dto.Usage{PromptTokens: 1000, CompletionTokens: 500, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 100, CacheWriteTokens: 100, ImageTokens: 50}}
				base := calculateTextQuotaSummary(ctx, info, usage)
				info.PriceData.ServiceTier = &types.ServiceTierBillingRule{Model: "*", Tier: "Ultrafast", TagColor: "green", TagType: "solid", Multiplier: 6}
				scaled := calculateTextQuotaSummary(ctx, info, usage)
				require.InDelta(t, base.Quota*6, scaled.Quota, 5)
				require.Equal(t, base.PromptTokens, scaled.PromptTokens)
				require.Equal(t, base.CompletionTokens, scaled.CompletionTokens)
				require.Equal(t, base.CacheTokens, scaled.CacheTokens)
				info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{dto.BuildInToolFileSearch: {CallCount: 2}}}
				scaledWithTool := calculateTextQuotaSummary(ctx, info, usage)
				info.PriceData.ServiceTier = nil
				baseWithTool := calculateTextQuotaSummary(ctx, info, usage)
				require.Positive(t, baseWithTool.Quota-base.Quota)
				require.InDelta(t, baseWithTool.Quota-base.Quota, scaledWithTool.Quota-scaled.Quota, 1)
			})
		}
	}
}

func TestServiceTierAudioMultiplier(t *testing.T) {
	info := QuotaInfo{InputDetails: TokenDetails{TextTokens: 100, AudioTokens: 200}, OutputDetails: TokenDetails{TextTokens: 50, AudioTokens: 100}, ModelRatio: 1, GroupRatio: 1, GlobalRatio: 1, UseResolvedRatios: true, CompletionRatio: 2, AudioRatio: 3, AudioCompletionRatio: 4}
	base, clamp := calculateAudioQuota(info)
	require.Nil(t, clamp)
	info.ServiceTierMultiplier = 6
	scaled, clamp := calculateAudioQuota(info)
	require.Nil(t, clamp)
	require.Equal(t, base*6, scaled)
}

func TestServiceTierRetryReservationSettleAndRefund(t *testing.T) {
	for _, subscription := range []bool{false, true} {
		for _, refund := range []bool{false, true} {
			t.Run(fmt.Sprintf("subscription=%t/refund=%t", subscription, refund), func(t *testing.T) {
				truncate(t)
				quotaSetting := operation_setting.GetQuotaSetting()
				old := quotaSetting.EnablePreConsumeMinBalance
				quotaSetting.EnablePreConsumeMinBalance = false
				t.Cleanup(func() { quotaSetting.EnablePreConsumeMinBalance = old })
				seedUser(t, 9901, 10000)
				info := &relaycommon.RelayInfo{UserId: 9901, UserQuota: 10000, IsPlayground: true, ForcePreConsume: true, OriginModelName: "tier-test", TokenGroup: "default", UsingGroup: "default", RequestId: t.Name(), UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
				if subscription {
					info.UserSetting.BillingPreference = "subscription_only"
					require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9902, Title: "tier test", Enabled: true, DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 10000, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
					require.NoError(t, model.DB.Create(&model.UserSubscription{Id: 9903, UserId: 9901, PlanId: 9902, AmountTotal: 10000, Status: "active", StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix()}).Error)
				}
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				require.Nil(t, PreConsumeBilling(ctx, 1000, info))
				session := info.Billing.(*BillingSession)
				require.Nil(t, session.EnsurePreConsumedQuota(ctx, 6000))
				require.Equal(t, 6000, session.GetPreConsumedQuota())
				require.NotNil(t, session.EnsurePreConsumedQuota(ctx, 11000))
				require.Equal(t, 6000, session.GetPreConsumedQuota(), "failed retry leaves the existing reservation intact")
				require.Nil(t, session.EnsurePreConsumedQuota(ctx, 2000))
				expectedCharge := 4000
				if refund {
					expectedCharge = 0
					session.Refund(ctx)
					session.Refund(ctx)
				} else {
					require.NoError(t, session.Settle(expectedCharge))
					require.NoError(t, session.Settle(expectedCharge))
					session.Refund(ctx)
				}
				require.Eventually(t, func() bool {
					if subscription {
						var sub model.UserSubscription
						return model.DB.First(&sub, 9903).Error == nil && sub.AmountUsed == int64(expectedCharge)
					}
					var user model.User
					return model.DB.First(&user, 9901).Error == nil && user.Quota == 10000-expectedCharge
				}, time.Second*2, time.Millisecond*10)
			})
		}
	}
}
