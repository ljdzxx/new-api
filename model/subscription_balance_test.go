package model

import (
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
