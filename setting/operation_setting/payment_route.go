package operation_setting

import (
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
)

type PaymentRouteSetting struct {
	TopupProvider        types.PaymentProvider `json:"topup_provider"`
	SubscriptionProvider types.PaymentProvider `json:"subscription_provider"`
}

var paymentRouteSetting = PaymentRouteSetting{
	TopupProvider:        types.PaymentProviderLegacyAuto,
	SubscriptionProvider: types.PaymentProviderLegacyAuto,
}

func init() {
	config.GlobalConfig.Register("payment_route", &paymentRouteSetting)
}

func GetPaymentRouteSetting() *PaymentRouteSetting {
	return &paymentRouteSetting
}

func GetTopupPaymentProvider() types.PaymentProvider {
	return paymentRouteSetting.TopupProvider
}

func GetSubscriptionPaymentProvider() types.PaymentProvider {
	return paymentRouteSetting.SubscriptionProvider
}
