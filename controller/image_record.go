package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const imageRecordDownloadTimeout = 60 * time.Second

type imageRecordDownloadResult struct {
	Data         []imageRecordDownloadItem `json:"data"`
	OutputFormat string                    `json:"output_format"`
}

type imageRecordDownloadItem struct {
	URL     string `json:"url"`
	B64JSON string `json:"b64_json"`
}

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

func DownloadUserImageRecordImage(c *gin.Context) {
	userID := service.ImageRecordUserID(c)
	recordID := c.Param("record_id")
	index, err := strconv.Atoi(c.DefaultQuery("index", "0"))
	if err != nil || index < 0 {
		imageRecordDownloadError(c, http.StatusBadRequest, fmt.Errorf("invalid image index"))
		return
	}

	record, exists, err := model.GetUserImageGenerationRecord(userID, recordID)
	if err != nil {
		imageRecordDownloadError(c, http.StatusInternalServerError, err)
		return
	}
	if !exists {
		imageRecordDownloadError(c, http.StatusNotFound, fmt.Errorf("image record not found"))
		return
	}
	if len(record.Result) == 0 {
		imageRecordDownloadError(c, http.StatusNotFound, fmt.Errorf("image result not found"))
		return
	}

	var result imageRecordDownloadResult
	if err := common.Unmarshal(record.Result, &result); err != nil {
		imageRecordDownloadError(c, http.StatusInternalServerError, fmt.Errorf("parse image result failed: %w", err))
		return
	}
	if index >= len(result.Data) {
		imageRecordDownloadError(c, http.StatusBadRequest, fmt.Errorf("image index out of range"))
		return
	}

	item := result.Data[index]
	filename := imageRecordDownloadFilename(recordID, index, result.OutputFormat)
	if strings.TrimSpace(item.B64JSON) != "" {
		contentType := imageContentTypeFromFormat(result.OutputFormat)
		imageBytes, err := base64.StdEncoding.DecodeString(item.B64JSON)
		if err != nil {
			imageRecordDownloadError(c, http.StatusInternalServerError, fmt.Errorf("decode image failed: %w", err))
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		c.Data(http.StatusOK, contentType, imageBytes)
		return
	}

	imageURL := strings.TrimSpace(item.URL)
	if imageURL == "" {
		imageRecordDownloadError(c, http.StatusNotFound, fmt.Errorf("image url not found"))
		return
	}
	parsedURL, err := url.Parse(imageURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		imageRecordDownloadError(c, http.StatusBadRequest, fmt.Errorf("invalid image url"))
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), imageRecordDownloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		imageRecordDownloadError(c, http.StatusInternalServerError, err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		imageRecordDownloadError(c, http.StatusBadGateway, fmt.Errorf("download image failed: %w", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		imageRecordDownloadError(c, http.StatusBadGateway, fmt.Errorf("download image failed: upstream status %d", resp.StatusCode))
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = imageContentTypeFromFormat(result.OutputFormat)
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.DataFromReader(http.StatusOK, resp.ContentLength, contentType, resp.Body, nil)
}

func imageRecordDownloadError(c *gin.Context, status int, err error) {
	message := "download image failed"
	if err != nil {
		message = err.Error()
	}
	c.JSON(status, gin.H{
		"success": false,
		"message": message,
	})
}

func imageRecordDownloadFilename(recordID string, index int, outputFormat string) string {
	ext := strings.TrimSpace(strings.ToLower(outputFormat))
	if ext == "" {
		ext = "png"
	}
	ext = strings.TrimPrefix(ext, ".")
	switch ext {
	case "jpg", "jpeg", "png", "webp", "gif":
	default:
		ext = "png"
	}
	return fmt.Sprintf("image-generation-%s-%d.%s", recordID, index+1, ext)
}

func imageContentTypeFromFormat(outputFormat string) string {
	switch strings.TrimSpace(strings.ToLower(outputFormat)) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		return "image/png"
	}
}
