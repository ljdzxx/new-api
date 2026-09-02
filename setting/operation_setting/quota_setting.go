package operation_setting

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/shopspring/decimal"
)

type QuotaSetting struct {
	EnableFreeModelPreConsume bool `json:"enable_free_model_pre_consume"` // 是否对免费模型启用预消耗
	// EnablePreConsumeMinBalance enables the minimum-balance pre-consume policy.
	// When enabled, wallet requests only require the user's balance to be above
	// PreConsumeMinBalance and do not pre-deduct the formula-calculated quota.
	EnablePreConsumeMinBalance bool `json:"enable_pre_consume_min_balance"`
	// PreConsumeMinBalance is expressed in the site's displayed currency (not
	// tokens). It is converted to quota units only for the balance comparison.
	PreConsumeMinBalance float64 `json:"pre_consume_min_balance"`
}

// 默认配置
var quotaSetting = QuotaSetting{
	EnableFreeModelPreConsume:  true,
	EnablePreConsumeMinBalance: false,
	PreConsumeMinBalance:       0,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("quota_setting", &quotaSetting)
}

func GetQuotaSetting() *QuotaSetting {
	return &quotaSetting
}

// PreConsumeMinBalanceQuota converts the configured monetary threshold to the
// internal quota unit used by user balances. The threshold follows the site's
// currency display setting; when the site displays USD, one unit is one USD.
// A strict conversion is used so an invalid or unrepresentable configuration
// can never silently turn into a permissive balance check.
func (s *QuotaSetting) PreConsumeMinBalanceQuota() (int, error) {
	if s == nil || !s.EnablePreConsumeMinBalance {
		return 0, nil
	}
	if math.IsNaN(s.PreConsumeMinBalance) || math.IsInf(s.PreConsumeMinBalance, 0) || s.PreConsumeMinBalance < 0 {
		return 0, fmt.Errorf("预扣最低余额必须是不小于 0 的有限金额")
	}
	if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
		return 0, fmt.Errorf("QuotaPerUnit 配置无效")
	}

	// QuotaPerUnit is the quota value of one USD. Convert from the site's
	// displayed currency back to USD before converting to quota units.
	currencyRate := GetUsdToCurrencyRate(USDExchangeRate)
	if currencyRate <= 0 || math.IsNaN(currencyRate) || math.IsInf(currencyRate, 0) {
		return 0, fmt.Errorf("货币汇率配置无效")
	}
	quota := decimal.NewFromFloat(s.PreConsumeMinBalance).
		Div(decimal.NewFromFloat(currencyRate)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	// The check is strictly "balance > threshold". Truncating the converted
	// threshold means an integer balance q passes exactly when q is greater than
	// the real monetary threshold (q > floor(threshold)).
	return common.QuotaFromFloatStrict(quota.InexactFloat64())
}
