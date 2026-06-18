package model

import (
	"fmt"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var e2eTopUpSeq int64

func setupUserLevelUpgradeE2E(t *testing.T, policyJSON string) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &Redemption{}, &RedemptionUsage{}, &UserSubscription{}, &UserSubscriptionDailyStat{}))
	if common.UsingSQLite {
		require.NoError(t, ensureSubscriptionPlanTableSQLite())
	} else {
		require.NoError(t, DB.AutoMigrate(&SubscriptionPlan{}))
	}

	originPolicies := setting.UserLevelPolicies2JSONString()
	require.NoError(t, setting.UpdateUserLevelPoliciesByJSONString(policyJSON))
	t.Cleanup(func() {
		_ = setting.UpdateUserLevelPoliciesByJSONString(originPolicies)
	})

	originQuotaPerUnit := common.QuotaPerUnit
	t.Cleanup(func() {
		common.QuotaPerUnit = originQuotaPerUnit
	})

	require.NoError(t, DB.Exec("DELETE FROM top_ups").Error)
	require.NoError(t, DB.Exec("DELETE FROM redemption_usages").Error)
	require.NoError(t, DB.Exec("DELETE FROM redemptions").Error)
	require.NoError(t, DB.Exec("DELETE FROM user_subscription_daily_stats").Error)
	require.NoError(t, DB.Exec("DELETE FROM user_subscriptions").Error)
	require.NoError(t, DB.Exec("DELETE FROM subscription_plans").Error)
	require.NoError(t, DB.Exec("DELETE FROM users").Error)
	require.NoError(t, DB.Exec("DELETE FROM logs").Error)
}

func createRegisteredUser(t *testing.T, name string) *User {
	t.Helper()
	user := &User{
		Username:    fmt.Sprintf("%s_%d", name, time.Now().UnixNano()),
		Password:    "Password123",
		DisplayName: name,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, user.Insert(0))
	return user
}

func createTopUp(t *testing.T, userId int, money float64) {
	t.Helper()
	now := common.GetTimestamp()
	seq := atomic.AddInt64(&e2eTopUpSeq, 1)
	topup := &TopUp{
		UserId:        userId,
		Amount:        int64(math.Round(money)),
		Money:         money,
		TradeNo:       fmt.Sprintf("topup-%d-%d", userId, seq),
		PaymentMethod: "test",
		CreateTime:    now,
		CompleteTime:  now,
		Status:        common.TopUpStatusSuccess,
	}
	require.NoError(t, topup.Insert())
}

func TestUserRegister_DefaultUserLevelIDIsOne(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[]`)

	user := createRegisteredUser(t, "register_default")
	reloaded, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 1, reloaded.UserLevelId)
}

func TestAutoUpgradeByRecharge_ExactlyThreshold(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[
{"id":1,"level":"Tier 1","recharge":0,"discount":"0","icon":"/t1.png","channel":[],"rate":50},
{"id":2,"level":"Tier 2","recharge":100,"discount":"0.1","icon":"/t2.png","channel":[],"rate":100},
{"id":3,"level":"Tier 3","recharge":500,"discount":"0.2","icon":"/t3.png","channel":[],"rate":300}
]`)

	user := createRegisteredUser(t, "exactly_threshold")
	createTopUp(t, user.Id, 40)
	createTopUp(t, user.Id, 60)

	require.NoError(t, TryAutoUpgradeUserLevelByRecharge(user.Id))

	reloaded, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "default", reloaded.Group)
	assert.Equal(t, 2, reloaded.UserLevelId)
}

func TestAutoUpgradeByRecharge_CrossLevelThreshold(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[
{"id":1,"level":"Tier 1","recharge":0,"discount":"0","icon":"/t1.png","channel":[],"rate":50},
{"id":2,"level":"Tier 2","recharge":100,"discount":"0.1","icon":"/t2.png","channel":[],"rate":100},
{"id":3,"level":"Tier 3","recharge":500,"discount":"0.2","icon":"/t3.png","channel":[],"rate":300}
]`)

	user := createRegisteredUser(t, "cross_level")
	createTopUp(t, user.Id, 600)

	require.NoError(t, TryAutoUpgradeUserLevelByRecharge(user.Id))

	reloaded, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "default", reloaded.Group)
	assert.Equal(t, 3, reloaded.UserLevelId)
}

func TestAutoUpgradeByRecharge_NoPolicyConfigured(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[]`)

	user := createRegisteredUser(t, "no_policy")
	createTopUp(t, user.Id, 9999)

	require.NoError(t, TryAutoUpgradeUserLevelByRecharge(user.Id))

	reloaded, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "default", reloaded.Group)
	assert.Equal(t, 1, reloaded.UserLevelId)
}

func TestAutoUpgradeByRecharge_RedemptionAlsoEffective(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[
{"id":1,"level":"Tier 1","recharge":0,"discount":"0","icon":"/t1.png","channel":[],"rate":50},
{"id":2,"level":"Tier 2","recharge":200,"discount":"0.1","icon":"/t2.png","channel":[],"rate":100}
]`)
	common.QuotaPerUnit = 100

	user := createRegisteredUser(t, "redeem_effective")
	now := common.GetTimestamp()
	redeemKey := fmt.Sprintf("R%031d", time.Now().UnixNano()%1_000_000_000_000_000_000)
	redemption := &Redemption{
		Key:         redeemKey,
		Status:      common.RedemptionCodeStatusEnabled,
		Name:        "e2e_redeem",
		Quota:       12000,
		PayMoney:    240,
		CreatedTime: now,
		ExpiredTime: 0,
	}
	require.NoError(t, redemption.Insert())

	redeemResult, err := Redeem(redeemKey, user.Id)
	require.NoError(t, err)
	require.NotNil(t, redeemResult)
	assert.Equal(t, common.RedemptionRewardTypeQuota, redeemResult.RewardType)
	assert.Equal(t, 12000, redeemResult.Quota)

	reloaded, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "default", reloaded.Group)
	assert.Equal(t, 2, reloaded.UserLevelId)

	var topup TopUp
	require.NoError(t, DB.Where("user_id = ? AND payment_method = ?", user.Id, "redemption").First(&topup).Error)
	assert.InDelta(t, 240.0, topup.Money, 0.0001)
	assert.Equal(t, common.TopUpStatusSuccess, topup.Status)
}

func TestAutoUpgradeByRecharge_SubscriptionRedemptionAlsoEffective(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[
{"id":1,"level":"Tier 1","recharge":0,"discount":"0","icon":"/t1.png","channel":[],"rate":50},
{"id":2,"level":"Tier 2","recharge":200,"discount":"0.1","icon":"/t2.png","channel":[],"rate":100}
]`)

	user := createRegisteredUser(t, "subscription_redeem_effective")
	plan := &SubscriptionPlan{
		Title:         "redeem plan",
		PriceAmount:   199,
		Currency:      "USD",
		DurationUnit:  "month",
		DurationValue: 1,
		Enabled:       true,
		TotalAmount:   1000,
	}
	require.NoError(t, DB.Create(plan).Error)

	now := common.GetTimestamp()
	redeemKey := fmt.Sprintf("S%031d", time.Now().UnixNano()%1_000_000_000_000_000_000)
	redemption := &Redemption{
		Key:         redeemKey,
		Status:      common.RedemptionCodeStatusEnabled,
		RewardType:  common.RedemptionRewardTypeSubscription,
		Name:        "e2e_subscription_redeem",
		PlanId:      plan.Id,
		PayMoney:    240,
		CreatedTime: now,
		ExpiredTime: 0,
	}
	require.NoError(t, redemption.Insert())

	redeemResult, err := Redeem(redeemKey, user.Id)
	require.NoError(t, err)
	require.NotNil(t, redeemResult)
	assert.Equal(t, common.RedemptionRewardTypeSubscription, redeemResult.RewardType)
	assert.Equal(t, plan.Id, redeemResult.PlanId)

	reloaded, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "default", reloaded.Group)
	assert.Equal(t, 2, reloaded.UserLevelId)

	var topup TopUp
	require.NoError(t, DB.Where("user_id = ? AND payment_method = ?", user.Id, "redemption").First(&topup).Error)
	assert.Equal(t, int64(0), topup.Amount)
	assert.InDelta(t, 240.0, topup.Money, 0.0001)
	assert.Equal(t, common.TopUpStatusSuccess, topup.Status)
}

func TestRedeem_WelfareCodeCanBeUsedOncePerUser(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[]`)
	common.QuotaPerUnit = 100

	userA := createRegisteredUser(t, "welfare_a")
	userB := createRegisteredUser(t, "welfare_b")
	now := common.GetTimestamp()
	redeemKey := fmt.Sprintf("W%031d", time.Now().UnixNano()%1_000_000_000_000_000_000)
	redemption := &Redemption{
		Key:         redeemKey,
		Status:      common.RedemptionCodeStatusEnabled,
		CodeType:    common.RedemptionCodeTypeWelfare,
		Name:        "welfare_redeem",
		Quota:       5000,
		PayMoney:    0,
		CreatedTime: now,
		ExpiredTime: 0,
	}
	require.NoError(t, redemption.Insert())

	firstResult, err := Redeem(redeemKey, userA.Id)
	require.NoError(t, err)
	require.NotNil(t, firstResult)
	assert.Equal(t, common.RedemptionRewardTypeQuota, firstResult.RewardType)
	assert.Equal(t, 5000, firstResult.Quota)

	secondResult, err := Redeem(redeemKey, userB.Id)
	require.NoError(t, err)
	require.NotNil(t, secondResult)
	assert.Equal(t, 5000, secondResult.Quota)

	duplicateResult, err := Redeem(redeemKey, userA.Id)
	require.Error(t, err)
	assert.Nil(t, duplicateResult)
	assert.Equal(t, "已兑换", err.Error())

	reloadedCode, err := GetRedemptionById(redemption.Id)
	require.NoError(t, err)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, reloadedCode.Status)
	assert.Equal(t, 0, reloadedCode.UsedUserId)

	var usageCount int64
	require.NoError(t, DB.Model(&RedemptionUsage{}).Where("redemption_id = ?", redemption.Id).Count(&usageCount).Error)
	assert.Equal(t, int64(2), usageCount)

	reloadedA, err := GetUserById(userA.Id, true)
	require.NoError(t, err)
	reloadedB, err := GetUserById(userB.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 5000, reloadedA.Quota)
	assert.Equal(t, 5000, reloadedB.Quota)
}

func TestRedeem_ExpiredCodeReturnsSpecificError(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[]`)

	user := createRegisteredUser(t, "expired_redeem")
	redeemKey := fmt.Sprintf("E%031d", time.Now().UnixNano()%1_000_000_000_000_000_000)
	redemption := &Redemption{
		Key:         redeemKey,
		Status:      common.RedemptionCodeStatusEnabled,
		CodeType:    common.RedemptionCodeTypeWelfare,
		Name:        "expired_redeem",
		Quota:       5000,
		CreatedTime: common.GetTimestamp() - 3600,
		ExpiredTime: common.GetTimestamp() - 1,
	}
	require.NoError(t, redemption.Insert())

	result, err := Redeem(redeemKey, user.Id)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, "兑换码已过期", err.Error())
}
