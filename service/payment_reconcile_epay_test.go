package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestValidateEpayReconcileOrder(t *testing.T) {
	require.NoError(t, model.DB.AutoMigrate(&model.EpayChannelRevision{}))
	revision := model.EpayChannelRevision{ChannelId: 1, EpayChannelConfig: model.EpayChannelConfig{MerchantId: "pid-1"}}
	require.NoError(t, model.DB.Create(&revision).Error)
	t.Cleanup(func() { model.DB.Delete(&revision) })

	topUp := &model.TopUp{
		TradeNo:         "USR1NOABC1231710000000",
		PaymentMethod:   "wxpay",
		PaymentProvider: model.PaymentProviderEpay,
		Money:           12.3,
		EpayChannelId:   1, EpayChannelRevisionId: revision.Id,
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
