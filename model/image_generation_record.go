package model

import (
	"encoding/json"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	ImageRecordStatusInProgress = "IN_PROGRESS"
	ImageRecordStatusSuccess    = "SUCCESS"
	ImageRecordStatusFailure    = "FAILURE"
)

type ImageGenerationRecord struct {
	ID             int64           `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	CreatedAt      int64           `json:"created_at" gorm:"index"`
	UpdatedAt      int64           `json:"updated_at"`
	FinishedAt     int64           `json:"finished_at" gorm:"index"`
	RecordID       string          `json:"record_id" gorm:"type:varchar(191);uniqueIndex"`
	RequestID      string          `json:"request_id" gorm:"type:varchar(191);index"`
	UserID         int             `json:"user_id" gorm:"index"`
	TokenID        int             `json:"token_id" gorm:"index"`
	ChannelID      int             `json:"channel_id" gorm:"index"`
	Action         string          `json:"action" gorm:"type:varchar(32);index"`
	ModelName      string          `json:"model_name" gorm:"type:varchar(191);index"`
	Status         string          `json:"status" gorm:"type:varchar(32);index"`
	Prompt         string          `json:"prompt" gorm:"type:text"`
	Size           string          `json:"size" gorm:"type:varchar(64)"`
	Quality        string          `json:"quality" gorm:"type:varchar(64)"`
	ResponseFormat string          `json:"response_format" gorm:"type:varchar(64)"`
	FailReason     string          `json:"fail_reason" gorm:"type:text"`
	Result         json.RawMessage `json:"result" gorm:"type:json"`
}

func (r *ImageGenerationRecord) BeforeCreate(_ *gorm.DB) error {
	now := time.Now().Unix()
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	if r.UpdatedAt == 0 {
		r.UpdatedAt = now
	}
	return nil
}

func (r *ImageGenerationRecord) BeforeUpdate(_ *gorm.DB) error {
	r.UpdatedAt = time.Now().Unix()
	return nil
}

func CreateImageGenerationRecord(record *ImageGenerationRecord) error {
	return DB.Create(record).Error
}

func GetImageGenerationRecordByRecordID(recordID string) (*ImageGenerationRecord, bool, error) {
	if recordID == "" {
		return nil, false, nil
	}
	var record ImageGenerationRecord
	err := DB.Where("record_id = ?", recordID).First(&record).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	if !exist {
		return nil, false, nil
	}
	return &record, true, nil
}

func GetUserImageGenerationRecord(userID int, recordID string) (*ImageGenerationRecord, bool, error) {
	if recordID == "" {
		return nil, false, nil
	}
	var record ImageGenerationRecord
	err := DB.Where("user_id = ? AND record_id = ?", userID, recordID).First(&record).Error
	exist, err := RecordExist(err)
	if err != nil {
		return nil, false, err
	}
	if !exist {
		return nil, false, nil
	}
	return &record, true, nil
}

func GetUserImageGenerationRecords(userID int, startIdx int, num int) ([]*ImageGenerationRecord, int64, error) {
	var records []*ImageGenerationRecord
	var total int64
	query := DB.Model(&ImageGenerationRecord{}).Where("user_id = ?", userID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Limit(num).Offset(startIdx).Find(&records).Error
	return records, total, err
}

func UpdateImageGenerationRecordStatus(recordID string, status string, result json.RawMessage, failReason string) error {
	if recordID == "" {
		return nil
	}
	updates := map[string]any{
		"status":      status,
		"fail_reason": failReason,
		"updated_at":  time.Now().Unix(),
	}
	if status == ImageRecordStatusSuccess || status == ImageRecordStatusFailure {
		updates["finished_at"] = time.Now().Unix()
	}
	if result != nil {
		updates["result"] = result
	}
	return DB.Model(&ImageGenerationRecord{}).Where("record_id = ?", recordID).Updates(updates).Error
}

func DeleteUserImageGenerationRecord(userID int, recordID string) error {
	if userID <= 0 || recordID == "" {
		return nil
	}
	return DB.Where("user_id = ? AND record_id = ?", userID, recordID).Delete(&ImageGenerationRecord{}).Error
}

func MarshalImageRecordResult(v any) json.RawMessage {
	data, err := common.Marshal(v)
	if err != nil {
		return nil
	}
	return json.RawMessage(data)
}
