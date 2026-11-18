package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
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

func TestEpayReconcileQueryProtocols(test *testing.T) {
	require.NoError(test, model.DB.AutoMigrate(&model.EpayChannelRevision{}))
	for _, queryAPI := range []string{types.EpayQueryAPISigned, types.EpayQueryAPIPHP} {
		test.Run(queryAPI, func(test *testing.T) {
			code := 0
			if queryAPI == types.EpayQueryAPIPHP {
				code = 1
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				require.NoError(test, request.ParseForm())
				require.Equal(test, "local-reconcile-order", request.Form.Get("out_trade_no"))
				require.False(test, request.Form.Has("trade_no"))
				_, _ = fmt.Fprintf(writer, `{"code":%d,"trade_no":"remote-order","out_trade_no":"local-reconcile-order","pid":"1001","type":"wxpay","money":"12.30","status":1}`, code)
			}))
			defer server.Close()
			revision := model.EpayChannelRevision{
				ChannelId: 1, EpayChannelConfig: model.EpayChannelConfig{MerchantId: "1001", QueryAPI: queryAPI, PayAddress: server.URL}, MerchantKey: "test-key",
			}
			require.NoError(test, model.DB.Create(&revision).Error)
			test.Cleanup(func() { model.DB.Delete(&revision) })
			topup := &model.TopUp{
				TradeNo: "local-reconcile-order", PaymentMethod: "wxpay", PaymentProvider: model.PaymentProviderEpay,
				Money: 12.3, EpayChannelId: 1, EpayChannelRevisionId: revision.Id,
			}
			info, err := QueryEpayOrder(context.Background(), &revision, topup.TradeNo, "")
			require.NoError(test, err)
			require.NoError(test, validateEpayReconcileOrder(topup, info))
			if queryAPI == types.EpayQueryAPIPHP {
				booked := *topup
				booked.EpayPayCNY, booked.Amount, booked.Status, booked.Money = "12.30", 100, common.TopUpStatusSuccess, 12.67
				paid := *info
				paid.Money = "12.6700"
				require.NoError(test, validateEpayReconcileOrder(&booked, &paid))
				paid.Money = "12.68"
				require.Error(test, validateEpayReconcileOrder(&booked, &paid))
			}
			for _, scenario := range []struct {
				name   string
				change func(*EpayOrderInfo)
			}{
				{"wrong success code", func(info *EpayOrderInfo) { info.Code = 1 - code }},
				{"unpaid", func(info *EpayOrderInfo) { info.Status = "0" }},
				{"wrong order", func(info *EpayOrderInfo) { info.OutTradeNo = "other-order" }},
				{"wrong merchant", func(info *EpayOrderInfo) { info.Pid = "other-merchant" }},
				{"wrong method", func(info *EpayOrderInfo) { info.Type = "alipay" }},
				{"wrong amount", func(info *EpayOrderInfo) { info.Money = "13.30" }},
				{"missing platform order", func(info *EpayOrderInfo) { info.TradeNo = "" }},
			} {
				test.Run(scenario.name, func(test *testing.T) {
					invalid := *info
					scenario.change(&invalid)
					require.Error(test, validateEpayReconcileOrder(topup, &invalid))
				})
			}
		})
	}
}
