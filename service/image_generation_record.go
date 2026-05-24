package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
)

const ImageRecordIDHeader = "X-Newapi-Image-Record-Id"

const imageRecordDeleteTimeout = 30 * time.Second

func InitImageGenerationRecord(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ImageRequest) {
	if c == nil || info == nil || request == nil {
		return
	}
	recordID := strings.TrimSpace(c.GetHeader(ImageRecordIDHeader))
	if recordID == "" {
		return
	}
	if _, exists, err := model.GetImageGenerationRecordByRecordID(recordID); err != nil {
		logger.LogError(c, fmt.Sprintf("[image record] get record failed: %s", err.Error()))
		return
	} else if exists {
		c.Set(ImageRecordIDHeader, recordID)
		return
	}

	action := "generations"
	if info.RelayMode == relayconstant.RelayModeImagesEdits {
		action = "edits"
	}
	record := &model.ImageGenerationRecord{
		RecordID:       recordID,
		RequestID:      info.RequestId,
		UserID:         info.UserId,
		TokenID:        info.TokenId,
		ChannelID:      info.ChannelId,
		Action:         action,
		ModelName:      request.Model,
		Status:         model.ImageRecordStatusInProgress,
		Prompt:         request.Prompt,
		Size:           request.Size,
		Quality:        request.Quality,
		ResponseFormat: request.ResponseFormat,
	}
	if record.Quality == "" {
		record.Quality = "auto"
	}
	if err := model.CreateImageGenerationRecord(record); err != nil {
		logger.LogError(c, fmt.Sprintf("[image record] create record failed: %s", err.Error()))
		return
	}
	c.Set(ImageRecordIDHeader, recordID)
	logger.LogInfo(c, fmt.Sprintf("[image record] created: record_id=%s user_id=%d action=%s", recordID, info.UserId, action))
}

func GetImageRecordID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if recordID := strings.TrimSpace(c.GetString(ImageRecordIDHeader)); recordID != "" {
		return recordID
	}
	return strings.TrimSpace(c.GetHeader(ImageRecordIDHeader))
}

func MarkImageRecordSuccess(c *gin.Context, responseBody []byte) {
	recordID := GetImageRecordID(c)
	if recordID == "" {
		return
	}
	var raw json.RawMessage
	if len(responseBody) > 0 {
		raw = json.RawMessage(append([]byte(nil), responseBody...))
	}
	if err := model.UpdateImageGenerationRecordStatus(recordID, model.ImageRecordStatusSuccess, raw, ""); err != nil {
		logger.LogError(c, fmt.Sprintf("[image record] mark success failed: record_id=%s err=%s", recordID, err.Error()))
	}
}

func MarkImageRecordFailure(c *gin.Context, err error) {
	recordID := GetImageRecordID(c)
	if recordID == "" {
		return
	}
	reason := "unknown error"
	if err != nil {
		reason = err.Error()
	}
	if err := model.UpdateImageGenerationRecordStatus(recordID, model.ImageRecordStatusFailure, nil, reason); err != nil {
		logger.LogError(c, fmt.Sprintf("[image record] mark failure failed: record_id=%s err=%s", recordID, err.Error()))
	}
}

func BuildImageRecordPublicResponse(record *model.ImageGenerationRecord) map[string]any {
	if record == nil {
		return nil
	}
	result := any(nil)
	if len(record.Result) > 0 {
		var parsed any
		if err := common.Unmarshal(record.Result, &parsed); err == nil {
			result = parsed
		}
	}
	return map[string]any{
		"record_id":       record.RecordID,
		"request_id":      record.RequestID,
		"action":          record.Action,
		"model":           record.ModelName,
		"status":          record.Status,
		"prompt":          record.Prompt,
		"size":            record.Size,
		"quality":         record.Quality,
		"response_format": record.ResponseFormat,
		"fail_reason":     record.FailReason,
		"created_at":      record.CreatedAt,
		"updated_at":      record.UpdatedAt,
		"finished_at":     record.FinishedAt,
		"result":          result,
	}
}

func DeleteUserImageGenerationRecord(c *gin.Context, userID int, recordID string) (int, error) {
	record, exists, err := model.GetUserImageGenerationRecord(userID, recordID)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, fmt.Errorf("image record not found")
	}

	deletedObjects := 0
	if len(record.Result) > 0 {
		deleteCtx, cancel := context.WithTimeout(context.Background(), imageRecordDeleteTimeout)
		defer cancel()
		deletedObjects, err = DeleteR2ImagesByResponseBody(deleteCtx, record.Result)
		if err != nil {
			logger.LogWarn(c, fmt.Sprintf("[image record] delete R2 image ignored: record_id=%s user_id=%d deleted=%d err=%s", recordID, userID, deletedObjects, err.Error()))
		}
	}
	if err := model.DeleteUserImageGenerationRecord(userID, recordID); err != nil {
		return deletedObjects, err
	}
	logger.LogInfo(c, fmt.Sprintf("[image record] deleted: record_id=%s user_id=%d r2_objects=%d", recordID, userID, deletedObjects))
	return deletedObjects, nil
}

func ImageRecordUserID(c *gin.Context) int {
	if c == nil {
		return 0
	}
	if userID := c.GetInt("id"); userID > 0 {
		return userID
	}
	return common.GetContextKeyInt(c, constant.ContextKeyUserId)
}
