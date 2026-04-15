package operation_setting

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type MonitorSetting struct {
	AutoTestChannelEnabled          bool     `json:"auto_test_channel_enabled"`
	AutoTestChannelMinutes          float64  `json:"auto_test_channel_minutes"`
	GlobalQuotaInsufficientKeywords []string `json:"global_quota_insufficient_keywords"`
}

var defaultGlobalQuotaInsufficientKeywords = []string{
	"insufficient",
	"insufficient_quota",
	"insufficient_user_quota",
	"额度不足",
	"余额不足",
}

// 默认配置
var monitorSetting = MonitorSetting{
	AutoTestChannelEnabled:          false,
	AutoTestChannelMinutes:          10,
	GlobalQuotaInsufficientKeywords: append([]string(nil), defaultGlobalQuotaInsufficientKeywords...),
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("monitor_setting", &monitorSetting)
}

func GetMonitorSetting() *MonitorSetting {
	if os.Getenv("CHANNEL_TEST_FREQUENCY") != "" {
		frequency, err := strconv.Atoi(os.Getenv("CHANNEL_TEST_FREQUENCY"))
		if err == nil && frequency > 0 {
			monitorSetting.AutoTestChannelEnabled = true
			monitorSetting.AutoTestChannelMinutes = float64(frequency)
		}
	}
	return &monitorSetting
}

func ValidateGlobalQuotaInsufficientKeywords(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("全局余额不足关键词必须为 JSON 数组")
	}
	var keywords []string
	if err := json.Unmarshal([]byte(raw), &keywords); err != nil {
		return fmt.Errorf("全局余额不足关键词必须为字符串 JSON 数组: %w", err)
	}
	for _, keyword := range keywords {
		if strings.TrimSpace(keyword) == "" {
			return fmt.Errorf("全局余额不足关键词不能包含空字符串")
		}
	}
	return nil
}

func GetGlobalQuotaInsufficientKeywords() []string {
	cleaned := normalizeQuotaKeywords(monitorSetting.GlobalQuotaInsufficientKeywords)
	if len(cleaned) == 0 {
		return append([]string(nil), defaultGlobalQuotaInsufficientKeywords...)
	}
	return cleaned
}

func GlobalQuotaInsufficientKeywordsToJSONString() string {
	keywords := GetGlobalQuotaInsufficientKeywords()
	raw, err := json.Marshal(keywords)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func normalizeQuotaKeywords(keywords []string) []string {
	cleaned := make([]string, 0, len(keywords))
	seen := make(map[string]struct{}, len(keywords))
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		lowerKeyword := strings.ToLower(keyword)
		if _, ok := seen[lowerKeyword]; ok {
			continue
		}
		seen[lowerKeyword] = struct{}{}
		cleaned = append(cleaned, keyword)
	}
	return cleaned
}
