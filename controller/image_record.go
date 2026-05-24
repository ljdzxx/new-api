package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func GetUserImageRecord(c *gin.Context) {
	userID := service.ImageRecordUserID(c)
	recordID := c.Param("record_id")
	record, exists, err := model.GetUserImageGenerationRecord(userID, recordID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !exists {
		common.ApiError(c, fmt.Errorf("image record not found"))
		return
	}
	common.ApiSuccess(c, service.BuildImageRecordPublicResponse(record))
}

func GetUserImageRecords(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userID := service.ImageRecordUserID(c)
	records, total, err := model.GetUserImageGenerationRecords(userID, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]map[string]any, 0, len(records))
	for _, record := range records {
		items = append(items, service.BuildImageRecordPublicResponse(record))
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func DeleteUserImageRecord(c *gin.Context) {
	userID := service.ImageRecordUserID(c)
	recordID := c.Param("record_id")
	deletedObjects, err := service.DeleteUserImageGenerationRecord(c, userID, recordID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"deleted_r2_objects": deletedObjects,
	})
}
