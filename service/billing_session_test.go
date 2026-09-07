package service

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsSubscriptionUnsupportedGroupError(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(
		fmt.Errorf("订阅套餐不支持当前分组: 套餐不支持在 pro 下使用"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)

	require.True(t, isSubscriptionUnsupportedGroupError(apiErr))
	require.False(t, isSubscriptionUnsupportedGroupError(types.NewErrorWithStatusCode(
		fmt.Errorf("invalid model"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
	)))
}

func TestLocalQuotaErrorsUseBadRequestStatus(t *testing.T) {
	require.Equal(t, http.StatusBadRequest, types.NewErrorWithStatusCode(
		fmt.Errorf("用户额度不足, 剩余额度: ＄0.000000"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	).StatusCode)

	require.Equal(t, http.StatusBadRequest, types.NewErrorWithStatusCode(
		fmt.Errorf("token quota is not enough"),
		types.ErrorCodePreConsumeTokenQuotaFailed,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	).StatusCode)
}

func TestNewBillingSessionRejectsChannelWithoutPaymentMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	session, apiErr := NewBillingSession(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			AllowSubscription: false,
			AllowWallet:       false,
		},
	}, 1)

	require.Nil(t, session)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
}

func TestLogPreConsumePathIncludesSelectedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalWriter := gin.DefaultWriter
	t.Cleanup(func() { gin.DefaultWriter = originalWriter })
	var output bytes.Buffer
	gin.DefaultWriter = &output

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		UserId:          7,
		TokenId:         8,
		OriginModelName: "route-log-test",
		PreConsumePath:  preConsumePathMinimumBalance,
		BillingSource:   BillingSourceWallet,
		PriceData: types.PriceData{
			SystemGlobalModelRatio: 1,
			UserGlobalModelRatio:   1,
			ChannelModelRatio:      1,
			GlobalModelRatio:       1,
		},
	}

	logPreConsumePath(ctx, info, 123, 0)
	require.Contains(t, output.String(), "global model ratio token scaling pre-consume:")
	require.Contains(t, output.String(), "pre_consume_path=minimum_balance")
	require.Contains(t, output.String(), "billing_source=wallet")
}

func TestPreConsumeBillingFundingPreferenceMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	quotaSetting := operation_setting.GetQuotaSetting()
	originalQuotaSetting := *quotaSetting
	generalSetting := operation_setting.GetGeneralSetting()
	originalGeneralSetting := *generalSetting
	t.Cleanup(func() {
		*quotaSetting = originalQuotaSetting
		*generalSetting = originalGeneralSetting
	})

	generalSetting.QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	quotaSetting.EnablePreConsumeMinBalance = true
	quotaSetting.PreConsumeMinBalance = 1

	minimumBalanceQuota, err := quotaSetting.PreConsumeMinBalanceQuota()
	require.NoError(t, err)
	require.Positive(t, minimumBalanceQuota)

	const formulaQuota = 100_000
	trustQuota := common.GetTrustQuota()
	require.Greater(t, trustQuota, minimumBalanceQuota+formulaQuota)

	walletBelowMinimum := minimumBalanceQuota
	walletAboveMinimum := minimumBalanceQuota + formulaQuota
	walletAboveTrust := trustQuota + formulaQuota
	tokenQuota := trustQuota + formulaQuota*10

	type subscriptionState string
	const (
		subscriptionNone         subscriptionState = "none"
		subscriptionEnough       subscriptionState = "enough"
		subscriptionInsufficient subscriptionState = "insufficient"
		subscriptionUnsupported  subscriptionState = "unsupported"
	)

	tests := []struct {
		name                   string
		preference             string
		walletQuota            int
		subscription           subscriptionState
		expectedSource         string
		expectedPath           string
		expectedPreConsumed    int
		expectedSubscription   int64
		expectedPreConsumeRows int64
	}{
		{
			name:                 "wallet only ignores an available subscription",
			preference:           "wallet_only",
			walletQuota:          walletAboveMinimum,
			subscription:         subscriptionEnough,
			expectedSource:       BillingSourceWallet,
			expectedPath:         preConsumePathMinimumBalance,
			expectedPreConsumed:  0,
			expectedSubscription: 0,
		},
		{
			name:                   "subscription only ignores wallet minimum balance",
			preference:             "subscription_only",
			walletQuota:            walletBelowMinimum,
			subscription:           subscriptionEnough,
			expectedSource:         BillingSourceSubscription,
			expectedPath:           preConsumePathFormula,
			expectedPreConsumed:    formulaQuota,
			expectedSubscription:   formulaQuota,
			expectedPreConsumeRows: 1,
		},
		{
			name:                 "wallet first selects wallet minimum balance path",
			preference:           "wallet_first",
			walletQuota:          walletAboveMinimum,
			subscription:         subscriptionEnough,
			expectedSource:       BillingSourceWallet,
			expectedPath:         preConsumePathMinimumBalance,
			expectedPreConsumed:  0,
			expectedSubscription: 0,
		},
		{
			name:                   "wallet first falls back to subscription below minimum balance",
			preference:             "wallet_first",
			walletQuota:            walletBelowMinimum,
			subscription:           subscriptionEnough,
			expectedSource:         BillingSourceSubscription,
			expectedPath:           preConsumePathFormula,
			expectedPreConsumed:    formulaQuota,
			expectedSubscription:   formulaQuota,
			expectedPreConsumeRows: 1,
		},
		{
			name:                   "subscription first selects subscription formula path",
			preference:             "subscription_first",
			walletQuota:            walletAboveMinimum,
			subscription:           subscriptionEnough,
			expectedSource:         BillingSourceSubscription,
			expectedPath:           preConsumePathFormula,
			expectedPreConsumed:    formulaQuota,
			expectedSubscription:   formulaQuota,
			expectedPreConsumeRows: 1,
		},
		{
			name:                "subscription first falls back to wallet without active subscription",
			preference:          "subscription_first",
			walletQuota:         walletAboveMinimum,
			subscription:        subscriptionNone,
			expectedSource:      BillingSourceWallet,
			expectedPath:        preConsumePathMinimumBalance,
			expectedPreConsumed: 0,
		},
		{
			name:                 "subscription first falls back to wallet when subscription is insufficient",
			preference:           "subscription_first",
			walletQuota:          walletAboveMinimum,
			subscription:         subscriptionInsufficient,
			expectedSource:       BillingSourceWallet,
			expectedPath:         preConsumePathMinimumBalance,
			expectedPreConsumed:  0,
			expectedSubscription: 0,
		},
		{
			name:                 "subscription first falls back when subscription group is unsupported",
			preference:           "subscription_first",
			walletQuota:          walletAboveMinimum,
			subscription:         subscriptionUnsupported,
			expectedSource:       BillingSourceWallet,
			expectedPath:         preConsumePathMinimumBalance,
			expectedPreConsumed:  0,
			expectedSubscription: 0,
		},
		{
			name:                 "wallet trust quota overrides minimum balance path marker",
			preference:           "wallet_first",
			walletQuota:          walletAboveTrust,
			subscription:         subscriptionEnough,
			expectedSource:       BillingSourceWallet,
			expectedPath:         preConsumePathTrustQuota,
			expectedPreConsumed:  0,
			expectedSubscription: 0,
		},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			truncate(t)

			userID := 10_000 + index
			tokenID := 20_000 + index
			planID := 30_000 + index
			subscriptionID := 40_000 + index
			tokenKey := fmt.Sprintf("billing-matrix-token-%d", index)
			requestID := fmt.Sprintf("billing-matrix-request-%d", index)

			user := &model.User{
				Id:       userID,
				Username: fmt.Sprintf("billing_user_%d", index),
				Quota:    tt.walletQuota,
				Status:   common.UserStatusEnabled,
			}
			require.NoError(t, model.DB.Create(user).Error)

			if tt.subscription != subscriptionNone {
				allowedGroups := ""
				if tt.subscription == subscriptionUnsupported {
					allowedGroups = "vip"
				}
				plan := &model.SubscriptionPlan{
					Id:               planID,
					Title:            fmt.Sprintf("billing plan %d", index),
					Enabled:          true,
					DurationUnit:     model.SubscriptionDurationMonth,
					DurationValue:    1,
					AllowedGroups:    allowedGroups,
					TotalAmount:      int64(formulaQuota * 10),
					QuotaResetPeriod: model.SubscriptionResetNever,
				}
				require.NoError(t, model.DB.Create(plan).Error)

				subscriptionTotal := int64(formulaQuota * 10)
				if tt.subscription == subscriptionInsufficient {
					subscriptionTotal = int64(formulaQuota - 1)
				}
				subscription := &model.UserSubscription{
					Id:          subscriptionID,
					UserId:      userID,
					PlanId:      planID,
					AmountTotal: subscriptionTotal,
					Status:      "active",
					StartTime:   time.Now().Add(-time.Hour).Unix(),
					EndTime:     time.Now().Add(24 * time.Hour).Unix(),
				}
				require.NoError(t, model.DB.Create(subscription).Error)
			}

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			ctx.Set("token_quota", tokenQuota)

			info := &relaycommon.RelayInfo{
				TokenId:         tokenID,
				TokenKey:        tokenKey,
				TokenGroup:      "default",
				UserId:          userID,
				UserGroup:       "default",
				OriginModelName: "billing-matrix-model",
				RequestId:       requestID,
				// Keep this matrix focused on funding selection. Token eligibility for
				// the trust decision is supplied through the authenticated context.
				IsPlayground: true,
				UserSetting: dto.UserSetting{
					BillingPreference: tt.preference,
				},
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelId:         1,
					AllowSubscription: true,
					AllowWallet:       true,
				},
			}

			apiErr := PreConsumeBilling(ctx, formulaQuota, info)
			require.Nil(t, apiErr)
			require.NotNil(t, info.Billing)
			require.Equal(t, tt.expectedSource, info.BillingSource)
			require.Equal(t, tt.expectedPath, info.PreConsumePath)
			require.Equal(t, tt.expectedPreConsumed, info.Billing.GetPreConsumedQuota())
			require.Equal(t, tt.expectedPreConsumed, info.FinalPreConsumedQuota)

			var storedUser model.User
			require.NoError(t, model.DB.Select("quota").First(&storedUser, userID).Error)
			require.Equal(t, tt.walletQuota, storedUser.Quota)

			if tt.subscription != subscriptionNone {
				var storedSubscription model.UserSubscription
				require.NoError(t, model.DB.Select("amount_used").First(&storedSubscription, subscriptionID).Error)
				require.Equal(t, tt.expectedSubscription, storedSubscription.AmountUsed)
			}

			var preConsumeRows int64
			require.NoError(t, model.DB.Model(&model.SubscriptionPreConsumeRecord{}).Count(&preConsumeRows).Error)
			require.Equal(t, tt.expectedPreConsumeRows, preConsumeRows)
		})
	}
}
