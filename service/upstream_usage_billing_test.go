package service

import (
	"context"
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
	"github.com/tidwall/gjson"
)

func setupUpstreamUsageBillingDB(t *testing.T) {
	t.Helper()
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldPath, oldMaster := common.SQLitePath, common.IsMasterNode
	t.Setenv("SQL_DSN", "local")
	common.SQLitePath, common.IsMasterNode = ":memory:", false
	require.NoError(t, model.InitDB())
	testDB, err := model.DB.DB()
	require.NoError(t, err)
	testDB.SetMaxOpenConns(1)
	model.LOG_DB = model.DB
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SQLitePath, common.IsMasterNode = oldPath, oldMaster
		require.NoError(t, testDB.Close())
	})
	require.NoError(t, model.DB.AutoMigrate(
		&model.Task{}, &model.User{}, &model.Token{}, &model.Log{}, &model.Channel{},
		&model.SubscriptionPlan{}, &model.UserSubscription{},
		&model.SubscriptionPreConsumeRecord{}, &model.UserSubscriptionDailyStat{},
	))
}

func TestUpstreamUsageSettlementAndRefund(t *testing.T) {
	setupUpstreamUsageBillingDB(t)
	for _, funding := range []string{"wallet", "subscription", "trust"} {
		for _, tc := range []struct {
			name     string
			reported *dto.Usage
			audio    bool
			want     int
		}{
			{name: "missing despite local estimate"},
			{name: "missing audio usage", audio: true},
			{name: "explicit zero", reported: &dto.Usage{}},
			{name: "explicit audio zero", reported: &dto.Usage{}, audio: true},
			{name: "partial input only", reported: &dto.Usage{PromptTokens: 100, TotalTokens: 100}, want: 100},
			{name: "complete", reported: &dto.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120}, want: 120},
			{name: "audio aliases and details", reported: &dto.Usage{InputTokens: 100, OutputTokens: 20, InputTokensDetails: &dto.InputTokenDetails{AudioTokens: 100}, CompletionTokenDetails: dto.OutputTokenDetails{AudioTokens: 20}}, audio: true, want: 120},
		} {
			t.Run(funding+"/"+tc.name, func(t *testing.T) {
				truncate(t)
				config := billing_policy.GetConfig()
				require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(billing_policy.NewConfig())))
				t.Cleanup(func() { require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(config))) })
				qs := operation_setting.GetQuotaSetting()
				old := qs.EnablePreConsumeMinBalance
				qs.EnablePreConsumeMinBalance = false
				t.Cleanup(func() { qs.EnablePreConsumeMinBalance = old })
				balance := common.GetTrustQuota() + 10000
				seedUser(t, 9901, balance)
				seedToken(t, 9902, 9901, "usage-billing-test", balance)
				require.NoError(t, model.DB.Create(&model.Channel{Id: 9903, Name: "usage billing"}).Error)
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
				ctx.Set("token_quota", balance)
				info := &relaycommon.RelayInfo{
					UserId: 9901, UserQuota: balance, TokenId: 9902, TokenKey: "usage-billing-test",
					ForcePreConsume: funding != "trust", OriginModelName: "usage-billing-test", StartTime: time.Now(),
					TokenGroup: "default", UsingGroup: "default", RequestId: t.Name(),
					UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
					ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 9903, AllowWallet: true, AllowSubscription: true},
					PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 1, AudioRatio: 1, AudioCompletionRatio: 1, GlobalModelRatio: 1,
						GlobalRatioConfigSnapshot: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
				}
				info.SetEstimatePromptTokens(200653)
				if funding == "subscription" {
					info.UserSetting.BillingPreference = "subscription_only"
					require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9904, Title: "usage billing", Enabled: true, DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 10000, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
					require.NoError(t, model.DB.Create(&model.UserSubscription{Id: 9905, UserId: 9901, PlanId: 9904, AmountTotal: 10000, Status: "active", StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix()}).Error)
				}
				require.Nil(t, PreConsumeBilling(ctx, 1000, info))
				if funding == "trust" {
					require.Zero(t, info.Billing.GetPreConsumedQuota())
				} else {
					require.Equal(t, 1000, info.Billing.GetPreConsumedQuota())
				}
				info.RecordUpstreamUsage(tc.reported)
				estimated := &dto.Usage{PromptTokens: 200653, CompletionTokens: 500, TotalTokens: 201153}
				if tc.audio {
					PostAudioConsumeQuota(ctx, info, estimated, "")
				} else {
					PostTextConsumeQuota(ctx, info, estimated, nil)
				}
				// The controller's later refund must not undo a completed settlement.
				info.Billing.Refund(ctx)
				var user model.User
				var token model.Token
				var channel model.Channel
				require.NoError(t, model.DB.First(&user, 9901).Error)
				require.NoError(t, model.DB.First(&token, 9902).Error)
				require.NoError(t, model.DB.First(&channel, 9903).Error)
				if funding == "subscription" {
					var sub model.UserSubscription
					require.NoError(t, model.DB.First(&sub, 9905).Error)
					require.EqualValues(t, tc.want, sub.AmountUsed)
					require.Equal(t, balance, user.Quota)
				} else {
					require.Equal(t, balance-tc.want, user.Quota)
				}
				require.Equal(t, balance-tc.want, token.RemainQuota)
				require.Equal(t, tc.want, user.UsedQuota)
				require.Equal(t, tc.want, token.UsedQuota)
				require.EqualValues(t, tc.want, channel.UsedQuota)
				var logs []model.Log
				require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				require.Len(t, logs, 1)
				require.Equal(t, tc.want, logs[0].Quota)
				require.Equal(t, tc.reported != nil, gjson.Get(logs[0].Other, "admin_info.upstream_usage_reported").Bool())
				if tc.reported == nil {
					require.Zero(t, logs[0].PromptTokens)
					require.Equal(t, "missing_upstream_usage", gjson.Get(logs[0].Other, "billing_skipped_reason").String())
				}
			})
		}
	}
}

func TestTaskUpstreamUsageMissingRefundsTokenPricingOnly(t *testing.T) {
	setupUpstreamUsageBillingDB(t)
	for _, subscription := range []bool{false, true} {
		for _, tokenBilling := range []bool{false, true} {
			t.Run(fmt.Sprintf("subscription=%t/token=%t", subscription, tokenBilling), func(t *testing.T) {
				truncate(t)
				seedUser(t, 9911, 10000)
				seedToken(t, 9912, 9911, "task-usage-test", 10000)
				seedChannel(t, 9913)
				source := BillingSourceWallet
				subscriptionID := 0
				if subscription {
					source, subscriptionID = BillingSourceSubscription, 9914
					require.NoError(t, model.DB.Create(&model.SubscriptionPlan{Id: 9915, Title: "task usage billing", Enabled: true, DurationUnit: model.SubscriptionDurationMonth, DurationValue: 1, TotalAmount: 10000, QuotaResetPeriod: model.SubscriptionResetNever}).Error)
					require.NoError(t, model.DB.Create(&model.UserSubscription{Id: subscriptionID, UserId: 9911, PlanId: 9915, AmountTotal: 10000, AmountUsed: 1000, Status: "active", StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix()}).Error)
				}
				task := makeTask(9911, 9913, 1000, 9912, source, subscriptionID)
				task.PrivateData.BillingContext.TokenBilling = &tokenBilling
				settleTaskBillingOnComplete(context.Background(), &mockAdaptor{}, task, &relaycommon.TaskInfo{Status: model.TaskStatusSuccess})
				if tokenBilling {
					require.Zero(t, task.Quota)
					require.Equal(t, 11000, getTokenRemainQuota(t, 9912))
					if subscription {
						var sub model.UserSubscription
						require.NoError(t, model.DB.First(&sub, subscriptionID).Error)
						require.Zero(t, sub.AmountUsed)
						require.Equal(t, 10000, getUserQuota(t, 9911))
					} else {
						require.Equal(t, 11000, getUserQuota(t, 9911))
					}
				} else {
					require.Equal(t, 1000, task.Quota)
					require.Equal(t, 10000, getTokenRemainQuota(t, 9912))
					require.Equal(t, 10000, getUserQuota(t, 9911))
				}
			})
		}
	}
}

func TestTaskUpstreamUsageBillingModeSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name    string
		context model.TaskBillingContext
		want    bool
	}{
		{name: "frozen token mode", context: model.TaskBillingContext{TokenBilling: common.GetPointer(true)}, want: true},
		{name: "frozen fixed mode overrides ratio", context: model.TaskBillingContext{TokenBilling: common.GetPointer(false), ModelRatio: 1}},
		{name: "per call override", context: model.TaskBillingContext{PerCallBilling: true, TokenBilling: common.GetPointer(true)}},
		{name: "frozen per token policy", context: model.TaskBillingContext{BillingPolicy: `{"mode":"per_token"}`}, want: true},
		{name: "frozen tiered policy", context: model.TaskBillingContext{BillingPolicy: `{"mode":"tiered"}`}, want: true},
		{name: "frozen per request policy", context: model.TaskBillingContext{BillingPolicy: `{"mode":"per_request"}`, ModelRatio: 1}},
		{name: "historical ratio snapshot", context: model.TaskBillingContext{ModelRatio: 1}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Exercise the JSON round trip used when polling persisted tasks,
			// including an explicit false pricing-mode snapshot.
			encoded, err := common.Marshal(tc.context)
			require.NoError(t, err)
			var restored model.TaskBillingContext
			require.NoError(t, common.Unmarshal(encoded, &restored))
			task := &model.Task{PrivateData: model.TaskPrivateData{BillingContext: &restored}}
			require.Equal(t, tc.want, TaskUsesTokenBilling(task))
		})
	}
}

func TestUpstreamUsageMissingSkipsPoliciesAndTools(t *testing.T) {
	for _, mode := range []string{"per_token", "tiered"} {
		t.Run(mode, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			policy := fullTextBillingPolicy()
			if mode == "tiered" {
				policy.Mode = "tiered"
				policy.Tiers = []billing_policy.Tier{{ID: "all", Fallback: true, Prices: policy.Prices}}
			}
			installActiveBillingPolicyForTest(t, "missing-usage", policy)
			info := &relaycommon.RelayInfo{OriginModelName: "missing-usage", FinalPreConsumedQuota: 1000, StartTime: time.Now()}
			info.SetEstimatePromptTokens(200653)
			ctx.Set("claude_web_search_requests", 1)
			summary := calculateTextQuotaSummary(ctx, info, usageForSettlement(info, &dto.Usage{PromptTokens: 200653}))
			require.Zero(t, summary.Quota, fmt.Sprint(summary))
			require.True(t, summary.MissingUpstreamUsage)
		})
	}
}
