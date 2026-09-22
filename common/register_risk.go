package common

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const DefaultRegisterRiskMessage = "当前注册环境操作过于频繁，请稍后重试"

type RegisterRiskConfig struct {
	Enabled       bool
	CooldownHours int
	HitThreshold  int
	RejectMessage string
}

// Updated under OptionMapRWMutex, like the other legacy operation settings.
var RegistrationRisk = RegisterRiskConfig{
	CooldownHours: 24,
	HitThreshold:  1,
	RejectMessage: DefaultRegisterRiskMessage,
}

func GetRegisterRiskConfig() RegisterRiskConfig {
	OptionMapRWMutex.RLock()
	defer OptionMapRWMutex.RUnlock()
	return RegistrationRisk
}

func ValidateRegisterRiskOption(key, value string) error {
	switch key {
	case "RegisterRiskControlEnabled":
		if value != "true" && value != "false" {
			return fmt.Errorf("注册风控开关必须是 true 或 false")
		}
	case "RegisterRiskCooldownHours", "RegisterRiskHitThreshold", "InviteRiskThreshold", "InviteRiskDailyLimit":
		min, max := 1, 720
		switch key {
		case "RegisterRiskHitThreshold":
			max = 100000
		case "InviteRiskThreshold":
			min, max = 0, 100
		case "InviteRiskDailyLimit":
			min, max = 0, 2147483647
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < min || n > max {
			return fmt.Errorf("%s 必须是 %d 到 %d 之间的整数", key, min, max)
		}
	case "RegisterRiskRejectMessage":
		if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 200 {
			return fmt.Errorf("注册失败提示词必须为 1 到 200 个字符")
		}
	}
	return nil
}
