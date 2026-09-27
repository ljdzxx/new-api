package operation_setting

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type ModelHealthSettings struct {
	Enabled     bool
	StatusCodes string
	Threshold   int
}

// Option updates and background sync hold this same lock while changing fields.
func GetModelHealthSettings() ModelHealthSettings {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return ModelHealthSettings{Enabled: monitorSetting.ModelHealthEnabled,
		StatusCodes: monitorSetting.ModelHealthStatusCodes, Threshold: monitorSetting.ModelHealthThreshold}
}

// Model health accepts explicit HTTP error codes, independently of retry rules.
func ParseModelHealthStatusCodes(raw string) (map[int]bool, error) {
	codes := make(map[int]bool)
	for _, part := range strings.Split(raw, ",") {
		code, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || code < 400 || code > 599 {
			return nil, fmt.Errorf("模型不可用状态码必须为逗号分隔的 400–599 整数")
		}
		codes[code] = true
	}
	return codes, nil
}

func ValidateModelHealthOption(key, value string) error {
	switch key {
	case "monitor_setting.model_health_enabled":
		if value != "true" && value != "false" {
			return fmt.Errorf("模型监控开关必须为 true 或 false")
		}
	case "monitor_setting.model_health_status_codes":
		_, err := ParseModelHealthStatusCodes(value)
		return err
	case "monitor_setting.model_health_threshold":
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 1000 {
			return fmt.Errorf("连续失败阈值必须为 1–1000 的整数")
		}
	}
	return nil
}
