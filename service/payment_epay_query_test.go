package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
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
	require.True(test, order.IsSuccess())
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

func TestQueryEpayOrderProtocols(test *testing.T) {
	for _, scenario := range []struct {
		name, queryAPI, outTradeNo, tradeNo, method, path string
		code                                              int
	}{
		{"legacy default", "", "local-order", "remote-order", http.MethodPost, "/api/pay/query", 0},
		{"signed", types.EpayQueryAPISigned, "local-order", "", http.MethodPost, "/api/pay/query", 0},
		{"PHP merchant order", types.EpayQueryAPIPHP, "local-order", "", http.MethodGet, "/api.php", 1},
		{"PHP platform order", types.EpayQueryAPIPHP, "", "remote-order", http.MethodGet, "/api.php", 1},
		{"PHP platform order priority", types.EpayQueryAPIPHP, "local-order", "remote-order", http.MethodGet, "/api.php", 1},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			merchantKey := "test +&/=?%密钥"
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(test, scenario.method, request.Method)
				require.Equal(test, "/xpay/epay"+scenario.path, request.URL.Path)
				require.NoError(test, request.ParseForm())
				require.Equal(test, "1001", request.Form.Get("pid"))
				require.Equal(test, scenario.tradeNo, request.Form.Get("trade_no"))
				if scenario.queryAPI == types.EpayQueryAPIPHP {
					require.Empty(test, request.PostForm)
					require.Equal(test, "order", request.URL.Query().Get("act"))
					require.Equal(test, merchantKey, request.URL.Query().Get("key"))
					if scenario.tradeNo == "" {
						require.Equal(test, scenario.outTradeNo, request.Form.Get("out_trade_no"))
					} else {
						require.False(test, request.Form.Has("out_trade_no"))
					}
					for _, field := range []string{"sign", "sign_type", "timestamp"} {
						require.False(test, request.Form.Has(field))
					}
				} else {
					require.Empty(test, request.URL.RawQuery)
					require.Equal(test, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
					require.Equal(test, scenario.outTradeNo, request.PostForm.Get("out_trade_no"))
					require.NotEmpty(test, request.PostForm.Get("timestamp"))
					require.Equal(test, "MD5", request.PostForm.Get("sign_type"))
					require.Equal(test, makeEpayMD5Sign(request.PostForm, merchantKey), request.PostForm.Get("sign"))
					require.False(test, request.Form.Has("key"))
				}
				_, _ = fmt.Fprintf(writer, `{"code":%d,"message":"queried","trade_no":2026111700000001234,"out_trade_no":"local-order","pid":1001,"money":"98.00","type":"alipay","status":1}`, scenario.code)
			}))
			defer server.Close()
			revision := &model.EpayChannelRevision{
				EpayChannelConfig: model.EpayChannelConfig{PayAddress: server.URL + "/xpay/epay/", MerchantId: "1001", QueryAPI: scenario.queryAPI},
				MerchantKey:       merchantKey,
			}
			order, err := QueryEpayOrder(context.Background(), revision, scenario.outTradeNo, scenario.tradeNo)
			require.NoError(test, err)
			require.True(test, order.IsSuccess())
			require.Equal(test, "queried", order.Msg)
			require.Equal(test, "2026111700000001234", order.TradeNo)
			require.Equal(test, "local-order", order.OutTradeNo)
			require.Equal(test, "1001", order.Pid)
			require.Equal(test, "1", order.Status)
		})
	}
}

func TestEpayOrderInfoProtocolSuccessCodes(test *testing.T) {
	var empty *EpayOrderInfo
	require.False(test, empty.IsSuccess())
	for _, scenario := range []struct {
		queryAPI string
		code     int
		success  bool
	}{
		{"", 0, true},
		{types.EpayQueryAPISigned, 0, true},
		{types.EpayQueryAPISigned, 1, false},
		{types.EpayQueryAPIPHP, 1, true},
		{types.EpayQueryAPIPHP, 0, false},
		{types.EpayQueryAPIPHP, -1, false},
		{types.EpayQueryAPIPHP, 404, false},
		{"unknown", 0, false},
	} {
		info := &EpayOrderInfo{QueryAPI: scenario.queryAPI, Code: scenario.code}
		require.Equal(test, scenario.success, info.IsSuccess(), "%s code=%d", scenario.queryAPI, scenario.code)
	}
}

func TestQueryEpayOrderRejectsUnsafeResponses(test *testing.T) {
	merchantKey := "test +&/=?%secret"
	for _, scenario := range []struct {
		name, payload string
		status        int
		cancel        bool
	}{
		{"HTTP error", merchantKey, http.StatusBadGateway, false},
		{"oversized response", strings.Repeat(" ", 1048577), http.StatusOK, false},
		{"invalid JSON", "invalid " + merchantKey, http.StatusOK, false},
		{"invalid code", `{"code":` + fmt.Sprintf("%q", merchantKey) + `}`, http.StatusOK, false},
		{"transport error", "", http.StatusOK, true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(scenario.status)
				_, _ = writer.Write([]byte(scenario.payload))
			}))
			defer server.Close()
			revision := &model.EpayChannelRevision{
				EpayChannelConfig: model.EpayChannelConfig{PayAddress: server.URL, MerchantId: "1001", QueryAPI: types.EpayQueryAPIPHP},
				MerchantKey:       merchantKey,
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario.cancel {
				cancel()
			}
			_, err := QueryEpayOrder(ctx, revision, "local-order", "")
			require.Error(test, err)
			for _, secret := range []string{merchantKey, url.QueryEscape(merchantKey), url.PathEscape(merchantKey)} {
				require.NotContains(test, err.Error(), secret)
			}
		})
	}
}

func TestQueryEpayOrderRedactsFailureMessage(test *testing.T) {
	merchantKey := "test +&/=?%secret"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		payload, err := common.Marshal(map[string]any{"code": -1, "msg": merchantKey + " " + url.QueryEscape(merchantKey) + " " + url.PathEscape(merchantKey)})
		require.NoError(test, err)
		_, _ = writer.Write(payload)
	}))
	defer server.Close()
	revision := &model.EpayChannelRevision{
		EpayChannelConfig: model.EpayChannelConfig{PayAddress: server.URL, MerchantId: "1001", QueryAPI: types.EpayQueryAPIPHP},
		MerchantKey:       merchantKey,
	}
	order, err := QueryEpayOrder(context.Background(), revision, "local-order", "")
	require.NoError(test, err)
	require.False(test, order.IsSuccess())
	require.Equal(test, "[REDACTED] [REDACTED] [REDACTED]", order.Msg)
}

func TestQueryEpayOrderDoesNotFollowRedirectOrFallback(test *testing.T) {
	var redirectCalls, queryCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirectCalls.Add(1)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		queryCalls.Add(1)
		require.Equal(test, "/api.php", request.URL.Path)
		http.Redirect(writer, request, target.URL+"?key="+request.URL.Query().Get("key"), http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	revision := &model.EpayChannelRevision{
		EpayChannelConfig: model.EpayChannelConfig{PayAddress: server.URL, MerchantId: "1001", QueryAPI: types.EpayQueryAPIPHP},
		MerchantKey:       "redirect-secret",
	}
	_, err := QueryEpayOrder(context.Background(), revision, "local-order", "")
	require.ErrorContains(test, err, "http 307")
	require.Zero(test, redirectCalls.Load())
	require.EqualValues(test, 1, queryCalls.Load())
	revision.QueryAPI = "unknown"
	_, err = QueryEpayOrder(context.Background(), revision, "local-order", "")
	require.ErrorContains(test, err, "查单接口类型无效")
	require.EqualValues(test, 1, queryCalls.Load())
}
