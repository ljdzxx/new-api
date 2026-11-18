package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestEpayHoldingSharedAcrossChannelsAndScenes(t *testing.T) {
	useEpayFileDatabase(t)
	wallet, wr := createEpayLimitsFixture(t)
	subs, sr := createEpayLimitsFixture(t)
	const userID = 200007
	start := make(chan struct{})
	results := make(chan error, 12)
	for i := 0; i < cap(results); i++ {
		go func(i int) {
			<-start
			trade := fmt.Sprintf("holding-%d", i)
			form := fmt.Sprintf(`{"url":"https://example.test","fields":{"out_trade_no":%q}}`, trade)
			if i%2 == 0 {
				o := epayLimitTopup(wallet, wr, "wxpay", "10", trade)
				o.UserId, o.TradeNo, o.EpayCheckoutFormJSON = userID, trade, form
				results <- CreateEpayTopupOrder(o)
			} else {
				results <- CreateEpaySubscriptionOrder(&SubscriptionOrder{UserId: userID, TradeNo: trade, EpayCheckoutFormJSON: form, Status: common.TopUpStatusPending, PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay", EpayPayCNY: "10", EpayChannelId: subs.Id, EpayChannelRevisionId: sr.Id})
			}
		}(i)
	}
	close(start)
	success := 0
	for i := 0; i < cap(results); i++ {
		if err := <-results; err == nil {
			success++
		} else {
			require.ErrorIs(t, err, ErrEpayHolding)
		}
	}
	require.Equal(t, 1, success)
	order, err := CurrentEpayOrder(userID)
	require.NoError(t, err)
	require.NotNil(t, order)
	require.NotNil(t, order.Form)
	require.Equal(t, order.TradeNo, order.Form.Fields["out_trade_no"])
	require.Error(t, CancelEpayOrder(userID+1, order.TradeNo, order.Scene))
	require.NoError(t, CancelEpayOrder(userID, order.TradeNo, order.Scene))
	next := epayLimitTopup(wallet, wr, "wxpay", "10", "replacement")
	next.UserId = userID
	require.NoError(t, CreateEpayTopupOrder(next))
	require.Error(t, CancelEpayOrder(userID, order.TradeNo, order.Scene))
	current, err := CurrentEpayOrder(userID)
	require.NoError(t, err)
	require.Equal(t, next.TradeNo, current.TradeNo)
}

func TestEpayHoldingExpiryBoundary(t *testing.T) {
	channel, revision := createEpayLimitsFixture(t)
	now := time.Now().Unix()
	require.False(t, epayOrderExpired(now-epayHoldingSeconds+1, now))
	for _, scene := range []types.PaymentScene{types.PaymentSceneTopup, types.PaymentSceneSubscription} {
		wallet := epayLimitTopup(channel, revision, "wxpay", "10", string(scene))
		var table any = wallet
		if scene == types.PaymentSceneTopup {
			require.NoError(t, CreateEpayTopupOrder(wallet))
		} else {
			sub := &SubscriptionOrder{UserId: wallet.UserId, TradeNo: wallet.TradeNo, PaymentProvider: PaymentProviderEpay, PaymentMethod: "wxpay", Status: common.TopUpStatusPending, EpayChannelId: channel.Id, EpayChannelRevisionId: revision.Id, EpayPayCNY: "10"}
			require.NoError(t, CreateEpaySubscriptionOrder(sub))
			table = sub
		}
		require.NoError(t, DB.Model(table).Update("create_time", now-epayHoldingSeconds).Error)
		tracked, err := GetEpayOrderStatus(wallet.UserId, scene, wallet.TradeNo)
		require.NoError(t, err)
		require.Equal(t, common.TopUpStatusPending, tracked.Status)
		require.Nil(t, tracked.Form)
		if scene == types.PaymentSceneTopup {
			require.ErrorIs(t, ManualCompleteTopUp(wallet.TradeNo), ErrTopUpStatusInvalid)
		} else {
			require.ErrorIs(t, CompleteSubscriptionOrder(wallet.TradeNo, "", PaymentProviderEpay, "wxpay"), ErrSubscriptionOrderStatusInvalid)
		}
		require.Error(t, CancelEpayOrder(wallet.UserId, wallet.TradeNo, scene))
		pending, err := CurrentEpayOrder(wallet.UserId)
		require.NoError(t, err)
		require.Nil(t, pending)
		require.NoError(t, ExpireDueEpayOrders(now, 1))
		tracked, err = GetEpayOrderStatus(wallet.UserId, scene, wallet.TradeNo)
		require.NoError(t, err)
		require.Equal(t, common.TopUpStatusExpired, tracked.Status)
		require.Nil(t, tracked.Form)
		var status string
		require.NoError(t, DB.Model(table).Select("status").Scan(&status).Error)
		require.Equal(t, common.TopUpStatusExpired, status)
		next := epayLimitTopup(channel, revision, "wxpay", "10", string(scene)+"-next")
		next.UserId = wallet.UserId
		require.NoError(t, CreateEpayTopupOrder(next))
	}
}

func TestEpayCancelAndSettlementExclusive(t *testing.T) {
	user, order := createEpaySettlementFixture(t)
	start := make(chan struct{})
	paid := make(chan error, 1)
	cancelled := make(chan error, 1)
	go func() { <-start; paid <- CompleteEpayTopup(order.TradeNo) }()
	go func() { <-start; cancelled <- CancelEpayOrder(user.Id, order.TradeNo, types.PaymentSceneTopup) }()
	close(start)
	paidErr, cancelErr := <-paid, <-cancelled
	require.NotEqual(t, paidErr == nil, cancelErr == nil)
	require.NoError(t, DB.First(user, user.Id).Error)
	if cancelErr == nil {
		require.Equal(t, 100, user.Quota)
		require.Error(t, ManualCompleteTopUp(order.TradeNo))
	} else {
		require.Equal(t, 100+order.CreditQuota, user.Quota)
	}
}
