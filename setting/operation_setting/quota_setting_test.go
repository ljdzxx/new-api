package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestPreConsumeMinBalanceQuotaUsesDisplayedCurrency(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDisplayType := generalSetting.QuotaDisplayType
	originalUSDExchangeRate := USDExchangeRate
	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		generalSetting.QuotaDisplayType = originalDisplayType
		USDExchangeRate = originalUSDExchangeRate
	})

	common.QuotaPerUnit = 500_000
	generalSetting.QuotaDisplayType = QuotaDisplayTypeUSD
	setting := QuotaSetting{
		EnablePreConsumeMinBalance: true,
		PreConsumeMinBalance:       2.5,
	}

	quota, err := setting.PreConsumeMinBalanceQuota()
	require.NoError(t, err)
	require.Equal(t, 1_250_000, quota)

	generalSetting.QuotaDisplayType = QuotaDisplayTypeCNY
	USDExchangeRate = 5
	quota, err = setting.PreConsumeMinBalanceQuota()
	require.NoError(t, err)
	require.Equal(t, 250_000, quota)

	// The comparison is strict. A fractional quota threshold must be truncated
	// so a one-unit balance is accepted when it is genuinely above the amount.
	setting.PreConsumeMinBalance = 0.000001
	generalSetting.QuotaDisplayType = QuotaDisplayTypeUSD
	quota, err = setting.PreConsumeMinBalanceQuota()
	require.NoError(t, err)
	require.Equal(t, 0, quota)
}

func TestPreConsumeMinBalanceQuotaRejectsInvalidAmount(t *testing.T) {
	setting := QuotaSetting{
		EnablePreConsumeMinBalance: true,
		PreConsumeMinBalance:       -1,
	}
	_, err := setting.PreConsumeMinBalanceQuota()
	require.Error(t, err)
}
