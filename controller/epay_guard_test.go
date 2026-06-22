package controller

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestValidateEpayTopUpCallbackChecksRemoteOrder(t *testing.T) {
	originalPayAddress := operation_setting.PayAddress
	originalEpayId := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	defer func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayId
		operation_setting.EpayKey = originalEpayKey
	}()

	verifyInfo := &epay.VerifyRes{
		Type:           "alipay",
		TradeNo:        "202606220001",
		ServiceTradeNo: "USR12NOABC1231710000000",
		Money:          "12.34",
		TradeStatus:    epay.StatusTradeSuccess,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/pay/query", r.URL.Path)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "pid-1", r.PostForm.Get("pid"))
		require.Equal(t, verifyInfo.ServiceTradeNo, r.PostForm.Get("out_trade_no"))
		require.Equal(t, verifyInfo.TradeNo, r.PostForm.Get("trade_no"))
		require.NotEmpty(t, r.PostForm.Get("timestamp"))
		require.Equal(t, "MD5", r.PostForm.Get("sign_type"))
		require.Equal(t, expectedEpayMD5Sign(r.PostForm, "key-1"), r.PostForm.Get("sign"))
		_, _ = fmt.Fprintf(w, `{"code":0,"msg":"succ","trade_no":%q,"out_trade_no":%q,"type":"alipay","pid":"pid-1","money":"12.34","status":"1"}`, verifyInfo.TradeNo, verifyInfo.ServiceTradeNo)
	}))
	defer server.Close()

	operation_setting.PayAddress = server.URL
	operation_setting.EpayId = "pid-1"
	operation_setting.EpayKey = "key-1"

	topUp := &model.TopUp{
		TradeNo:         verifyInfo.ServiceTradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderEpay,
		Money:           12.34,
	}
	require.NoError(t, validateEpayTopUpCallback(context.Background(), verifyInfo, topUp))
}

func TestValidateEpayTopUpCallbackRejectsProviderMismatch(t *testing.T) {
	verifyInfo := &epay.VerifyRes{
		Type:           "alipay",
		TradeNo:        "202606220001",
		ServiceTradeNo: "USR12NOABC1231710000000",
		Money:          "12.34",
		TradeStatus:    epay.StatusTradeSuccess,
	}
	topUp := &model.TopUp{
		TradeNo:         verifyInfo.ServiceTradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: model.PaymentProviderStripe,
		Money:           12.34,
	}
	require.Error(t, validateEpayTopUpCallback(context.Background(), verifyInfo, topUp))
}

func expectedEpayMD5Sign(values url.Values, key string) string {
	names := make([]string, 0, len(values))
	for name := range values {
		if name == "sign" || name == "sign_type" || strings.TrimSpace(values.Get(name)) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+values.Get(name))
	}
	return fmt.Sprintf("%x", md5.Sum([]byte(strings.Join(parts, "&")+key)))
}
