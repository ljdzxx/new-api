package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEpayChannelAdminOrderAndToggle(test *testing.T) {
	originalDB, originalLogDB := model.DB, model.LOG_DB
	test.Cleanup(func() { model.DB, model.LOG_DB = originalDB, originalLogDB })
	database := setupTokenControllerTestDB(test)
	require.NoError(test, database.AutoMigrate(&model.EpayChannel{}, &model.EpayChannelRevision{}))
	for _, name := range []string{"first", "second"} {
		input := service.EpayChannelInput{
			EpayChannelConfig: model.EpayChannelConfig{
				Name: name, PayAddress: "https://pay.example.com", MerchantId: name,
				CallbackAddress: "https://callback.example.com", MinPayCNY: "1", MaxPayCNY: "5000",
				TopupOfferType: "none", TopupOfferThresholdCNY: "0", SubscriptionOfferThresholdCNY: "0",
			},
			Enabled: true, MerchantKey: "private-key", Methods: []string{"wxpay"},
			MethodLimits: map[string]types.EpayMethodLimits{"wxpay": {MinPayCNY: "1", MaxPayCNY: "5000", DailyLimitCNY: "0"}},
		}
		require.NoError(test, service.SaveEpayChannel(0, &input))
	}
	channels, err := model.ListEpayChannels(false)
	require.NoError(test, err)
	first, second := channels[0], channels[1]
	engine := gin.New()
	engine.PUT("/epay/channels/order", AdminReorderEpayChannels)
	engine.PUT("/epay/channels/:id/enabled", AdminSetEpayChannelEnabled)
	request := func(path string, payload interface{}) tokenAPIResponse {
		test.Helper()
		body, err := common.Marshal(payload)
		require.NoError(test, err)
		response := httptest.NewRecorder()
		httpRequest := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(body))
		httpRequest.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(response, httpRequest)
		var result tokenAPIResponse
		require.NoError(test, common.Unmarshal(response.Body.Bytes(), &result))
		return result
	}
	require.True(test, request("/epay/channels/order", map[string]interface{}{
		"channel_ids": []int{second.Id, first.Id}, "expected_ids": []int{first.Id, second.Id},
	}).Success)
	for _, payload := range []interface{}{
		map[string]interface{}{"channel_ids": []int{first.Id, first.Id}, "expected_ids": []int{second.Id, first.Id}},
		map[string]interface{}{"channel_ids": []int{first.Id, second.Id}, "expected_ids": []int{first.Id, second.Id}},
		map[string]interface{}{"channel_ids": []int{first.Id, second.Id}},
		[]string{"invalid"},
	} {
		require.False(test, request("/epay/channels/order", payload).Success)
	}
	path := fmt.Sprintf("/epay/channels/%d/enabled", first.Id)
	require.True(test, request(path, map[string]interface{}{"enabled": false, "revision_id": first.CurrentRevisionId}).Success)
	updated, err := model.GetEpayChannel(first.Id)
	require.NoError(test, err)
	require.False(test, updated.Enabled)
	require.Equal(test, first.CurrentRevisionId, updated.CurrentRevisionId)
	public, err := service.ListPublicEpayChannels(nil)
	require.NoError(test, err)
	require.Len(test, public, 1)
	require.Equal(test, second.Id, public[0].ID)
	for _, payload := range []interface{}{
		map[string]interface{}{"revision_id": first.CurrentRevisionId},
		map[string]interface{}{"enabled": nil, "revision_id": first.CurrentRevisionId},
		map[string]interface{}{"enabled": true, "revision_id": 0},
		map[string]interface{}{"enabled": true, "revision_id": second.CurrentRevisionId},
		[]string{"invalid"},
	} {
		require.False(test, request(path, payload).Success)
	}
	require.False(test, request("/epay/channels/invalid/enabled", map[string]interface{}{"enabled": true}).Success)
	require.True(test, request(path, map[string]interface{}{"enabled": true, "revision_id": first.CurrentRevisionId}).Success)
	public, err = service.ListPublicEpayChannels(nil)
	require.NoError(test, err)
	require.Len(test, public, 2)
	require.Equal(test, second.Id, public[0].ID)
	require.Equal(test, first.Id, public[1].ID)
}

func TestEpayMultiChannelCheckoutAndCallbacks(test *testing.T) {
	originalDB, originalLogDB := model.DB, model.LOG_DB
	originalUnit, originalRate := common.QuotaPerUnit, operation_setting.USDExchangeRate
	originalPolicies := setting.UserLevelPolicies2JSONString()
	test.Cleanup(func() {
		model.DB, model.LOG_DB = originalDB, originalLogDB
		common.QuotaPerUnit, operation_setting.USDExchangeRate = originalUnit, originalRate
		_ = setting.UpdateUserLevelPoliciesByJSONString(originalPolicies)
	})
	database := setupTokenControllerTestDB(test)
	sqlDatabase, err := database.DB()
	require.NoError(test, err)
	sqlDatabase.SetMaxOpenConns(1)
	require.NoError(test, database.AutoMigrate(
		&model.User{}, &model.Log{}, &model.TopUp{}, &model.EpayChannel{}, &model.EpayChannelRevision{},
		&model.SubscriptionPlan{}, &model.SubscriptionOrder{}, &model.UserSubscription{}, &model.UserSubscriptionDailyStat{},
	))
	require.NoError(test, setting.UpdateUserLevelPoliciesByJSONString(`[]`))
	common.QuotaPerUnit, operation_setting.USDExchangeRate = 500000, 5
	user := model.User{Username: "epay_callback_user", AffCode: "epay_callback_user"}
	require.NoError(test, database.Create(&user).Error)
	plan := model.SubscriptionPlan{Title: "epay callback plan", PriceAmount: 10, Enabled: true, DurationUnit: "month", DurationValue: 1, TotalAmount: 5000000}
	require.NoError(test, database.Create(&plan).Error)
	orders := make(map[string]map[string]string)
	remoteOrders := make(map[string]map[string]string)
	queryCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		queryCount++
		require.NoError(test, request.ParseForm())
		var order map[string]string
		code := 0
		if request.Method == http.MethodGet {
			code = 1
			require.Equal(test, "/xpay/epay/api.php", request.URL.Path)
			require.Equal(test, "order", request.Form.Get("act"))
			require.False(test, request.Form.Has("out_trade_no"))
			order = remoteOrders[request.Form.Get("trade_no")]
			require.NotNil(test, order)
			require.Equal(test, "merchant-key-"+order["pid"], request.Form.Get("key"))
		} else {
			require.Equal(test, http.MethodPost, request.Method)
			require.Equal(test, "/api/pay/query", request.URL.Path)
			order = orders[request.PostForm.Get("out_trade_no")]
			require.NotNil(test, order)
			require.Equal(test, expectedEpayMD5Sign(request.PostForm, "merchant-key-"+order["pid"]), request.PostForm.Get("sign"))
		}
		require.NotNil(test, order)
		merchant := order["pid"]
		require.Equal(test, merchant, request.Form.Get("pid"))
		_, _ = fmt.Fprintf(writer, `{"code":%d,"trade_no":"remote-%s","out_trade_no":%q,"pid":%q,"type":%q,"money":%q,"status":"1"}`,
			code, merchant, order["out_trade_no"], merchant, order["type"], order["money"])
	}))
	defer server.Close()
	channels := make([]model.EpayChannel, 0, 2)
	inputs := make([]service.EpayChannelInput, 0, 2)
	for channelIndex, offerType := range []string{"bonus", "discount"} {
		merchant := fmt.Sprint(channelIndex + 1)
		percent := "102"
		queryAPI, payAddress := types.EpayQueryAPISigned, server.URL
		if offerType == "discount" {
			percent = "98"
			queryAPI, payAddress = types.EpayQueryAPIPHP, server.URL+"/xpay/epay"
		}
		input := service.EpayChannelInput{
			EpayChannelConfig: model.EpayChannelConfig{
				Name: "channel-" + merchant, PayAddress: payAddress, QueryAPI: queryAPI, MerchantId: merchant, CallbackAddress: "https://callback.example.com",
				MinPayCNY: "1", MaxPayCNY: "5000", TopupOfferType: offerType, TopupOfferPercent: percent, TopupOfferThresholdCNY: "100",
				SubscriptionDiscountPercent: "90", SubscriptionOfferThresholdCNY: "73",
			}, Enabled: true, MerchantKey: "merchant-key-" + merchant, Methods: []string{"wxpay", "alipay"},
			MethodLimits: map[string]types.EpayMethodLimits{
				"wxpay":  {MinPayCNY: "1", MaxPayCNY: "5000", DailyLimitCNY: "0"},
				"alipay": {MinPayCNY: "1", MaxPayCNY: "5000", DailyLimitCNY: "0"},
			},
		}
		require.NoError(test, service.SaveEpayChannel(0, &input))
		var channel model.EpayChannel
		require.NoError(test, database.Order("id desc").First(&channel).Error)
		channels = append(channels, channel)
		inputs = append(inputs, input)
	}
	invokeCallback := func(fields map[string]string, key, scene string, duplicate bool) string {
		params := map[string]string{
			"pid": fields["pid"], "type": fields["type"], "out_trade_no": fields["out_trade_no"],
			"trade_no": "remote-" + fields["pid"], "trade_status": epay.StatusTradeSuccess, "money": fields["money"],
		}
		params = epay.GenerateParams(params, key)
		values := url.Values{}
		for name, value := range params {
			values.Set(name, value)
		}
		if duplicate {
			values.Add("out_trade_no", fields["out_trade_no"])
		}
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(values.Encode()))
		context.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if scene == "subscription" {
			SubscriptionEpayNotify(context)
		} else {
			EpayNotify(context)
		}
		return recorder.Body.String()
	}
	provider := service.EpayPaymentProvider{}
	for channelIndex, channel := range channels {
		quote, err := service.QuoteEpay(channel.Id, "wxpay", types.PaymentSceneTopup, "100", nil)
		require.NoError(test, err)
		checkout, err := provider.CreateTopupCheckout(&types.TopupCheckoutRequest{
			UserID: user.Id, EpayChannelID: channel.Id, PaymentMethod: "wxpay", AmountCNY: "100", QuoteToken: quote.Token,
		})
		require.NoError(test, err)
		fields := checkout.Form.Fields
		orders[fields["out_trade_no"]] = fields
		remoteOrders["remote-"+fields["pid"]] = fields
		merchantKey := inputs[channelIndex].MerchantKey
		require.Equal(test, "fail", invokeCallback(fields, "other-channel-key", "topup", false))
		require.Equal(test, "fail", invokeCallback(fields, merchantKey, "topup", true))
		alteredFields := make(map[string]string, len(fields))
		for field, value := range fields {
			alteredFields[field] = value
		}
		alteredFields["money"] = "1.00"
		require.Equal(test, "fail", invokeCallback(alteredFields, merchantKey, "topup", false))
		alteredFields["money"], alteredFields["pid"] = fields["money"], "wrong-merchant"
		require.Equal(test, "fail", invokeCallback(alteredFields, merchantKey, "topup", false))
		require.Zero(test, queryCount)
		input := inputs[channelIndex]
		input.RevisionID, input.Enabled, input.MerchantKey, input.TopupOfferPercent = channel.CurrentRevisionId, false, "rotated-key", "105"
		if input.QueryAPI == types.EpayQueryAPIPHP {
			input.QueryAPI = types.EpayQueryAPISigned
		} else {
			input.QueryAPI = types.EpayQueryAPIPHP
		}
		if input.TopupOfferType == "discount" {
			input.TopupOfferPercent = "95"
		}
		require.NoError(test, service.SaveEpayChannel(channel.Id, &input))
		operation_setting.USDExchangeRate = 10
		require.Equal(test, "success", invokeCallback(fields, merchantKey, "topup", false))
		require.Equal(test, "success", invokeCallback(fields, merchantKey, "topup", false))
		queryCount = 0
		operation_setting.USDExchangeRate = 5
	}
	var reloaded model.User
	require.NoError(test, database.First(&reloaded, user.Id).Error)
	require.Equal(test, 20200000, reloaded.Quota)
	total, err := model.GetUserTotalRechargeAmount(user.Id)
	require.NoError(test, err)
	require.Equal(test, float64(198), total)
	for channelIndex, channel := range channels {
		input := inputs[channelIndex]
		var current model.EpayChannel
		require.NoError(test, database.First(&current, channel.Id).Error)
		input.RevisionID = current.CurrentRevisionId
		require.NoError(test, service.SaveEpayChannel(current.Id, &input))
		operation_setting.USDExchangeRate = 7.3
		quote, err := service.QuoteEpay(current.Id, "alipay", types.PaymentSceneSubscription, "", &plan)
		require.NoError(test, err)
		checkout, err := provider.CreateSubscriptionCheckout(&plan, &types.SubscriptionCheckoutRequest{
			UserID: user.Id, PlanID: plan.Id, EpayChannelID: current.Id, PaymentMethod: "alipay", QuoteToken: quote.Token,
		})
		require.NoError(test, err)
		require.Equal(test, "65.70", checkout.Form.Fields["money"])
		require.Equal(test, "https://callback.example.com/api/subscription/epay/notify", checkout.Form.Fields["notify_url"])
		orders[checkout.Form.Fields["out_trade_no"]] = checkout.Form.Fields
		remoteOrders["remote-"+checkout.Form.Fields["pid"]] = checkout.Form.Fields
		operation_setting.USDExchangeRate = 100
		require.Equal(test, "success", invokeCallback(checkout.Form.Fields, input.MerchantKey, "subscription", false))
		require.Equal(test, "success", invokeCallback(checkout.Form.Fields, input.MerchantKey, "subscription", false))
		var audit model.TopUp
		require.NoError(test, database.Where("trade_no = ?", checkout.Form.Fields["out_trade_no"]).First(&audit).Error)
		require.EqualValues(test, 0, audit.Amount)
		require.Equal(test, "65.70", audit.EpayPayCNY)
	}
	var subscriptionCount int64
	require.NoError(test, database.Model(&model.UserSubscription{}).Where("user_id = ?", user.Id).Count(&subscriptionCount).Error)
	require.EqualValues(test, 2, subscriptionCount)
	require.NoError(test, database.First(&reloaded, user.Id).Error)
	require.Equal(test, 20200000, reloaded.Quota)
	total, err = model.GetUserTotalRechargeAmount(user.Id)
	require.NoError(test, err)
	require.Equal(test, float64(198), total)
}
