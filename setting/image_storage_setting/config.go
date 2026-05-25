package image_storage_setting

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

type ImageStorageSetting struct {
	R2Enabled                             bool   `json:"r2_enabled"`
	R2AccountID                           string `json:"r2_account_id"`
	R2Bucket                              string `json:"r2_bucket"`
	R2Endpoint                            string `json:"r2_endpoint"`
	R2AccessKeyID                         string `json:"r2_access_key_id"`
	R2SecretAccessKey                     string `json:"r2_secret"`
	R2ObjectPrefix                        string `json:"r2_object_prefix"`
	R2URLExpireHours                      int    `json:"r2_url_expire_hours"`
	ImageEditsBaseURL                     string `json:"image_edits_base_url"`
	EditReferenceImageCompressionEnabled  bool   `json:"edit_reference_image_compression_enabled"`
	EditReferenceImageCompressThresholdMB int    `json:"edit_reference_image_compress_threshold_mb"`
	EditReferenceImageTargetSizeMB        int    `json:"edit_reference_image_target_size_mb"`
	EditReferenceImageMaxSide             int    `json:"edit_reference_image_max_side"`
	EditReferenceImageMinJPEGQuality      int    `json:"edit_reference_image_min_jpeg_quality"`
}

var imageStorageSetting = ImageStorageSetting{
	R2Enabled:                             false,
	R2ObjectPrefix:                        "generated-images/",
	R2URLExpireHours:                      24,
	EditReferenceImageCompressionEnabled:  true,
	EditReferenceImageCompressThresholdMB: 8,
	EditReferenceImageTargetSizeMB:        8,
	EditReferenceImageMaxSide:             2048,
	EditReferenceImageMinJPEGQuality:      65,
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

func (s *ImageStorageSetting) EditReferenceImageCompressThresholdBytes() int64 {
	mb := s.EditReferenceImageCompressThresholdMB
	if mb <= 0 {
		mb = 8
	}
	return int64(mb) << 20
}

func (s *ImageStorageSetting) EditReferenceImageTargetBytes() int64 {
	mb := s.EditReferenceImageTargetSizeMB
	if mb <= 0 {
		mb = 8
	}
	return int64(mb) << 20
}

func (s *ImageStorageSetting) EditReferenceImageMaxSideValue() int {
	if s.EditReferenceImageMaxSide <= 0 {
		return 2048
	}
	return s.EditReferenceImageMaxSide
}

func (s *ImageStorageSetting) EditReferenceImageMinJPEGQualityValue() int {
	quality := s.EditReferenceImageMinJPEGQuality
	if quality <= 0 {
		return 65
	}
	if quality > 92 {
		return 92
	}
	return quality
}
