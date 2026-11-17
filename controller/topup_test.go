package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTopupAmountQuotesIgnoreLegacyGroupRatio(test *testing.T) {
	originalStripeUnitPrice := setting.StripeUnitPrice
	originalStripeMinTopup := setting.StripeMinTopUp
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	originalQuotaPerUnit := common.QuotaPerUnit
	originalOptions := common.OptionMap
	test.Cleanup(func() {
		setting.StripeUnitPrice = originalStripeUnitPrice
		setting.StripeMinTopUp = originalStripeMinTopup
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		common.QuotaPerUnit = originalQuotaPerUnit
		common.OptionMap = originalOptions
	})
	setting.StripeUnitPrice = 1.25
	setting.StripeMinTopUp = 1
	common.QuotaPerUnit = 500000
	common.OptionMap = map[string]string{"TopupGroupRatio": `{"default":0.5,"vip":2}`}

	for _, testCase := range []struct {
		name        string
		displayType string
		amount      int64
		discounts   map[int]float64
		stripeWant  string
	}{
		{name: "USD", displayType: operation_setting.QuotaDisplayTypeUSD, amount: 100, stripeWant: "125.00"},
		{name: "CNY", displayType: operation_setting.QuotaDisplayTypeCNY, amount: 100, stripeWant: "125.00"},
		{name: "tokens", displayType: operation_setting.QuotaDisplayTypeTokens, amount: 1000000, stripeWant: "2.50"},
		{name: "preset discount", displayType: operation_setting.QuotaDisplayTypeUSD, amount: 100, discounts: map[int]float64{100: 0.8}, stripeWant: "100.00"},
		{name: "tokens preset discount", displayType: operation_setting.QuotaDisplayTypeTokens, amount: 1000000, discounts: map[int]float64{1000000: 0.8}, stripeWant: "2.00"},
		{name: "unmatched discount", displayType: operation_setting.QuotaDisplayTypeUSD, amount: 100, discounts: map[int]float64{50: 0.8}, stripeWant: "125.00"},
		{name: "zero discount", displayType: operation_setting.QuotaDisplayTypeUSD, amount: 100, discounts: map[int]float64{100: 0}, stripeWant: "125.00"},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			operation_setting.GetGeneralSetting().QuotaDisplayType = testCase.displayType
			operation_setting.GetPaymentSetting().AmountDiscount = testCase.discounts

			for _, provider := range []struct {
				name    string
				handler gin.HandlerFunc
				want    string
			}{
				{name: "stripe", handler: RequestStripeAmount, want: testCase.stripeWant},
			} {
				test.Run(provider.name, func(test *testing.T) {
					ctx, recorder := newAuthenticatedContext(test, http.MethodPost, "/api/user/amount", map[string]any{"amount": testCase.amount}, 1)
					provider.handler(ctx)

					response := decodeAPIResponse(test, recorder)
					require.Equal(test, "success", response.Message, recorder.Body.String())
					var amount string
					require.NoError(test, common.Unmarshal(response.Data, &amount))
					require.Equal(test, provider.want, amount)
				})
			}
		})
	}
}
