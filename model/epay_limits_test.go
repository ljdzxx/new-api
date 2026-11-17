package model

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestEpayLocalDayBounds(test *testing.T) {
	original := time.Local
	test.Cleanup(func() { time.Local = original })
	for _, scenario := range []struct {
		zone  string
		date  string
		hours int64
	}{
		{"Asia/Shanghai", "2026-11-17", 24},
		{"America/New_York", "2026-03-08", 23},
		{"America/New_York", "2026-11-01", 25},
	} {
		location, err := time.LoadLocation(scenario.zone)
		require.NoError(test, err)
		time.Local = location
		midday, err := time.ParseInLocation("2006-01-02 15:04", scenario.date+" 12:30", location)
		require.NoError(test, err)
		start, end := EpayLocalDayBounds(midday.UTC())
		require.Equal(test, scenario.date+" 00:00", time.Unix(start, 0).In(location).Format("2006-01-02 15:04"))
		require.Equal(test, "00:00", time.Unix(end, 0).In(location).Format("15:04"))
		require.Equal(test, scenario.hours*3600, end-start)
		nextStart, _ := EpayLocalDayBounds(time.Unix(end, 0))
		require.Equal(test, end, nextStart)
	}
}

func createEpayLimitsFixture(test *testing.T) (*EpayChannel, *EpayChannelRevision) {
	test.Helper()
	require.NoError(test, DB.AutoMigrate(&TopUp{}, &SubscriptionOrder{}, &EpayChannel{}, &EpayChannelRevision{}))
	limitsJSON, err := common.Marshal(map[string]types.EpayMethodLimits{
		"wxpay":  {MinPayCNY: "1", MaxPayCNY: "100", DailyLimitCNY: "100"},
		"alipay": {MinPayCNY: "10", MaxPayCNY: "200", DailyLimitCNY: "200"},
		"qqpay":  {MinPayCNY: "1", MaxPayCNY: "1000", DailyLimitCNY: "0"},
	})
	require.NoError(test, err)
	channel := &EpayChannel{Enabled: true}
	revision := &EpayChannelRevision{EpayChannelConfig: EpayChannelConfig{
		Name: test.Name(), PaymentMethods: `["wxpay","alipay","qqpay"]`, PaymentMethodLimits: string(limitsJSON),
	}}
	require.NoError(test, SaveEpayChannel(channel, revision, 0))
	test.Cleanup(func() {
		DB.Where("epay_channel_id = ?", channel.Id).Delete(&TopUp{})
		DB.Where("epay_channel_id = ?", channel.Id).Delete(&SubscriptionOrder{})
		DB.Where("channel_id = ?", channel.Id).Delete(&EpayChannelRevision{})
		DB.Delete(channel)
	})
	return channel, revision
}

func epayLimitTopup(channel *EpayChannel, revision *EpayChannelRevision, method, amount, suffix string) *TopUp {
	return &TopUp{TradeNo: fmt.Sprintf("limits-%d-%s", channel.Id, suffix), UserId: 1, Amount: 1,
		PaymentProvider: PaymentProviderEpay, PaymentMethod: method, Status: common.TopUpStatusPending,
		EpayChannelId: channel.Id, EpayChannelRevisionId: revision.Id, EpayPayCNY: amount,
	}
}

func TestEpayDailyLimitsShareWalletAndSubscriptions(test *testing.T) {
	channel, revision := createEpayLimitsFixture(test)
	topup := epayLimitTopup(channel, revision, "wxpay", "60.00", "wallet")
	require.NoError(test, CreateEpayTopupOrder(topup))
	require.EqualValues(test, 6000, topup.EpayPayCents)
	subscription := &SubscriptionOrder{
		TradeNo: fmt.Sprintf("limits-%d-subscription", channel.Id), UserId: 1, PlanId: 1,
		PaymentProvider: PaymentProviderEpay, PaymentMethod: "wxpay", Status: common.TopUpStatusPending,
		EpayChannelId: channel.Id, EpayChannelRevisionId: revision.Id, EpayPayCNY: "40.00",
	}
	require.NoError(test, CreateEpaySubscriptionOrder(subscription))
	used, _, err := GetEpayDailyUsed(channel.Id, "wxpay", time.Now())
	require.NoError(test, err)
	require.EqualValues(test, 10000, used)
	rejected := epayLimitTopup(channel, revision, "wxpay", "1.00", "rejected")
	require.ErrorContains(test, CreateEpayTopupOrder(rejected), "今日剩余额度不足")
	require.Zero(test, rejected.Id)
	require.NoError(test, CreateEpayTopupOrder(epayLimitTopup(channel, revision, "alipay", "200.00", "alipay")))
	require.NoError(test, CreateEpayTopupOrder(epayLimitTopup(channel, revision, "qqpay", "500.00", "qq-first")))
	require.NoError(test, CreateEpayTopupOrder(epayLimitTopup(channel, revision, "qqpay", "500.00", "qq-second")))
	require.ErrorContains(test, CreateEpayTopupOrder(epayLimitTopup(channel, revision, "alipay", "9.99", "below-min")), "单笔限额")
	require.ErrorContains(test, CreateEpayTopupOrder(epayLimitTopup(channel, revision, "wxpay", "100.01", "above-max")), "单笔限额")
	require.ErrorContains(test, CreateEpayTopupOrder(epayLimitTopup(channel, revision, "stripe", "10.00", "unsupported")), "不支持")
	audit := TopUp{TradeNo: subscription.TradeNo, PaymentProvider: PaymentProviderEpay, PaymentMethod: "wxpay",
		Status: common.TopUpStatusSuccess, EpayChannelId: channel.Id, EpayPayCNY: "40.00", EpayPayCents: 4000, CreateTime: subscription.CreateTime,
	}
	require.NoError(test, DB.Create(&audit).Error)
	used, _, err = GetEpayDailyUsed(channel.Id, "wxpay", time.Now())
	require.NoError(test, err)
	require.EqualValues(test, 10000, used)
	require.NoError(test, DB.Model(topup).Updates(map[string]interface{}{"status": common.TopUpStatusSuccess, "complete_time": topup.CreateTime + 86400}).Error)
	require.NoError(test, DB.Model(subscription).Updates(map[string]interface{}{"status": common.TopUpStatusSuccess, "complete_time": subscription.CreateTime + 86400}).Error)
	used, _, err = GetEpayDailyUsed(channel.Id, "wxpay", time.Now())
	require.NoError(test, err)
	require.EqualValues(test, 10000, used)
	require.NoError(test, DB.Model(topup).Update("status", common.TopUpStatusRefunded).Error)
	require.NoError(test, DB.Model(subscription).Update("status", SubscriptionOrderStatusInvalidated).Error)
	used, _, err = GetEpayDailyUsed(channel.Id, "wxpay", time.Now())
	require.NoError(test, err)
	require.EqualValues(test, 10000, used)
	_, tomorrow := EpayLocalDayBounds(time.Now())
	used, _, err = GetEpayDailyUsed(channel.Id, "wxpay", time.Unix(tomorrow, 0))
	require.NoError(test, err)
	require.Zero(test, used)
}

func TestEpayDailyLimitsCountLocalBoundariesAndLegacyOrders(test *testing.T) {
	channel, _ := createEpayLimitsFixture(test)
	other, _ := createEpayLimitsFixture(test)
	now := time.Now()
	start, end := EpayLocalDayBounds(now)
	for index, order := range []TopUp{
		{CreateTime: start - 1, EpayPayCents: 99900, Status: common.TopUpStatusSuccess},
		{CreateTime: start, EpayPayCents: 1000, Status: common.TopUpStatusSuccess},
		{CreateTime: end - 1, EpayPayCents: 2000, Status: common.TopUpStatusPending},
		{CreateTime: end, EpayPayCents: 99900, Status: common.TopUpStatusSuccess},
		{CreateTime: start, EpayPayCNY: "5.05", Status: common.TopUpStatusSuccess},
		{CreateTime: start, Money: 6.06, Status: common.TopUpStatusRefunded},
		{CreateTime: start, EpayPayCents: 99900, Status: common.TopUpStatusExpired},
	} {
		order.TradeNo = fmt.Sprintf("limits-boundary-%d-%d", channel.Id, index)
		order.Amount, order.EpayChannelId, order.PaymentMethod, order.PaymentProvider = 1, channel.Id, "wxpay", PaymentProviderEpay
		require.NoError(test, DB.Create(&order).Error)
	}
	for _, ignored := range []TopUp{
		{EpayChannelId: other.Id, PaymentMethod: "wxpay", PaymentProvider: PaymentProviderEpay},
		{EpayChannelId: channel.Id, PaymentMethod: "alipay", PaymentProvider: PaymentProviderEpay},
		{EpayChannelId: channel.Id, PaymentMethod: "wxpay", PaymentProvider: PaymentProviderStripe},
	} {
		ignored.TradeNo = fmt.Sprintf("limits-ignore-%d-%s-%s", ignored.EpayChannelId, ignored.PaymentMethod, ignored.PaymentProvider)
		ignored.Amount, ignored.EpayPayCents, ignored.CreateTime, ignored.Status = 1, 99900, start, common.TopUpStatusSuccess
		require.NoError(test, DB.Create(&ignored).Error)
	}
	used, reset, err := GetEpayDailyUsed(channel.Id, "wxpay", now)
	require.NoError(test, err)
	require.EqualValues(test, 4111, used)
	require.Equal(test, end, reset)
}

func TestEpayDailyLimitReservationRollsBackAndSurvivesRevisionChange(test *testing.T) {
	channel, revision := createEpayLimitsFixture(test)
	first := epayLimitTopup(channel, revision, "wxpay", "40.00", "duplicate")
	require.NoError(test, CreateEpayTopupOrder(first))
	duplicate := epayLimitTopup(channel, revision, "wxpay", "40.00", "duplicate")
	require.Error(test, CreateEpayTopupOrder(duplicate))
	used, _, err := GetEpayDailyUsed(channel.Id, "wxpay", time.Now())
	require.NoError(test, err)
	require.EqualValues(test, 4000, used)
	updated := &EpayChannel{Id: channel.Id, Enabled: true}
	updatedRevision := &EpayChannelRevision{EpayChannelConfig: revision.EpayChannelConfig}
	require.NoError(test, SaveEpayChannel(updated, updatedRevision, revision.Id))
	require.ErrorContains(test, CreateEpayTopupOrder(epayLimitTopup(channel, revision, "wxpay", "1.00", "stale")), "配置已更新")
	require.NoError(test, CreateEpayTopupOrder(epayLimitTopup(channel, updatedRevision, "wxpay", "60.00", "updated")))
	require.ErrorContains(test, CreateEpayTopupOrder(epayLimitTopup(channel, updatedRevision, "wxpay", "1.00", "over-daily")), "今日剩余额度不足")
	require.NoError(test, SetEpayChannelEnabled(channel.Id, false, updatedRevision.Id))
	require.ErrorContains(test, CreateEpayTopupOrder(epayLimitTopup(channel, updatedRevision, "qqpay", "1.00", "disabled")), "停用")
}

func TestEpayDailyLimitConcurrentReservations(test *testing.T) {
	channel, revision := createEpayLimitsFixture(test)
	var wait sync.WaitGroup
	results := make(chan error, 20)
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			if index%2 == 0 {
				results <- CreateEpayTopupOrder(epayLimitTopup(channel, revision, "wxpay", "10.00", fmt.Sprintf("concurrent-%d", index)))
				return
			}
			results <- CreateEpaySubscriptionOrder(&SubscriptionOrder{
				TradeNo: fmt.Sprintf("limits-%d-concurrent-subscription-%d", channel.Id, index), UserId: 1, PlanId: 1,
				PaymentProvider: PaymentProviderEpay, PaymentMethod: "wxpay", Status: common.TopUpStatusPending,
				EpayChannelId: channel.Id, EpayChannelRevisionId: revision.Id, EpayPayCNY: "10.00",
			})
		}(index)
	}
	wait.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	require.Equal(test, 10, succeeded)
	used, _, err := GetEpayDailyUsed(channel.Id, "wxpay", time.Now())
	require.NoError(test, err)
	require.EqualValues(test, 10000, used)
}

func TestEpayDailyLimitConcurrentSQLiteConnections(test *testing.T) {
	originalDB := DB
	database, err := gorm.Open(sqlite.Open(filepath.Join(test.TempDir(), "epay-limits.db")+"?_pragma=busy_timeout(5000)"), &gorm.Config{})
	require.NoError(test, err)
	sqlDatabase, err := database.DB()
	require.NoError(test, err)
	sqlDatabase.SetMaxOpenConns(8)
	sqlDatabase.SetMaxIdleConns(8)
	DB = database
	test.Cleanup(func() {
		DB = originalDB
		require.NoError(test, sqlDatabase.Close())
	})
	channel, revision := createEpayLimitsFixture(test)
	var wait sync.WaitGroup
	results := make(chan error, 20)
	start := make(chan struct{})
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results <- CreateEpayTopupOrder(epayLimitTopup(channel, revision, "wxpay", "10.00", fmt.Sprintf("parallel-%d", index)))
		}(index)
	}
	close(start)
	wait.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else {
			require.Regexp(test, "locked|今日剩余", err.Error())
		}
	}
	require.Positive(test, succeeded)
	require.LessOrEqual(test, succeeded, 10)
	used, _, err := GetEpayDailyUsed(channel.Id, "wxpay", time.Now())
	require.NoError(test, err)
	require.EqualValues(test, succeeded*1000, used)
}

func TestEpayMethodLimitsAndAmountsRejectInvalidValues(test *testing.T) {
	for _, value := range []string{"", "-1", "NaN", "+Inf", "1e2", "0.001", "100000001", "18446744073686646784"} {
		_, err := EpayAmountCents(value)
		require.Error(test, err, value)
	}
	for _, limits := range []types.EpayMethodLimits{
		{MinPayCNY: "0", MaxPayCNY: "100", DailyLimitCNY: "0"},
		{MinPayCNY: "1", MaxPayCNY: "0.99", DailyLimitCNY: "0"},
		{MinPayCNY: "1", MaxPayCNY: "100", DailyLimitCNY: "-1"},
		{MinPayCNY: "1", MaxPayCNY: "100", DailyLimitCNY: "NaN"},
	} {
		_, _, _, err := ValidateEpayMethodLimits(limits)
		require.Error(test, err)
	}
}
