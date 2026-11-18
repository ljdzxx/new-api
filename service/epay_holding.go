package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/bytedance/gopkg/util/gopool"
)

func CancelPendingEpayOrder(ctx context.Context, userID int, tradeNo string, scene types.PaymentScene) error {
	order, err := model.CurrentEpayOrder(userID)
	if err != nil {
		return err
	}
	if order == nil || order.TradeNo != tradeNo || order.Scene != scene {
		return model.ErrTopUpStatusInvalid
	}
	// Check outside the transaction. A verified paid order awaits the normal
	// signed callback; cancellation itself is never a new path to grant credit.
	revision, err := model.GetEpayChannelRevision(order.EpayChannelRevisionId)
	if err == nil && revision.ChannelId == order.EpayChannelId {
		info, err := QueryEpayOrder(ctx, revision, tradeNo, "")
		if err == nil && info.IsSuccess() && info.Status == "1" && info.TradeNo != "" && info.OutTradeNo == tradeNo && info.Pid == revision.MerchantId && info.Type == order.PaymentMethod {
			if _, ok := EpayPaymentDifference(revision.QueryAPI, info.Money, order.Money); ok {
				return errors.New("平台已确认付款，请等待支付通知或联系管理员核对")
			}
		}
	}
	return model.CancelEpayOrder(userID, tradeNo, scene)
}

func StartEpayOrderExpiryTask() {
	if !common.IsMasterNode {
		return
	}
	gopool.Go(func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			if err := model.ExpireDueEpayOrders(time.Now().Unix(), 200); err != nil {
				common.SysError("expire epay orders: " + err.Error())
			}
			<-ticker.C
		}
	})
}
