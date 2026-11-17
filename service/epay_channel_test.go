package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func epayTestInput() EpayChannelInput {
	return EpayChannelInput{
		EpayChannelConfig: model.EpayChannelConfig{
			Name: "test channel", PayAddress: "https://pay.example.com", MerchantId: "merchant-1",
			CallbackAddress: "https://callback.example.com", MinPayCNY: "0.01", MaxPayCNY: "5000",
			TopupOfferType: "none", TopupOfferPercent: "100", TopupOfferThresholdCNY: "0",
			SubscriptionDiscountPercent: "100", SubscriptionOfferThresholdCNY: "0",
		},
		Enabled: true, MerchantKey: "test-merchant-key", Methods: []string{"wxpay", "alipay"},
		MethodLimits: map[string]types.EpayMethodLimits{
			"wxpay":  {MinPayCNY: "0.01", MaxPayCNY: "5000", DailyLimitCNY: "0"},
			"alipay": {MinPayCNY: "0.01", MaxPayCNY: "5000", DailyLimitCNY: "0"},
		},
	}
}

func epayTestRevision() *model.EpayChannelRevision {
	input := epayTestInput()
	input.PaymentMethods = `["wxpay","alipay"]`
	return &model.EpayChannelRevision{Id: 12, ChannelId: 3, EpayChannelConfig: input.EpayChannelConfig, MerchantKey: input.MerchantKey}
}

func setupEpayQuoteTest(test *testing.T) {
	test.Helper()
	originalQuotaPerUnit := common.QuotaPerUnit
	originalRate := operation_setting.USDExchangeRate
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	test.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.USDExchangeRate = originalRate
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
	})
	common.QuotaPerUnit = 500000
	operation_setting.USDExchangeRate = 5
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{100: 0.01}
}

func TestEpayWalletQuotes(test *testing.T) {
	setupEpayQuoteTest(test)
	for _, scenario := range []struct {
		name, offer, percent, threshold, amount, pay, base, bonus, credit string
		applied                                                           bool
	}{
		{"none ignores Stripe discounts", "none", "100", "0", "100", "100.00", "20", "0", "20", false},
		{"bonus at threshold", "bonus", "102", "100", "100", "100.00", "20", "0.4", "20.4", true},
		{"bonus below threshold", "bonus", "102", "100", "99.99", "99.99", "19.998", "0", "19.998", false},
		{"discount at threshold", "discount", "98", "100", "100", "98.00", "20", "0", "20", true},
		{"discount below threshold", "discount", "98", "100", "99.99", "99.99", "19.998", "0", "19.998", false},
		{"discount cents rounded", "discount", "98", "0", "1.23", "1.21", "0.246", "0", "0.246", true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			revision := epayTestRevision()
			revision.TopupOfferType = scenario.offer
			revision.TopupOfferPercent = scenario.percent
			revision.TopupOfferThresholdCNY = scenario.threshold
			quote, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, scenario.amount, nil)
			require.NoError(test, err)
			require.True(test, quote.Payable)
			require.Equal(test, scenario.pay, quote.PayCNY)
			require.Equal(test, scenario.base, quote.BaseUSD)
			require.Equal(test, scenario.bonus, quote.BonusUSD)
			require.Equal(test, scenario.credit, quote.CreditUSD)
			require.Equal(test, scenario.applied, quote.OfferApplied)
			require.Equal(test, quote.BaseQuota+quote.BonusQuota, quote.CreditQuota)
			require.NotEmpty(test, quote.Token)
		})
	}
}

func TestEpayActualPaymentLimits(test *testing.T) {
	setupEpayQuoteTest(test)
	revision := epayTestRevision()
	revision.TopupOfferType, revision.TopupOfferPercent, revision.TopupOfferThresholdCNY = "discount", "98", "100"
	revision.MinPayCNY, revision.MaxPayCNY = "98", "98"
	quote, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, "100", nil)
	require.NoError(test, err)
	require.True(test, quote.Payable)
	for _, amount := range []string{"99.99", "100.01"} {
		quote, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, amount, nil)
		require.NoError(test, err)
		require.False(test, quote.Payable, amount)
	}
	revision.MinPayCNY, revision.MaxPayCNY = "98.01", "5000"
	quote, err = CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, "100", nil)
	require.NoError(test, err)
	require.False(test, quote.Payable)
	revision.TopupOfferPercent, revision.TopupOfferThresholdCNY = "0.01", "0"
	quote, err = CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, "0.01", nil)
	require.NoError(test, err)
	require.Equal(test, "0.00", quote.PayCNY)
	require.False(test, quote.Payable)
}

func TestEpayWalletUsesReciprocalSystemRate(test *testing.T) {
	setupEpayQuoteTest(test)
	operation_setting.USDExchangeRate = 7.3
	revision := epayTestRevision()
	quote, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, "73", nil)
	require.NoError(test, err)
	require.Equal(test, "10", quote.BaseUSD)
	require.Equal(test, 5000000, quote.BaseQuota)
	require.Equal(test, "7.3", quote.USDExchangeRate)
	quote, err = CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, "100", nil)
	require.NoError(test, err)
	require.Equal(test, "13.69863", quote.BaseUSD)
	require.Equal(test, 6849315, quote.BaseQuota)
	revision.TopupOfferType, revision.TopupOfferPercent = "bonus", "102"
	quote, err = CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, "73", nil)
	require.NoError(test, err)
	require.Equal(test, "0.2", quote.BonusUSD)
	require.Equal(test, "10.2", quote.CreditUSD)
}

func TestEpayMethodSpecificLimitsAndTokens(test *testing.T) {
	setupEpayQuoteTest(test)
	revision := epayTestRevision()
	limits, err := common.Marshal(map[string]types.EpayMethodLimits{
		"wxpay":  {MinPayCNY: "98", MaxPayCNY: "98", DailyLimitCNY: "1000"},
		"alipay": {MinPayCNY: "100", MaxPayCNY: "1000", DailyLimitCNY: "0"},
		"qqpay":  {MinPayCNY: "1", MaxPayCNY: "5000", DailyLimitCNY: "0"},
	})
	require.NoError(test, err)
	revision.PaymentMethodLimits = string(limits)
	revision.TopupOfferType, revision.TopupOfferPercent, revision.TopupOfferThresholdCNY = "discount", "98", "0"
	wxpay, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, "100", nil)
	require.NoError(test, err)
	require.True(test, wxpay.Payable)
	require.Equal(test, "98.00", wxpay.PayCNY)
	alipay, err := CalculateEpayQuote(revision, "alipay", types.PaymentSceneTopup, "100", nil)
	require.NoError(test, err)
	require.False(test, alipay.Payable)
	qqpay, err := CalculateEpayQuote(revision, "qqpay", types.PaymentSceneTopup, "100", nil)
	require.NoError(test, err)
	require.True(test, qqpay.Payable)
	require.NotEqual(test, wxpay.Token, qqpay.Token)
	require.Error(test, validateEpayQuoteToken(qqpay, wxpay.Token))
	_, err = CalculateEpayQuote(revision, "stripe", types.PaymentSceneTopup, "100", nil)
	require.Error(test, err)
	applyEpayDailyAvailability(wxpay, "98.00", 123)
	require.True(test, wxpay.Payable)
	require.Equal(test, "98.00", wxpay.DailyRemainingCNY)
	applyEpayDailyAvailability(wxpay, "97.99", 123)
	require.False(test, wxpay.Payable)
	require.Contains(test, wxpay.UnavailableReason, "今日剩余")
}

func TestEpayDailyRemainingAndSubscriptionMethodQuotes(test *testing.T) {
	setupEpayQuoteTest(test)
	require.NoError(test, model.DB.AutoMigrate(&model.TopUp{}, &model.SubscriptionOrder{}))
	input := epayTestInput()
	input.Name = "daily remaining service test"
	input.MethodLimits["wxpay"] = types.EpayMethodLimits{MinPayCNY: "1", MaxPayCNY: "100", DailyLimitCNY: "100"}
	input.MethodLimits["alipay"] = types.EpayMethodLimits{MinPayCNY: "1", MaxPayCNY: "100", DailyLimitCNY: "200"}
	require.NoError(test, SaveEpayChannel(0, &input))
	var channel model.EpayChannel
	require.NoError(test, model.DB.Order("id desc").First(&channel).Error)
	test.Cleanup(func() {
		model.DB.Where("epay_channel_id = ?", channel.Id).Delete(&model.TopUp{})
		model.DB.Where("epay_channel_id = ?", channel.Id).Delete(&model.SubscriptionOrder{})
		model.DB.Where("channel_id = ?", channel.Id).Delete(&model.EpayChannelRevision{})
		model.DB.Delete(&channel)
	})
	quote, err := QuoteEpay(channel.Id, "wxpay", types.PaymentSceneTopup, "50", nil)
	require.NoError(test, err)
	require.True(test, quote.Payable)
	require.Equal(test, "100.00", quote.DailyRemainingCNY)
	topup := &model.TopUp{TradeNo: "daily-test-first", UserId: 1, Amount: 1, PaymentProvider: model.PaymentProviderEpay, PaymentMethod: "wxpay", Status: common.TopUpStatusPending,
		EpayChannelId: channel.Id, EpayChannelRevisionId: channel.CurrentRevisionId, EpayPayCNY: "60.00"}
	require.NoError(test, model.CreateEpayTopupOrder(topup))
	second, err := QuoteEpay(channel.Id, "wxpay", types.PaymentSceneTopup, "50", nil)
	require.NoError(test, err)
	require.False(test, second.Payable)
	require.Equal(test, "40.00", second.DailyRemainingCNY)
	require.Equal(test, quote.Token, second.Token)
	plan := &model.SubscriptionPlan{Id: 1, PriceAmount: 10, Currency: "USD", Enabled: true}
	channels, err := ListPublicEpayChannels(plan)
	require.NoError(test, err)
	found := false
	for _, public := range channels {
		if public.ID != channel.Id {
			continue
		}
		found = true
		require.False(test, public.MethodLimits["wxpay"].SubscriptionQuote.Payable)
		require.True(test, public.MethodLimits["alipay"].SubscriptionQuote.Payable)
		require.Equal(test, "200.00", public.MethodLimits["alipay"].DailyRemainingCNY)
	}
	require.True(test, found)
}

func TestEpayCheckoutRechecksSharedDailyCapacityAfterQuote(test *testing.T) {
	setupEpayQuoteTest(test)
	require.NoError(test, model.DB.AutoMigrate(&model.TopUp{}, &model.SubscriptionOrder{}))
	input := epayTestInput()
	input.MethodLimits["wxpay"] = types.EpayMethodLimits{MinPayCNY: "1", MaxPayCNY: "100", DailyLimitCNY: "100"}
	require.NoError(test, SaveEpayChannel(0, &input))
	var channel model.EpayChannel
	require.NoError(test, model.DB.Order("id desc").First(&channel).Error)
	user := model.User{Username: "epay_daily_checkout", AffCode: "epay_daily_checkout"}
	require.NoError(test, model.DB.Create(&user).Error)
	test.Cleanup(func() {
		model.DB.Where("epay_channel_id = ?", channel.Id).Delete(&model.TopUp{})
		model.DB.Where("epay_channel_id = ?", channel.Id).Delete(&model.SubscriptionOrder{})
		model.DB.Where("channel_id = ?", channel.Id).Delete(&model.EpayChannelRevision{})
		model.DB.Delete(&channel)
		model.DB.Unscoped().Delete(&user)
	})
	plan := &model.SubscriptionPlan{Id: 1, PriceAmount: 10, Currency: "USD", Enabled: true}
	walletQuote, err := QuoteEpay(channel.Id, "wxpay", types.PaymentSceneTopup, "50", nil)
	require.NoError(test, err)
	subscriptionQuote, err := QuoteEpay(channel.Id, "wxpay", types.PaymentSceneSubscription, "", plan)
	require.NoError(test, err)
	require.True(test, walletQuote.Payable)
	require.True(test, subscriptionQuote.Payable)
	provider := EpayPaymentProvider{}
	request := &types.TopupCheckoutRequest{UserID: user.Id, EpayChannelID: channel.Id, PaymentMethod: "wxpay", AmountCNY: "60"}
	quote, err := QuoteEpay(channel.Id, "wxpay", types.PaymentSceneTopup, request.AmountCNY, nil)
	require.NoError(test, err)
	request.QuoteToken = quote.Token
	_, err = provider.CreateTopupCheckout(request)
	require.NoError(test, err)
	request.AmountCNY, request.QuoteToken = "50", walletQuote.Token
	_, err = provider.CreateTopupCheckout(request)
	require.ErrorContains(test, err, "今日剩余额度不足")
	subscriptionRequest := &types.SubscriptionCheckoutRequest{
		UserID: user.Id, PlanID: plan.Id, EpayChannelID: channel.Id, PaymentMethod: "wxpay", QuoteToken: subscriptionQuote.Token,
	}
	_, err = provider.CreateSubscriptionCheckout(plan, subscriptionRequest)
	require.ErrorContains(test, err, "今日剩余额度不足")
	request.AmountCNY = "40"
	quote, err = QuoteEpay(channel.Id, "wxpay", types.PaymentSceneTopup, request.AmountCNY, nil)
	require.NoError(test, err)
	request.QuoteToken = quote.Token
	_, err = provider.CreateTopupCheckout(request)
	require.NoError(test, err)
	quote, err = QuoteEpay(channel.Id, "alipay", types.PaymentSceneSubscription, "", plan)
	require.NoError(test, err)
	subscriptionRequest.PaymentMethod, subscriptionRequest.QuoteToken = "alipay", quote.Token
	_, err = provider.CreateSubscriptionCheckout(plan, subscriptionRequest)
	require.NoError(test, err)
	remaining, err := QuoteEpay(channel.Id, "wxpay", types.PaymentSceneTopup, "1", nil)
	require.NoError(test, err)
	require.False(test, remaining.Payable)
	require.Equal(test, "0.00", remaining.DailyRemainingCNY)
	var walletCount, subscriptionCount int64
	require.NoError(test, model.DB.Model(&model.TopUp{}).Where("epay_channel_id = ?", channel.Id).Count(&walletCount).Error)
	require.NoError(test, model.DB.Model(&model.SubscriptionOrder{}).Where("epay_channel_id = ?", channel.Id).Count(&subscriptionCount).Error)
	require.EqualValues(test, 2, walletCount)
	require.EqualValues(test, 1, subscriptionCount)
}

func TestEpaySubscriptionIndependentDiscount(test *testing.T) {
	setupEpayQuoteTest(test)
	operation_setting.USDExchangeRate = 7.3
	revision := epayTestRevision()
	revision.TopupOfferType, revision.TopupOfferPercent = "bonus", "200"
	revision.SubscriptionDiscountPercent, revision.SubscriptionOfferThresholdCNY = "90", "73"
	plan := &model.SubscriptionPlan{Id: 7, PriceAmount: 10, Currency: "USD", Enabled: true}
	quote, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneSubscription, "999", plan)
	require.NoError(test, err)
	require.Equal(test, "73.00", quote.InputCNY)
	require.Equal(test, "65.70", quote.PayCNY)
	require.Equal(test, "discount", quote.OfferType)
	require.Equal(test, "0", quote.BonusUSD)
	require.Zero(test, quote.CreditQuota)
	revision.TopupOfferType, revision.TopupOfferPercent = "discount", "1"
	second, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneSubscription, "", plan)
	require.NoError(test, err)
	require.Equal(test, quote.Token, second.Token)
	revision.SubscriptionOfferThresholdCNY = "73.01"
	quote, err = CalculateEpayQuote(revision, "wxpay", types.PaymentSceneSubscription, "", plan)
	require.NoError(test, err)
	require.Equal(test, "73.00", quote.PayCNY)
	require.False(test, quote.OfferApplied)
	revision.SubscriptionDiscountPercent = "100"
	revision.SubscriptionOfferThresholdCNY = "0"
	quote, err = CalculateEpayQuote(revision, "wxpay", types.PaymentSceneSubscription, "", plan)
	require.NoError(test, err)
	require.False(test, quote.OfferApplied)
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		operation_setting.USDExchangeRate = rate
		_, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneSubscription, "", plan)
		require.Error(test, err)
	}
}

func TestEpayQuoteInvalidAmountsAndSafeRounding(test *testing.T) {
	setupEpayQuoteTest(test)
	revision := epayTestRevision()
	for _, amount := range []string{"", "0", "-1", "0.001", "NaN", "Inf", "1e10", "100000001", "18446744073686646784"} {
		_, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, amount, nil)
		require.Error(test, err, amount)
	}
	_, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, "100000000", nil)
	require.ErrorContains(test, err, "安全范围")
	operation_setting.USDExchangeRate = 7.3
	revision.TopupOfferType, revision.TopupOfferPercent = "bonus", "102"
	quote, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, "1", nil)
	require.NoError(test, err)
	for _, quotaAndUSD := range []struct {
		quota int
		usd   string
	}{{quote.BaseQuota, quote.BaseUSD}, {quote.BonusQuota, quote.BonusUSD}, {quote.CreditQuota, quote.CreditUSD}} {
		usd, err := decimal.NewFromString(quotaAndUSD.usd)
		require.NoError(test, err)
		require.True(test, usd.Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Equal(decimal.NewFromInt(int64(quotaAndUSD.quota))))
	}
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64} {
		operation_setting.USDExchangeRate = rate
		_, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, "100", nil)
		require.Error(test, err)
	}
	operation_setting.USDExchangeRate = 5
	for _, unit := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		common.QuotaPerUnit = unit
		_, err := CalculateEpayQuote(revision, "wxpay", types.PaymentSceneTopup, "100", nil)
		require.Error(test, err)
	}
}

func TestEpayChannelValidation(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*EpayChannelInput)
	}{
		{"empty name", func(input *EpayChannelInput) { input.Name = "" }},
		{"missing key", func(input *EpayChannelInput) { input.MerchantKey = "" }},
		{"unsupported URL", func(input *EpayChannelInput) { input.PayAddress = "file:///etc/passwd" }},
		{"URL credentials", func(input *EpayChannelInput) { input.PayAddress = "https://user:password@pay.example.com" }},
		{"callback query", func(input *EpayChannelInput) { input.CallbackAddress = "https://site.example.com?x=1" }},
		{"missing methods", func(input *EpayChannelInput) { input.Methods = nil }},
		{"duplicate methods", func(input *EpayChannelInput) { input.Methods = []string{"alipay", "alipay"} }},
		{"unsupported method", func(input *EpayChannelInput) { input.Methods = []string{"stripe"} }},
		{"negative minimum", func(input *EpayChannelInput) {
			input.MethodLimits["wxpay"] = types.EpayMethodLimits{MinPayCNY: "-1", MaxPayCNY: "5000", DailyLimitCNY: "0"}
		}},
		{"reversed limits", func(input *EpayChannelInput) {
			input.MethodLimits["wxpay"] = types.EpayMethodLimits{MinPayCNY: "100", MaxPayCNY: "99", DailyLimitCNY: "0"}
		}},
		{"missing method limits", func(input *EpayChannelInput) { delete(input.MethodLimits, "wxpay") }},
		{"invalid daily limit", func(input *EpayChannelInput) {
			input.MethodLimits["wxpay"] = types.EpayMethodLimits{MinPayCNY: "1", MaxPayCNY: "5000", DailyLimitCNY: "-1"}
		}},
		{"subcent daily limit", func(input *EpayChannelInput) {
			input.MethodLimits["wxpay"] = types.EpayMethodLimits{MinPayCNY: "1", MaxPayCNY: "5000", DailyLimitCNY: "1.001"}
		}},
		{"unsupported method limits", func(input *EpayChannelInput) { input.MethodLimits["stripe"] = input.MethodLimits["wxpay"] }},
		{"bonus equals 100", func(input *EpayChannelInput) { input.TopupOfferType, input.TopupOfferPercent = "bonus", "100" }},
		{"discount equals 100", func(input *EpayChannelInput) { input.TopupOfferType, input.TopupOfferPercent = "discount", "100" }},
		{"zero discount", func(input *EpayChannelInput) { input.TopupOfferType, input.TopupOfferPercent = "discount", "0" }},
		{"nonfinite bonus", func(input *EpayChannelInput) { input.TopupOfferType, input.TopupOfferPercent = "bonus", "NaN" }},
		{"subscription bonus", func(input *EpayChannelInput) { input.SubscriptionDiscountPercent = "102" }},
		{"negative threshold", func(input *EpayChannelInput) { input.SubscriptionOfferThresholdCNY = "-1" }},
		{"subcent threshold", func(input *EpayChannelInput) {
			input.TopupOfferType, input.TopupOfferThresholdCNY = "bonus", "0.001"
			input.TopupOfferPercent = "102"
		}},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			input := epayTestInput()
			scenario.change(&input)
			require.Error(test, SaveEpayChannel(0, &input))
		})
	}
}

func TestEpayMoneyMatchesExactly(test *testing.T) {
	for _, amount := range []string{"12.34", "12.3400", " 12.34 "} {
		require.True(test, EpayMoneyMatches(amount, 12.34))
	}
	for _, amount := range []string{"12.339", "12.344", "12.35", "-12.34", "NaN", "1e2", "0"} {
		require.False(test, EpayMoneyMatches(amount, 12.34), amount)
	}
	for _, expected := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		require.False(test, EpayMoneyMatches("12.34", expected))
	}
}

func TestEpayChannelRevisionAndCheckoutSnapshot(test *testing.T) {
	setupEpayQuoteTest(test)
	require.NoError(test, model.DB.AutoMigrate(&model.TopUp{}, &model.SubscriptionOrder{}))
	input := epayTestInput()
	input.Name = "snapshot channel"
	input.Methods = []string{"wxpay"}
	input.TopupOfferType, input.TopupOfferPercent, input.TopupOfferThresholdCNY = "bonus", "102", "100"
	require.NoError(test, SaveEpayChannel(0, &input))
	var channel model.EpayChannel
	require.NoError(test, model.DB.Order("id desc").First(&channel).Error)
	user := model.User{Username: "epay_snapshot_user", AffCode: "epay_snapshot_user"}
	require.NoError(test, model.DB.Create(&user).Error)
	test.Cleanup(func() {
		model.DB.Where("user_id = ?", user.Id).Delete(&model.TopUp{})
		model.DB.Where("user_id = ?", user.Id).Delete(&model.SubscriptionOrder{})
		model.DB.Unscoped().Delete(&user)
		model.DB.Where("channel_id = ?", channel.Id).Delete(&model.EpayChannelRevision{})
		model.DB.Delete(&channel)
	})
	revision, err := GetEnabledEpayRevision(channel.Id, "wxpay")
	require.NoError(test, err)
	_, err = GetEnabledEpayRevision(channel.Id, "alipay")
	require.ErrorContains(test, err, "不支持")
	quote, err := QuoteEpay(channel.Id, "wxpay", types.PaymentSceneTopup, "100", nil)
	require.NoError(test, err)
	request := &types.TopupCheckoutRequest{UserID: user.Id, EpayChannelID: channel.Id, PaymentMethod: "wxpay", AmountCNY: "100", QuoteToken: quote.Token}
	provider := EpayPaymentProvider{}
	request.QuoteToken = "tampered"
	_, err = provider.CreateTopupCheckout(request)
	require.ErrorContains(test, err, "报价")
	request.QuoteToken = quote.Token
	result, err := provider.CreateTopupCheckout(request)
	require.NoError(test, err)
	require.Equal(test, "https://pay.example.com/submit.php", result.Form.URL)
	require.Equal(test, "merchant-1", result.Form.Fields["pid"])
	require.Equal(test, "100.00", result.Form.Fields["money"])
	require.Equal(test, "https://callback.example.com/api/user/epay/notify", result.Form.Fields["notify_url"])
	var topup model.TopUp
	require.NoError(test, model.DB.Where("trade_no = ?", result.Form.Fields["out_trade_no"]).First(&topup).Error)
	require.Equal(test, 10200000, topup.CreditQuota)
	require.Equal(test, "20.4", topup.EpayCreditUSD)
	require.Equal(test, revision.Id, topup.EpayChannelRevisionId)
	operation_setting.USDExchangeRate = 6
	_, err = provider.CreateTopupCheckout(request)
	require.ErrorContains(test, err, "报价")
	operation_setting.USDExchangeRate = 5
	input.Methods = []string{"wxpay", "alipay"}
	input.RevisionID, input.MerchantKey, input.TopupOfferPercent = revision.Id, "", "103"
	require.NoError(test, SaveEpayChannel(channel.Id, &input))
	updated, err := GetEnabledEpayRevision(channel.Id, "alipay")
	require.NoError(test, err)
	require.NotEqual(test, revision.Id, updated.Id)
	require.Equal(test, revision.MerchantKey, updated.MerchantKey)
	_, err = provider.CreateTopupCheckout(request)
	require.ErrorContains(test, err, "报价")
	require.ErrorContains(test, SaveEpayChannel(channel.Id, &input), "已更新")
	views, err := ListAdminEpayChannels()
	require.NoError(test, err)
	encoded, err := common.Marshal(views)
	require.NoError(test, err)
	require.NotContains(test, string(encoded), revision.MerchantKey)
	public, err := ListPublicEpayChannels(nil)
	require.NoError(test, err)
	encoded, err = common.Marshal(public)
	require.NoError(test, err)
	for _, private := range []string{"merchant_id", "merchant_key", "pay_address", "callback_address"} {
		require.NotContains(test, string(encoded), private)
	}
	input.RevisionID, input.Enabled, input.MerchantKey = updated.Id, false, "rotated-key"
	require.NoError(test, SaveEpayChannel(channel.Id, &input))
	_, err = GetEnabledEpayRevision(channel.Id, "wxpay")
	require.Error(test, err)
	frozen, err := model.GetEpayChannelRevision(topup.EpayChannelRevisionId)
	require.NoError(test, err)
	require.Equal(test, "102", frozen.TopupOfferPercent)
	require.Equal(test, revision.MerchantKey, frozen.MerchantKey)
	require.Equal(test, quote.CreditQuota, topup.CreditQuota)
}
