package operation_setting

import "github.com/QuantumNous/new-api/setting/config"

const (
	PaymentDisplayCurrencyFollowQuota = "FOLLOW_QUOTA"
	PaymentDisplayCurrencyUSD         = "USD"
	PaymentDisplayCurrencyCNY         = "CNY"
	PaymentDisplayCurrencyCustom      = "CUSTOM"
)

type PaymentSetting struct {
	AmountOptions               []int           `json:"amount_options"`
	AmountDiscount              map[int]float64 `json:"amount_discount"`
	MallLinks                   map[int]string  `json:"mall_links"`
	DisplayCurrencyType         string          `json:"display_currency_type"`
	DisplayCurrencySymbol       string          `json:"display_currency_symbol"`
	DisplayCurrencyExchangeRate float64         `json:"display_currency_exchange_rate"`
}

var paymentSetting = PaymentSetting{
	AmountOptions:               []int{10, 20, 50, 100, 200, 500},
	AmountDiscount:              map[int]float64{},
	MallLinks:                   map[int]string{},
	DisplayCurrencyType:         PaymentDisplayCurrencyFollowQuota,
	DisplayCurrencySymbol:       "\u00A4",
	DisplayCurrencyExchangeRate: 1.0,
}

func init() {
	config.GlobalConfig.Register("payment_setting", &paymentSetting)
}

func GetPaymentSetting() *PaymentSetting {
	return &paymentSetting
}

func GetPaymentDisplayCurrencyType() string {
	if paymentSetting.DisplayCurrencyType == "" {
		return PaymentDisplayCurrencyFollowQuota
	}
	return paymentSetting.DisplayCurrencyType
}

func GetPaymentDisplayCurrencySymbol() string {
	switch GetPaymentDisplayCurrencyType() {
	case PaymentDisplayCurrencyUSD:
		return "$"
	case PaymentDisplayCurrencyCNY:
		return "\u00A5"
	case PaymentDisplayCurrencyCustom:
		if paymentSetting.DisplayCurrencySymbol != "" {
			return paymentSetting.DisplayCurrencySymbol
		}
		return "\u00A4"
	default:
		return GetCurrencySymbol()
	}
}

func GetUsdToPaymentCurrencyRate(usdToCny float64) float64 {
	switch GetPaymentDisplayCurrencyType() {
	case PaymentDisplayCurrencyUSD:
		return 1
	case PaymentDisplayCurrencyCNY:
		return usdToCny
	case PaymentDisplayCurrencyCustom:
		if paymentSetting.DisplayCurrencyExchangeRate > 0 {
			return paymentSetting.DisplayCurrencyExchangeRate
		}
		return 1
	default:
		return GetUsdToCurrencyRate(usdToCny)
	}
}
