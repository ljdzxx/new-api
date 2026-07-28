package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSubscriptionBalancePurchaseTest(t *testing.T) {
	t.Helper()
	setupUserLevelUpgradeE2E(t, `[]`)
	require.NoError(t, DB.AutoMigrate(&SubscriptionOrder{}))
	require.NoError(t, DB.Exec("DELETE FROM subscription_orders").Error)
}

func createBalancePurchasePlan(t *testing.T, allowBalancePay bool, price float64) *SubscriptionPlan {
	t.Helper()
	plan := &SubscriptionPlan{
		Title:                   "Balance Purchase Plan",
		PriceAmount:             price,
		Currency:                "USD",
		DurationUnit:            SubscriptionDurationMonth,
		DurationValue:           1,
		Enabled:                 true,
		AllowBalancePay:         common.GetPointer(allowBalancePay),
		MaxPurchasePerUser:      0,
		TotalAmount:             1000,
		QuotaResetPeriod:        SubscriptionResetNever,
		QuotaResetCustomSeconds: 0,
	}
	require.NoError(t, DB.Create(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)
	return plan
}

func TestPurchaseSubscriptionWithBalanceDeductsQuotaAndCreatesSubscription(t *testing.T) {
	setupSubscriptionBalancePurchaseTest(t)
	common.QuotaPerUnit = 100

	user := createRegisteredUser(t, "balance_purchase")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", 500).Error)
	plan := createBalancePurchasePlan(t, true, 1.23)

	require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))

	reloaded, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 377, reloaded.Quota)

	var sub UserSubscription
	require.NoError(t, DB.Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).First(&sub).Error)
	assert.Equal(t, PaymentMethodBalance, sub.Source)
	assert.Equal(t, int64(1000), sub.AmountTotal)

	var order SubscriptionOrder
	require.NoError(t, DB.Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).First(&order).Error)
	assert.Equal(t, sub.Id, order.UserSubscriptionId)
	assert.Equal(t, PaymentMethodBalance, order.PaymentMethod)
	assert.Equal(t, PaymentProviderBalance, order.PaymentProvider)
	assert.Equal(t, common.TopUpStatusSuccess, order.Status)
	assert.True(t, strings.Contains(order.ProviderPayload, "charged_quota=123"))

	var topup TopUp
	require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&topup).Error)
	assert.Equal(t, PaymentMethodBalance, topup.PaymentMethod)
	assert.Equal(t, PaymentProviderBalance, topup.PaymentProvider)
	assert.Equal(t, common.TopUpStatusSuccess, topup.Status)
}

func TestRefundSubscriptionInvalidatesEntitlementWithoutChangingGroupBalanceOrLevel(t *testing.T) {
	setupSubscriptionBalancePurchaseTest(t)
	common.QuotaPerUnit = 100

	user := createRegisteredUser(t, "subscription_refund")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"quota":         500,
		"user_level_id": 9,
	}).Error)
	plan := createBalancePurchasePlan(t, true, 1.23)
	plan.UpgradeGroup = "vip-refund"
	require.NoError(t, DB.Save(plan).Error)
	InvalidateSubscriptionPlanCache(plan.Id)

	require.NoError(t, PurchaseSubscriptionWithBalance(user.Id, plan.Id))

	var sub UserSubscription
	require.NoError(t, DB.Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).First(&sub).Error)
	assert.Equal(t, "active", sub.Status)
	assert.Equal(t, "vip-refund", sub.UpgradeGroup)

	var order SubscriptionOrder
	require.NoError(t, DB.Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).First(&order).Error)
	assert.Equal(t, sub.Id, order.UserSubscriptionId)

	var topup TopUp
	require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&topup).Error)
	require.NoError(t, fillTopUpOrderMetadata([]*TopUp{&topup}))
	assert.Equal(t, TopUpOrderTypeSubscription, topup.OrderType)
	assert.True(t, topup.Refundable)

	// Simulate an order created before the explicit entitlement link was added.
	require.NoError(t, DB.Model(&SubscriptionOrder{}).Where("id = ?", order.Id).
		Update("user_subscription_id", 0).Error)
	result, err := MarkTopUpRefunded(order.TradeNo, 123, "payment refunded")
	require.NoError(t, err)
	assert.Equal(t, TopUpOrderTypeSubscription, result.OrderType)
	assert.Equal(t, sub.Id, result.SubscriptionId)
	assert.False(t, result.LevelChanged)
	assert.Equal(t, 9, result.UserLevelId)

	require.NoError(t, DB.First(&order, order.Id).Error)
	assert.Equal(t, SubscriptionOrderStatusInvalidated, order.Status)
	assert.Equal(t, sub.Id, order.UserSubscriptionId)
	assert.Equal(t, 123, order.RefundOperatorId)
	assert.Equal(t, "payment refunded", order.RefundReason)
	assert.Greater(t, order.RefundTime, int64(0))

	require.NoError(t, DB.First(&sub, sub.Id).Error)
	assert.Equal(t, "cancelled", sub.Status)
	assert.LessOrEqual(t, sub.EndTime, common.GetTimestamp())

	require.NoError(t, DB.Where("trade_no = ?", order.TradeNo).First(&topup).Error)
	assert.Equal(t, common.TopUpStatusRefunded, topup.Status)

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, "vip-refund", reloaded.Group)
	assert.Equal(t, 377, reloaded.Quota)
	assert.Equal(t, 9, reloaded.UserLevelId)

	var stat UserSubscriptionDailyStat
	require.NoError(t, DB.Where("user_subscription_id = ?", sub.Id).Order("id desc").First(&stat).Error)
	assert.Equal(t, "cancelled", stat.SnapshotStatus)

	result, err = MarkTopUpRefunded(order.TradeNo, 123, "payment refunded")
	require.NoError(t, err)
	assert.True(t, result.AlreadyRefunded)
}

func TestRefundPaidSubscriptionRedemptionTerminatesEntitlement(t *testing.T) {
	for _, testCase := range []struct {
		name               string
		codeType           int
		expectedCodeStatus int
	}{
		{name: "normal", codeType: common.RedemptionCodeTypeNormal, expectedCodeStatus: common.RedemptionCodeStatusUsed},
		{name: "welfare", codeType: common.RedemptionCodeTypeWelfare, expectedCodeStatus: common.RedemptionCodeStatusEnabled},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			setupSubscriptionBalancePurchaseTest(t)

			user := createRegisteredUser(t, "redemption_refund_"+testCase.name)
			require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
				"quota":         500,
				"user_level_id": 9,
			}).Error)
			plan := createBalancePurchasePlan(t, true, 12.5)
			plan.UpgradeGroup = "vip-redemption-refund"
			require.NoError(t, DB.Save(plan).Error)
			InvalidateSubscriptionPlanCache(plan.Id)

			redemption := &Redemption{
				Key:         fmt.Sprintf("refund-sub-%020d", user.Id),
				Status:      common.RedemptionCodeStatusEnabled,
				CodeType:    testCase.codeType,
				RewardType:  common.RedemptionRewardTypeSubscription,
				Name:        "paid subscription redemption",
				PlanId:      plan.Id,
				PayMoney:    12.5,
				CreatedTime: common.GetTimestamp(),
			}
			require.NoError(t, DB.Create(redemption).Error)
			_, err := Redeem(redemption.Key, user.Id, 0)
			require.NoError(t, err)

			var sub UserSubscription
			require.NoError(t, DB.Where("user_id = ? AND redemption_id = ?", user.Id, redemption.Id).First(&sub).Error)
			assert.Equal(t, "active", sub.Status)
			var topup TopUp
			require.NoError(t, DB.Where("user_id = ? AND payment_method = ?", user.Id, PaymentMethodRedemption).First(&topup).Error)
			require.NoError(t, fillTopUpOrderMetadata([]*TopUp{&topup}))
			assert.Equal(t, TopUpOrderTypeSubscription, topup.OrderType)
			assert.Equal(t, PaymentMethodRedemption, topup.SubscriptionSource)
			assert.True(t, topup.Refundable)
			// Simulate historical entitlements created before redemption_id was linked.
			require.NoError(t, DB.Model(&UserSubscription{}).Where("id = ?", sub.Id).Update("redemption_id", 0).Error)

			result, err := MarkTopUpRefunded(topup.TradeNo, 123, "redemption payment refunded")
			require.NoError(t, err)
			assert.Equal(t, TopUpOrderTypeSubscription, result.OrderType)
			assert.Equal(t, redemption.Id, result.RedemptionId)
			assert.Equal(t, sub.Id, result.SubscriptionId)
			assert.False(t, result.LevelChanged)
			assert.Equal(t, 9, result.UserLevelId)

			require.NoError(t, DB.First(&topup, topup.Id).Error)
			assert.Equal(t, common.TopUpStatusRefunded, topup.Status)
			assert.Equal(t, 123, topup.RefundOperatorId)
			require.NoError(t, DB.First(&sub, sub.Id).Error)
			assert.Equal(t, "cancelled", sub.Status)
			assert.Equal(t, redemption.Id, sub.RedemptionId)
			require.NoError(t, DB.First(redemption, redemption.Id).Error)
			assert.Equal(t, testCase.expectedCodeStatus, redemption.Status)

			var reloaded User
			require.NoError(t, DB.First(&reloaded, user.Id).Error)
			assert.Equal(t, "vip-redemption-refund", reloaded.Group)
			assert.Equal(t, 500, reloaded.Quota)
			assert.Equal(t, 9, reloaded.UserLevelId)

			result, err = MarkTopUpRefunded(topup.TradeNo, 123, "redemption payment refunded")
			require.NoError(t, err)
			assert.True(t, result.AlreadyRefunded)
		})
	}
}

func TestPurchaseSubscriptionWithBalanceRejectsDisabledBalancePay(t *testing.T) {
	setupSubscriptionBalancePurchaseTest(t)
	common.QuotaPerUnit = 100

	user := createRegisteredUser(t, "balance_disabled")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", 500).Error)
	plan := createBalancePurchasePlan(t, false, 1)

	err := PurchaseSubscriptionWithBalance(user.Id, plan.Id)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不允许使用余额")

	reloaded, getErr := GetUserById(user.Id, true)
	require.NoError(t, getErr)
	assert.Equal(t, 500, reloaded.Quota)

	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ? AND plan_id = ?", user.Id, plan.Id).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}
