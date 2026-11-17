package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
)

func TestStripeTopupCheckoutCreditsRequestedAmountForEveryGroup(test *testing.T) {
	require.NoError(test, model.DB.AutoMigrate(&model.TopUp{}))
	originalSecret := setting.StripeApiSecret
	originalPriceID := setting.StripePriceId
	originalMinTopup := setting.StripeMinTopUp
	originalKey := stripe.Key
	originalBackend := stripe.GetBackend(stripe.APIBackend)
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalOptions := common.OptionMap
	originalQuotaPerUnit := common.QuotaPerUnit
	test.Cleanup(func() {
		setting.StripeApiSecret = originalSecret
		setting.StripePriceId = originalPriceID
		setting.StripeMinTopUp = originalMinTopup
		stripe.Key = originalKey
		stripe.SetBackend(stripe.APIBackend, originalBackend)
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		common.OptionMap = originalOptions
		common.QuotaPerUnit = originalQuotaPerUnit
	})
	setting.StripeApiSecret = "sk_test_topup"
	setting.StripePriceId = "price_test_topup"
	setting.StripeMinTopUp = 1
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	common.OptionMap = map[string]string{"TopupGroupRatio": `{"default":0.5,"vip":2,"svip":0}`}
	common.QuotaPerUnit = 500000

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(test, http.MethodPost, request.Method)
		assert.Equal(test, "/v1/checkout/sessions", request.URL.Path)
		if !assert.NoError(test, request.ParseForm()) {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(test, "100", request.PostForm.Get("line_items[0][quantity]"))
		assert.Equal(test, "price_test_topup", request.PostForm.Get("line_items[0][price]"))
		payload, err := common.Marshal(map[string]string{
			"id":     "cs_test_topup",
			"object": "checkout.session",
			"url":    "https://checkout.example.com/topup",
		})
		if !assert.NoError(test, err) {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(payload)
	}))
	test.Cleanup(server.Close)
	stripe.SetBackend(stripe.APIBackend, stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
		URL:               stripe.String(server.URL),
		HTTPClient:        server.Client(),
		MaxNetworkRetries: stripe.Int64(0),
	}))

	for _, group := range []string{"default", "vip", "svip", "unconfigured"} {
		test.Run(group, func(test *testing.T) {
			user := &model.User{
				Username: fmt.Sprintf("stripe_topup_%s", group),
				Group:    group,
				AffCode:  fmt.Sprintf("stripe_topup_%s", group),
			}
			require.NoError(test, model.DB.Create(user).Error)
			test.Cleanup(func() {
				model.DB.Where("user_id = ?", user.Id).Delete(&model.Log{})
				model.DB.Where("user_id = ?", user.Id).Delete(&model.TopUp{})
				model.DB.Unscoped().Delete(user)
			})

			provider := &StripePaymentProvider{}
			result, err := provider.CreateTopupCheckout(&types.TopupCheckoutRequest{UserID: user.Id, Amount: 100})
			require.NoError(test, err)
			require.Equal(test, "https://checkout.example.com/topup", result.URL)

			var topup model.TopUp
			require.NoError(test, model.DB.Where("user_id = ?", user.Id).First(&topup).Error)
			require.Equal(test, int64(100), topup.Amount)
			require.Equal(test, float64(100), topup.Money)
			require.Equal(test, model.PaymentProviderStripe, topup.PaymentProvider)

			require.NoError(test, model.Recharge(topup.TradeNo, "cus_test_topup"))
			var reloaded model.User
			require.NoError(test, model.DB.First(&reloaded, user.Id).Error)
			require.Equal(test, common.QuotaFromFloat(100*common.QuotaPerUnit), reloaded.Quota)
			require.NoError(test, model.DB.First(&topup, topup.Id).Error)
			require.Equal(test, common.TopUpStatusSuccess, topup.Status)
		})
	}
}
