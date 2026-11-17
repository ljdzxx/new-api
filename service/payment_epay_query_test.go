package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestQueryEpayOrderPreservesNumericIdentifiers(test *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"code":0,"trade_no":2026111700000001234,"out_trade_no":"local-order","type":"alipay","pid":1234567890123456789,"money":98.00,"status":1}`))
	}))
	defer server.Close()
	revision := &model.EpayChannelRevision{EpayChannelConfig: model.EpayChannelConfig{PayAddress: server.URL, MerchantId: "1234567890123456789"}, MerchantKey: "test-key"}
	order, err := QueryEpayOrder(context.Background(), revision, "local-order", "")
	require.NoError(test, err)
	require.Equal(test, "2026111700000001234", order.TradeNo)
	require.Equal(test, "1234567890123456789", order.Pid)
	require.Equal(test, "1", order.Status)
	require.True(test, EpayMoneyMatches(order.Money, 98))
}

func TestQueryEpayOrderRejectsMalformedResponses(test *testing.T) {
	for _, payload := range []string{
		`{}`, `{"status":1}`, `{"code":null}`, `{"code":false}`, `{"code":0.5}`,
		`{"code":"0bad"}`, `{"code":0} {"status":1}`, `not json`,
	} {
		test.Run(payload, func(test *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				_, _ = writer.Write([]byte(payload))
			}))
			defer server.Close()
			revision := &model.EpayChannelRevision{EpayChannelConfig: model.EpayChannelConfig{PayAddress: server.URL, MerchantId: "123"}, MerchantKey: "test-key"}
			_, err := QueryEpayOrder(context.Background(), revision, "local-order", "")
			require.Error(test, err)
		})
	}
}
