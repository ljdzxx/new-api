package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateUSDExchangeRate(test *testing.T) {
	for _, value := range []string{"1", "7.3", "0.14", "0.000000000001", "1000000", "1e-12"} {
		require.NoError(test, ValidatePaymentOption("USDExchangeRate", value), value)
	}
	for _, value := range []string{"", "0", "-1", "NaN", "+Inf", "-Inf", "1e1000", "1e-1000"} {
		require.Error(test, ValidatePaymentOption("USDExchangeRate", value), value)
	}
}

func TestValidatePaymentOptions(test *testing.T) {
	for _, value := range []string{"[1,10,100]", "[100000000]"} {
		require.NoError(test, ValidatePaymentOption("epay_setting.amount_options", value))
	}
	for _, value := range []string{"null", "[]", "{}", "[1,1]", "[0]", "[-1]", "[0.1]", "[100000001]", "[18446744073686646784]"} {
		require.Error(test, ValidatePaymentOption("epay_setting.amount_options", value), value)
	}
	for _, value := range []string{`{}`, `{"100":0.98,"10":1}`} {
		require.NoError(test, ValidatePaymentOption("payment_setting.amount_discount", value))
	}
	for _, value := range []string{`null`, `[]`, `{"0":0.98}`, `{"100":0}`, `{"100":1.01}`, `{"100":-1}`, `{"100":"NaN"}`} {
		require.Error(test, ValidatePaymentOption("payment_setting.amount_discount", value), value)
	}
}
