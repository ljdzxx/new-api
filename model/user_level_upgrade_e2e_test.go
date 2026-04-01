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
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &Redemption{}))

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
	require.NoError(t, DB.Exec("DELETE FROM redemptions").Error)
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
{"id":2,"level":"Tier 2","recharge":100,"discount":"0.1","icon":"/t2.png","channel":[],"rate":100}
]`)
	common.QuotaPerUnit = 100

	user := createRegisteredUser(t, "redeem_effective")
	now := common.GetTimestamp()
	redeemKey := fmt.Sprintf("R%031d", time.Now().UnixNano()%1_000_000_000_000_000_000)
	redemption := &Redemption{
		Key:         redeemKey,
		Status:      common.RedemptionCodeStatusEnabled,
		Name:        "e2e_redeem",
		Quota:       12000, // 12000 / 100 = 120
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
	assert.InDelta(t, 120.0, topup.Money, 0.0001)
	assert.Equal(t, common.TopUpStatusSuccess, topup.Status)
}

func TestGetUserLevelGroupDailyConsumedMoney_WithRefund(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[
{"id":1,"level":"Tier 1","recharge":0,"discount":"0","icon":"/t1.png","channel":[],"rate":50,"group_day_limit":"100"}
]`)
	common.QuotaPerUnit = 100

	user := createRegisteredUser(t, "group_daily_limit")
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("group", "Tier 1").Error)

	now := common.GetTimestamp()
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    user.Id,
		Username:  user.Username,
		CreatedAt: now,
		Type:      LogTypeConsume,
		Quota:     15000, // 150
		Group:     "Tier 1",
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    user.Id,
		Username:  user.Username,
		CreatedAt: now,
		Type:      LogTypeRefund,
		Quota:     2000, // -20
		Group:     "Tier 1",
	}).Error)

	money, err := GetUserLevelGroupDailyConsumedMoney("Tier 1")
	require.NoError(t, err)
	assert.InDelta(t, 130.0, money, 0.0001)
}
