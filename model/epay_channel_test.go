package model

import (
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestEpayChannelOrderingAndStatus(test *testing.T) {
	require.NoError(test, DB.AutoMigrate(&EpayChannel{}, &EpayChannelRevision{}))
	originalChannels, err := ListEpayChannels(false)
	require.NoError(test, err)
	created := make([]*EpayChannel, 0, 3)
	test.Cleanup(func() {
		for _, channel := range created {
			DB.Where("channel_id = ?", channel.Id).Delete(&EpayChannelRevision{})
			DB.Delete(channel)
		}
		for _, channel := range originalChannels {
			DB.Model(channel).Update("sort_order", channel.SortOrder)
		}
	})
	for index := 0; index < 3; index++ {
		channel := &EpayChannel{Enabled: true}
		revision := &EpayChannelRevision{EpayChannelConfig: EpayChannelConfig{Name: fmt.Sprintf("sort channel %d", index)}}
		require.NoError(test, SaveEpayChannel(channel, revision, 0))
		created = append(created, channel)
		if index > 0 {
			require.Greater(test, channel.SortOrder, created[index-1].SortOrder)
		}
	}
	listed, err := ListEpayChannels(false)
	require.NoError(test, err)
	originalIDs := make([]int, 0, len(listed))
	for _, channel := range listed {
		originalIDs = append(originalIDs, channel.Id)
	}
	orderedIDs := append([]int(nil), originalIDs...)
	firstIndex, lastIndex := len(orderedIDs)-3, len(orderedIDs)-1
	orderedIDs[firstIndex], orderedIDs[lastIndex] = orderedIDs[lastIndex], orderedIDs[firstIndex]
	require.NoError(test, ReorderEpayChannels(orderedIDs, originalIDs))
	assertOrder := func(expected []int) {
		test.Helper()
		channels, err := ListEpayChannels(false)
		require.NoError(test, err)
		actual := make([]int, 0, len(channels))
		for _, channel := range channels {
			actual = append(actual, channel.Id)
		}
		require.Equal(test, expected, actual)
	}
	assertOrder(orderedIDs)
	invalidIDs := append([]int(nil), orderedIDs...)
	invalidIDs[0] = invalidIDs[1]
	require.Error(test, ReorderEpayChannels(invalidIDs, orderedIDs))
	invalidIDs[0] = -1
	require.Error(test, ReorderEpayChannels(invalidIDs, orderedIDs))
	require.Error(test, ReorderEpayChannels(orderedIDs[:len(orderedIDs)-1], orderedIDs))
	require.Error(test, ReorderEpayChannels(nil, nil))
	require.ErrorContains(test, ReorderEpayChannels(originalIDs, originalIDs), "已更新")
	assertOrder(orderedIDs)
	first := created[0]
	current, err := GetEpayChannel(first.Id)
	require.NoError(test, err)
	revisionID, sortOrder := current.CurrentRevisionId, current.SortOrder
	var beforeRevisions int64
	require.NoError(test, DB.Model(&EpayChannelRevision{}).Where("channel_id = ?", first.Id).Count(&beforeRevisions).Error)
	require.NoError(test, SetEpayChannelEnabled(first.Id, false, revisionID))
	current, err = GetEpayChannel(first.Id)
	require.NoError(test, err)
	require.False(test, current.Enabled)
	require.Equal(test, revisionID, current.CurrentRevisionId)
	require.Equal(test, sortOrder, current.SortOrder)
	enabled, err := ListEpayChannels(true)
	require.NoError(test, err)
	for _, channel := range enabled {
		require.NotEqual(test, first.Id, channel.Id)
	}
	require.NoError(test, SetEpayChannelEnabled(first.Id, true, revisionID))
	var afterRevisions int64
	require.NoError(test, DB.Model(&EpayChannelRevision{}).Where("channel_id = ?", first.Id).Count(&afterRevisions).Error)
	require.Equal(test, beforeRevisions, afterRevisions)
	updated := &EpayChannel{Id: first.Id, Enabled: true, SortOrder: -999}
	require.NoError(test, SaveEpayChannel(updated, &EpayChannelRevision{}, revisionID))
	require.Equal(test, sortOrder, updated.SortOrder)
	require.ErrorContains(test, SetEpayChannelEnabled(first.Id, false, revisionID), "已更新")
	require.Error(test, SetEpayChannelEnabled(0, false, revisionID))
	require.Error(test, SetEpayChannelEnabled(-1, false, revisionID))
	var maximumID int
	for _, channel := range listed {
		if channel.Id > maximumID {
			maximumID = channel.Id
		}
	}
	require.ErrorIs(test, SetEpayChannelEnabled(maximumID+1, false, revisionID), gorm.ErrRecordNotFound)
	assertOrder(orderedIDs)
}

func createEpaySettlementFixture(test *testing.T) (*User, *TopUp) {
	test.Helper()
	setupUserLevelUpgradeE2E(test, `[]`)
	common.QuotaPerUnit = 500000
	user := createRegisteredUser(test, "epay_settlement")
	require.NoError(test, DB.Model(user).Update("quota", 100).Error)
	topup := &TopUp{
		UserId: user.Id, Amount: 7140000, Money: 100, TradeNo: fmt.Sprintf("epay-settlement-%d", user.Id),
		PaymentMethod: "wxpay", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending,
		EpayChannelId: 1, EpayChannelRevisionId: 1, EpayChannelName: "original channel",
		EpayInputCNY: "100.00", EpayPayCNY: "100.00", EpayCreditUSD: "14.28", EpayBonusUSD: "0.28",
		BaseQuota: 7000000, BonusQuota: 140000, CreditQuota: 7140000,
	}
	require.NoError(test, topup.Insert())
	return user, topup
}

func TestCompleteEpayTopupConcurrentAndIdempotent(test *testing.T) {
	user, topup := createEpaySettlementFixture(test)
	common.QuotaPerUnit = 100000
	var callbacks sync.WaitGroup
	errors := make(chan error, 12)
	for callbackIndex := 0; callbackIndex < cap(errors); callbackIndex++ {
		callbacks.Add(1)
		go func() {
			defer callbacks.Done()
			errors <- CompleteEpayTopup(topup.TradeNo)
		}()
	}
	callbacks.Wait()
	close(errors)
	for err := range errors {
		require.NoError(test, err)
	}
	require.NoError(test, ManualCompleteTopUp(topup.TradeNo))
	var updatedUser User
	require.NoError(test, DB.First(&updatedUser, user.Id).Error)
	require.Equal(test, 7140100, updatedUser.Quota)
	var updatedTopup TopUp
	require.NoError(test, DB.First(&updatedTopup, topup.Id).Error)
	require.Equal(test, common.TopUpStatusSuccess, updatedTopup.Status)
	require.Positive(test, updatedTopup.CompleteTime)
	total, err := GetUserTotalRechargeAmount(user.Id)
	require.NoError(test, err)
	require.Equal(test, float64(100), total)
	var logCount int64
	require.NoError(test, DB.Model(&Log{}).Where("user_id = ? AND type = ?", user.Id, LogTypeTopup).Count(&logCount).Error)
	require.EqualValues(test, 1, logCount)
}

func TestCompleteEpayTopupOverflowRollsBackAndCanRetry(test *testing.T) {
	user, topup := createEpaySettlementFixture(test)
	require.NoError(test, DB.Model(user).Update("quota", common.MaxQuota-topup.CreditQuota+1).Error)
	require.ErrorContains(test, CompleteEpayTopup(topup.TradeNo), "安全上限")
	var updated TopUp
	require.NoError(test, DB.First(&updated, topup.Id).Error)
	require.Equal(test, common.TopUpStatusPending, updated.Status)
	require.Zero(test, updated.CompleteTime)
	var updatedUser User
	require.NoError(test, DB.First(&updatedUser, user.Id).Error)
	require.Equal(test, common.MaxQuota-topup.CreditQuota+1, updatedUser.Quota)
	require.NoError(test, DB.Model(user).Update("quota", 0).Error)
	require.NoError(test, CompleteEpayTopup(topup.TradeNo))
	require.NoError(test, DB.First(&updatedUser, user.Id).Error)
	require.Equal(test, topup.CreditQuota, updatedUser.Quota)
}

func TestCompleteEpayTopupRejectsInvalidSnapshots(test *testing.T) {
	user, original := createEpaySettlementFixture(test)
	for scenarioIndex, scenario := range []struct {
		name   string
		change func(*TopUp)
	}{
		{"legacy order", func(topup *TopUp) { topup.EpayChannelRevisionId = 0 }},
		{"wrong provider", func(topup *TopUp) { topup.PaymentProvider = PaymentProviderStripe }},
		{"negative bonus", func(topup *TopUp) { topup.BonusQuota = -1 }},
		{"negative base", func(topup *TopUp) { topup.BaseQuota = -1 }},
		{"inconsistent total", func(topup *TopUp) { topup.CreditQuota++ }},
		{"overflow", func(topup *TopUp) { topup.CreditQuota = common.MaxQuota }},
		{"expired", func(topup *TopUp) { topup.Status = common.TopUpStatusExpired }},
		{"refunded", func(topup *TopUp) { topup.Status = common.TopUpStatusRefunded }},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			topup := *original
			topup.Id = 0
			topup.TradeNo = fmt.Sprintf("epay-invalid-%d-%d", user.Id, scenarioIndex)
			scenario.change(&topup)
			require.NoError(test, topup.Insert())
			require.Error(test, CompleteEpayTopup(topup.TradeNo))
		})
	}
	var updated User
	require.NoError(test, DB.First(&updated, user.Id).Error)
	require.Equal(test, 100, updated.Quota)
	require.NoError(test, DB.Unscoped().Delete(user).Error)
	require.Error(test, CompleteEpayTopup(original.TradeNo))
	var pending TopUp
	require.NoError(test, DB.First(&pending, original.Id).Error)
	require.Equal(test, common.TopUpStatusPending, pending.Status)
}
