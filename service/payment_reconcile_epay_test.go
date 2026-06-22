package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestValidateEpayReconcileOrder(t *testing.T) {
	originalEpayId := operation_setting.EpayId
	defer func() {
		operation_setting.EpayId = originalEpayId
	}()
	operation_setting.EpayId = "pid-1"

	topUp := &model.TopUp{
		TradeNo:         "USR1NOABC1231710000000",
		PaymentMethod:   "wxpay",
		PaymentProvider: model.PaymentProviderEpay,
		Money:           12.3,
	}
	info := &EpayOrderInfo{
		Code:       0,
		TradeNo:    "202601010001",
		OutTradeNo: topUp.TradeNo,
		Type:       topUp.PaymentMethod,
		Pid:        "pid-1",
		Money:      "12.30",
		Status:     "1",
	}

	require.NoError(t, validateEpayReconcileOrder(topUp, info))

	info.Money = "12.31"
	require.Error(t, validateEpayReconcileOrder(topUp, info))

	info.Money = "12.30"
	info.TradeNo = ""
	require.Error(t, validateEpayReconcileOrder(topUp, info))
}
