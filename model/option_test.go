package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionMapIgnoresRemovedTopupGroupRatio(test *testing.T) {
	originalOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	test.Cleanup(func() { common.OptionMap = originalOptions })

	for _, value := range []string{`{"default":0.5,"vip":2}`, `invalid json`, ""} {
		require.NoError(test, updateOptionMap("TopupGroupRatio", value))
		require.NotContains(test, common.OptionMap, "TopupGroupRatio")
	}
}

func TestUpdateOptionRejectsRemovedTopupGroupRatio(test *testing.T) {
	require.NoError(test, DB.AutoMigrate(&Option{}))
	original := Option{Key: "TopupGroupRatio", Value: `{"vip":0.5}`}
	require.NoError(test, DB.Save(&original).Error)
	test.Cleanup(func() {
		DB.Where(map[string]interface{}{"key": original.Key}).Delete(&Option{})
	})

	require.ErrorContains(test, UpdateOption(original.Key, `{"vip":2}`), "配置已移除")

	var saved Option
	require.NoError(test, DB.First(&saved, Option{Key: original.Key}).Error)
	require.Equal(test, original.Value, saved.Value)
}

func TestUpdateOptionRejectsAllRetiredEpayFields(test *testing.T) {
	for _, key := range []string{"PayAddress", "EpayId", "EpayKey", "CustomCallbackAddress", "PayMethods", "Price", "MinTopUp", "epay_setting.balance_recharge_ratio"} {
		require.ErrorContains(test, UpdateOption(key, "old value"), "配置已移除")
		require.NoError(test, updateOptionMap(key, "old value"))
	}
}

func TestInvalidEpayOptionsNeverPersist(test *testing.T) {
	require.NoError(test, DB.AutoMigrate(&Option{}))
	originalOptions := common.OptionMap
	originalSettings := *operation_setting.GetEpaySetting()
	originalRate := operation_setting.USDExchangeRate
	common.OptionMap = make(map[string]string)
	test.Cleanup(func() {
		common.OptionMap = originalOptions
		*operation_setting.GetEpaySetting() = originalSettings
		operation_setting.USDExchangeRate = originalRate
	})
	for _, scenario := range []struct{ key, valid, invalid string }{
		{"USDExchangeRate", "7.3", "NaN"},
		{"epay_setting.amount_options", "[10,100]", "[0]"},
	} {
		require.NoError(test, UpdateOption(scenario.key, scenario.valid))
		require.Error(test, UpdateOption(scenario.key, scenario.invalid))
		var persisted Option
		require.NoError(test, DB.First(&persisted, Option{Key: scenario.key}).Error)
		require.Equal(test, scenario.valid, persisted.Value)
		require.Equal(test, scenario.valid, common.OptionMap[scenario.key])
		require.NoError(test, DB.Delete(&Option{Key: scenario.key}).Error)
	}
}
