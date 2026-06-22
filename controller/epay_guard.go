package controller

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

var (
	epayTopupTradeNoPattern        = regexp.MustCompile(`^USR[1-9][0-9]*NO[A-Za-z0-9]{6}[0-9]{10,}$`)
	epaySubscriptionTradeNoPattern = regexp.MustCompile(`^SUBUSR[1-9][0-9]*NO[A-Za-z0-9]{6}[0-9]{10,}$`)
)

func validateEpayTopUpCallback(ctx context.Context, verifyInfo *epay.VerifyRes, topUp *model.TopUp) error {
	if topUp == nil {
		return fmt.Errorf("topup order not found")
	}
	if err := validateEpayCallbackBase(ctx, verifyInfo, epayTopupTradeNoPattern, topUp.PaymentProvider, topUp.PaymentMethod, topUp.Money); err != nil {
		return err
	}
	return nil
}

func validateEpaySubscriptionCallback(ctx context.Context, verifyInfo *epay.VerifyRes, order *model.SubscriptionOrder) error {
	if order == nil {
		return fmt.Errorf("subscription order not found")
	}
	if err := validateEpayCallbackBase(ctx, verifyInfo, epaySubscriptionTradeNoPattern, order.PaymentProvider, order.PaymentMethod, order.Money); err != nil {
		return err
	}
	return nil
}

func validateEpayCallbackBase(ctx context.Context, verifyInfo *epay.VerifyRes, tradeNoPattern *regexp.Regexp, paymentProvider string, paymentMethod string, expectedMoney float64) error {
	if verifyInfo == nil {
		return fmt.Errorf("epay verify info is nil")
	}
	if strings.TrimSpace(verifyInfo.TradeNo) == "" {
		return fmt.Errorf("epay trade_no is empty")
	}
	if !tradeNoPattern.MatchString(strings.TrimSpace(verifyInfo.ServiceTradeNo)) {
		return fmt.Errorf("invalid epay out_trade_no format: %s", verifyInfo.ServiceTradeNo)
	}
	if paymentProvider != model.PaymentProviderEpay {
		return fmt.Errorf("payment provider mismatch: %s", paymentProvider)
	}
	if paymentMethod != verifyInfo.Type {
		return fmt.Errorf("payment method mismatch: order=%s callback=%s", paymentMethod, verifyInfo.Type)
	}
	if !sameMoney(verifyInfo.Money, expectedMoney) {
		return fmt.Errorf("payment money mismatch: order=%.6f callback=%s", expectedMoney, verifyInfo.Money)
	}

	orderInfo, err := service.QueryEpayOrder(ctx, verifyInfo.ServiceTradeNo, verifyInfo.TradeNo)
	if err != nil {
		return fmt.Errorf("query epay order failed: %w", err)
	}
	if orderInfo.Code != 0 {
		return fmt.Errorf("epay order query failed: code=%d msg=%s", orderInfo.Code, orderInfo.Msg)
	}
	if orderInfo.Status != "1" {
		return fmt.Errorf("epay order is not paid: status=%s", orderInfo.Status)
	}
	if orderInfo.OutTradeNo != verifyInfo.ServiceTradeNo {
		return fmt.Errorf("epay out_trade_no mismatch: query=%s callback=%s", orderInfo.OutTradeNo, verifyInfo.ServiceTradeNo)
	}
	if orderInfo.TradeNo != verifyInfo.TradeNo {
		return fmt.Errorf("epay trade_no mismatch: query=%s callback=%s", orderInfo.TradeNo, verifyInfo.TradeNo)
	}
	if orderInfo.Type != verifyInfo.Type {
		return fmt.Errorf("epay payment type mismatch: query=%s callback=%s", orderInfo.Type, verifyInfo.Type)
	}
	if orderInfo.Pid != operation_setting.EpayId {
		return fmt.Errorf("epay pid mismatch: query=%s", orderInfo.Pid)
	}
	if !sameMoney(orderInfo.Money, expectedMoney) || !sameMoney(orderInfo.Money, mustMoneyFloat(verifyInfo.Money)) {
		return fmt.Errorf("epay query money mismatch: query=%s callback=%s order=%.6f", orderInfo.Money, verifyInfo.Money, expectedMoney)
	}
	return nil
}

func sameMoney(actual string, expected float64) bool {
	actualDecimal, err := decimal.NewFromString(strings.TrimSpace(actual))
	if err != nil {
		return false
	}
	expectedDecimal := decimal.NewFromFloat(expected)
	return actualDecimal.Round(2).Equal(expectedDecimal.Round(2))
}

func mustMoneyFloat(value string) float64 {
	money, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil {
		return -1
	}
	result, _ := money.Float64()
	return result
}
