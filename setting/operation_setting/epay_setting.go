package operation_setting

import (
	"fmt"
	"math"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type EpaySetting struct {
	AmountOptions []int `json:"amount_options"`
}

var epaySetting = EpaySetting{
	AmountOptions: []int{10, 20, 50, 100, 200, 500, 1000, 2000, 5000},
}

func init() {
	config.GlobalConfig.Register("epay_setting", &epaySetting)
}

func GetEpaySetting() *EpaySetting {
	return &epaySetting
}

func ValidatePaymentOption(key, value string) error {
	switch key {
	case "USDExchangeRate":
		rate, err := strconv.ParseFloat(value, 64)
		if err != nil || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return fmt.Errorf("系统美元汇率必须是大于 0 的有限数值")
		}
	case "epay_setting.amount_options", "payment_setting.amount_options":
		var amounts []int
		if err := common.UnmarshalJsonStr(value, &amounts); err != nil || len(amounts) == 0 || len(amounts) > 50 {
			return fmt.Errorf("充值金额选项必须是包含 1 至 50 个金额的正整数数组")
		}
		seen := make(map[int]bool, len(amounts))
		for _, amount := range amounts {
			if amount <= 0 || amount > 100000000 || seen[amount] {
				return fmt.Errorf("充值金额选项必须是互不重复且不超过 100000000 的正整数")
			}
			seen[amount] = true
		}
	case "payment_setting.amount_discount":
		var discounts map[int]float64
		if err := common.UnmarshalJsonStr(value, &discounts); err != nil || discounts == nil || len(discounts) > 100 {
			return fmt.Errorf("Stripe 充值金额折扣报价必须是合法 JSON 对象")
		}
		for amount, discount := range discounts {
			if amount <= 0 || amount > 100000000 || !(discount > 0 && discount <= 1) {
				return fmt.Errorf("Stripe 折扣报价的金额必须为正整数，折扣系数须大于 0 且不大于 1")
			}
		}
	}
	return nil
}
