package operation_setting

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

// InvoiceSetting 自助发票设置
type InvoiceSetting struct {
	// 单笔充值实付金额达到该值才可申请开票
	MinAmount float64 `json:"min_amount"`
	// 开票功能上线时间，仅在此时间之后完成的订单才能申请开票，格式：2006-01-02 15:04:05
	OnlineTime string `json:"online_time"`
	// 发票文件 Cloudflare R2 存储配置
	R2Enabled         bool   `json:"r2_enabled"`
	R2AccountID       string `json:"r2_account_id"`
	R2Bucket          string `json:"r2_bucket"`
	R2Endpoint        string `json:"r2_endpoint"`
	R2AccessKeyID     string `json:"r2_access_key_id"`
	R2SecretAccessKey string `json:"r2_secret"`
	R2ObjectPrefix    string `json:"r2_object_prefix"`
	R2URLExpireHours  int    `json:"r2_url_expire_hours"`
}

// 默认配置
var invoiceSetting = InvoiceSetting{
	MinAmount:        300,
	OnlineTime:       "2026-08-01 00:00:00",
	R2Enabled:        false,
	R2ObjectPrefix:   "invoices/",
	R2URLExpireHours: 24,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("invoice_setting", &invoiceSetting)
}

func GetInvoiceSetting() *InvoiceSetting {
	return &invoiceSetting
}

func (s *InvoiceSetting) Endpoint() string {
	endpoint := strings.TrimSpace(s.R2Endpoint)
	if endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}
	accountID := strings.TrimSpace(s.R2AccountID)
	if accountID == "" {
		return ""
	}
	return "https://" + accountID + ".r2.cloudflarestorage.com"
}

func (s *InvoiceSetting) URLExpireDuration() time.Duration {
	hours := s.R2URLExpireHours
	if hours <= 0 {
		hours = 24
	}
	return time.Duration(hours) * time.Hour
}

func (s *InvoiceSetting) ObjectPrefix() string {
	prefix := strings.TrimSpace(s.R2ObjectPrefix)
	if prefix == "" {
		return "invoices/"
	}
	return strings.TrimLeft(prefix, "/")
}

// OnlineTimestamp 返回开票上线时间戳（秒），配置为空或格式错误时返回 0（不限制）
func (s *InvoiceSetting) OnlineTimestamp() int64 {
	onlineTime := strings.TrimSpace(s.OnlineTime)
	if onlineTime == "" {
		return 0
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", onlineTime, time.Local)
	if err != nil {
		return 0
	}
	return t.Unix()
}
