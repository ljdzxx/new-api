package image_storage_setting

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

type ImageStorageSetting struct {
	R2Enabled         bool   `json:"r2_enabled"`
	R2AccountID       string `json:"r2_account_id"`
	R2Bucket          string `json:"r2_bucket"`
	R2Endpoint        string `json:"r2_endpoint"`
	R2AccessKeyID     string `json:"r2_access_key_id"`
	R2SecretAccessKey string `json:"r2_secret"`
	R2ObjectPrefix    string `json:"r2_object_prefix"`
	R2URLExpireHours  int    `json:"r2_url_expire_hours"`
}

var imageStorageSetting = ImageStorageSetting{
	R2Enabled:        false,
	R2ObjectPrefix:   "generated-images/",
	R2URLExpireHours: 24,
}

func init() {
	config.GlobalConfig.Register("image_storage_setting", &imageStorageSetting)
}

func GetImageStorageSetting() *ImageStorageSetting {
	return &imageStorageSetting
}

func (s *ImageStorageSetting) Endpoint() string {
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

func (s *ImageStorageSetting) URLExpireDuration() time.Duration {
	hours := s.R2URLExpireHours
	if hours <= 0 {
		hours = 24
	}
	return time.Duration(hours) * time.Hour
}

func (s *ImageStorageSetting) ObjectPrefix() string {
	prefix := strings.TrimSpace(s.R2ObjectPrefix)
	if prefix == "" {
		return "generated-images/"
	}
	return strings.TrimLeft(prefix, "/")
}
