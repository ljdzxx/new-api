package controller

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/shopspring/decimal"
)

var (
	epayTopupTradeNoPattern        = regexp.MustCompile(`^USR[1-9][0-9]*NO[A-Za-z0-9]{6}[0-9]{10,}$`)
	epaySubscriptionTradeNoPattern = regexp.MustCompile(`^SUBUSR[1-9][0-9]*NO[A-Za-z0-9]{6}[0-9]{10,}$`)
)

func validateEpayTopUpCallback(ctx context.Context, verifyInfo *epay.VerifyRes, topUp *model.TopUp) (decimal.Decimal, error) {
	if topUp == nil {
		return decimal.Zero, fmt.Errorf("topup order not found")
	}
	if topUp.PaymentProvider != model.PaymentProviderEpay {
		return decimal.Zero, model.ErrPaymentMethodMismatch
	}
	revision, err := model.GetEpayChannelRevision(topUp.EpayChannelRevisionId)
	if err != nil || revision.ChannelId != topUp.EpayChannelId {
		return decimal.Zero, fmt.Errorf("invalid order channel revision")
	}
	return validateEpayCallbackBase(ctx, revision, verifyInfo, epayTopupTradeNoPattern, topUp.PaymentProvider, topUp.PaymentMethod, topUp.EpayQuotedMoney())
}

func validateEpaySubscriptionCallback(ctx context.Context, verifyInfo *epay.VerifyRes, order *model.SubscriptionOrder) error {
	if order == nil {
		return fmt.Errorf("subscription order not found")
	}
	if order.PaymentProvider != model.PaymentProviderEpay {
		return model.ErrPaymentMethodMismatch
	}
	revision, err := model.GetEpayChannelRevision(order.EpayChannelRevisionId)
	if err != nil || revision.ChannelId != order.EpayChannelId {
		return fmt.Errorf("invalid order channel revision")
	}
	_, err = validateEpayCallbackBase(ctx, revision, verifyInfo, epaySubscriptionTradeNoPattern, order.PaymentProvider, order.PaymentMethod, order.Money)
	return err
}

func validateEpayCallbackBase(ctx context.Context, revision *model.EpayChannelRevision, verifyInfo *epay.VerifyRes, tradeNoPattern *regexp.Regexp, paymentProvider string, paymentMethod string, expectedMoney float64) (decimal.Decimal, error) {
	if verifyInfo == nil {
		return decimal.Zero, fmt.Errorf("epay verify info is nil")
	}
	if strings.TrimSpace(verifyInfo.TradeNo) == "" {
		return decimal.Zero, fmt.Errorf("epay trade_no is empty")
	}
	if !tradeNoPattern.MatchString(strings.TrimSpace(verifyInfo.ServiceTradeNo)) {
		return decimal.Zero, fmt.Errorf("invalid epay out_trade_no format: %s", verifyInfo.ServiceTradeNo)
	}
	if paymentProvider != model.PaymentProviderEpay {
		return decimal.Zero, fmt.Errorf("payment provider mismatch: %s", paymentProvider)
	}
	if paymentMethod != verifyInfo.Type {
		return decimal.Zero, fmt.Errorf("payment method mismatch: order=%s callback=%s", paymentMethod, verifyInfo.Type)
	}
	if _, ok := service.EpayPaymentDifference(revision.QueryAPI, verifyInfo.Money, expectedMoney); !ok {
		return decimal.Zero, fmt.Errorf("payment money mismatch: order=%.6f callback=%s", expectedMoney, verifyInfo.Money)
	}

	orderInfo, err := service.QueryEpayOrder(ctx, revision, verifyInfo.ServiceTradeNo, verifyInfo.TradeNo)
	if err != nil {
		return decimal.Zero, fmt.Errorf("query epay order failed: %w", err)
	}
	if !orderInfo.IsSuccess() {
		return decimal.Zero, fmt.Errorf("epay order query failed: code=%d msg=%s", orderInfo.Code, orderInfo.Msg)
	}
	if orderInfo.Status != "1" {
		return decimal.Zero, fmt.Errorf("epay order is not paid: status=%s", orderInfo.Status)
	}
	if orderInfo.OutTradeNo != verifyInfo.ServiceTradeNo {
		return decimal.Zero, fmt.Errorf("epay out_trade_no mismatch: query=%s callback=%s", orderInfo.OutTradeNo, verifyInfo.ServiceTradeNo)
	}
	if orderInfo.TradeNo != verifyInfo.TradeNo {
		return decimal.Zero, fmt.Errorf("epay trade_no mismatch: query=%s callback=%s", orderInfo.TradeNo, verifyInfo.TradeNo)
	}
	if orderInfo.Type != verifyInfo.Type {
		return decimal.Zero, fmt.Errorf("epay payment type mismatch: query=%s callback=%s", orderInfo.Type, verifyInfo.Type)
	}
	if orderInfo.Pid != revision.MerchantId {
		return decimal.Zero, fmt.Errorf("epay pid mismatch: query=%s", orderInfo.Pid)
	}
	// PHP notifications may quote the original price. Settle the surcharge from the verified order query.
	queryDiff, ok := service.EpayPaymentDifference(revision.QueryAPI, orderInfo.Money, expectedMoney)
	if !ok {
		return decimal.Zero, fmt.Errorf("epay query money mismatch: query=%s callback=%s order=%.6f", orderInfo.Money, verifyInfo.Money, expectedMoney)
	}
	return queryDiff, nil
}
