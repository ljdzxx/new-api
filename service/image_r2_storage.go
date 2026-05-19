package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/image_storage_setting"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
)

func StoreImageResultsToR2(c *gin.Context, info *relaycommon.RelayInfo, responseBody []byte) ([]byte, error) {
	setting := image_storage_setting.GetImageStorageSetting()
	if setting == nil || !setting.R2Enabled {
		return responseBody, nil
	}

	var root map[string]json.RawMessage
	if err := common.Unmarshal(responseBody, &root); err != nil {
		return nil, fmt.Errorf("parse image response for r2 storage failed: %w", err)
	}
	dataRaw, ok := root["data"]
	if !ok || len(dataRaw) == 0 {
		return responseBody, nil
	}

	var imageData []map[string]json.RawMessage
	if err := common.Unmarshal(dataRaw, &imageData); err != nil {
		return nil, fmt.Errorf("parse image response data for r2 storage failed: %w", err)
	}
	if len(imageData) == 0 {
		return responseBody, nil
	}

	changed := false
	for i := range imageData {
		item := imageData[i]
		payload := jsonStringValue(item["b64_json"])
		urlValue := jsonStringValue(item["url"])
		if payload == "" && isImageDataURL(urlValue) {
			payload = urlValue
		}
		if payload == "" && urlValue != "" {
			continue
		}

		if payload == "" {
			continue
		}

		imageBytes, err := decodeImageBase64(payload)
		if err != nil {
			return nil, fmt.Errorf("decode image payload failed: %w", err)
		}

		contentType := http.DetectContentType(imageBytes)
		objectKey := buildR2ImageObjectKey(setting.ObjectPrefix(), requestIDForObjectKey(c, info), userIDForObjectKey(c, info), i, contentType)
		url, err := uploadImageToR2(c.Request.Context(), setting, objectKey, imageBytes, contentType)
		if err != nil {
			return nil, err
		}

		urlRaw, err := common.Marshal(url)
		if err != nil {
			return nil, fmt.Errorf("marshal R2 image URL failed: %w", err)
		}
		item["url"] = urlRaw
		delete(item, "b64_json")
		changed = true
	}

	if !changed {
		return responseBody, nil
	}

	newDataRaw, err := common.Marshal(imageData)
	if err != nil {
		return nil, fmt.Errorf("marshal r2 image response data failed: %w", err)
	}
	root["data"] = newDataRaw

	rewritten, err := common.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("marshal r2 image response failed: %w", err)
	}
	logger.LogInfo(c, fmt.Sprintf("[image r2] stored image result(s) to R2: count=%d", len(imageData)))
	return rewritten, nil
}

func isImageDataURL(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.HasPrefix(value, "data:image/") && strings.Contains(value, ";base64,")
}

func decodeImageBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, ","); idx >= 0 {
		value = value[idx+1:]
	}
	if value == "" {
		return nil, fmt.Errorf("empty image payload")
	}
	return base64.StdEncoding.DecodeString(value)
}

func jsonStringValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := common.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func buildR2ImageObjectKey(prefix string, requestID string, userID int, index int, contentType string) string {
	ext := imageExtension(contentType)
	datePath := time.Now().Format("20060102")
	userPath := strconv.Itoa(userID)
	if userID <= 0 {
		userPath = "unknown"
	}
	name := fmt.Sprintf("%s-%d%s", safeObjectName(requestID), index, ext)
	return path.Join(prefix, datePath, userPath, name)
}

func requestIDForObjectKey(c *gin.Context, info *relaycommon.RelayInfo) string {
	if info != nil && info.RequestId != "" {
		return info.RequestId
	}
	if c != nil {
		if requestID := c.GetString(common.RequestIdKey); requestID != "" {
			return requestID
		}
	}
	return common.GetTimeString() + common.GetRandomString(8)
}

func userIDForObjectKey(c *gin.Context, info *relaycommon.RelayInfo) int {
	if info != nil && info.UserId > 0 {
		return info.UserId
	}
	if c != nil {
		return common.GetContextKeyInt(c, constant.ContextKeyUserId)
	}
	return 0
}

func safeObjectName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return common.GetTimeString() + common.GetRandomString(8)
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return common.GetTimeString() + common.GetRandomString(8)
	}
	return b.String()
}

func imageExtension(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func uploadImageToR2(ctx context.Context, setting *image_storage_setting.ImageStorageSetting, objectKey string, data []byte, contentType string) (string, error) {
	endpoint := setting.Endpoint()
	if missing := missingR2ImageStorageSettings(setting, endpoint); len(missing) > 0 {
		return "", fmt.Errorf("R2 image storage is enabled but required settings are incomplete: missing %s", strings.Join(missing, ", "))
	}

	cfg := aws.Config{
		Region:      "auto",
		Credentials: credentials.NewStaticCredentialsProvider(setting.R2AccessKeyID, setting.R2SecretAccessKey, ""),
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(setting.R2Bucket),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("upload image to R2 failed: %w", err)
	}

	presignClient := s3.NewPresignClient(client)
	presigned, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(setting.R2Bucket),
		Key:    aws.String(objectKey),
	}, func(options *s3.PresignOptions) {
		options.Expires = setting.URLExpireDuration()
	})
	if err != nil {
		return "", fmt.Errorf("presign R2 image URL failed: %w", err)
	}
	return presigned.URL, nil
}

func missingR2ImageStorageSettings(setting *image_storage_setting.ImageStorageSetting, endpoint string) []string {
	var missing []string
	if strings.TrimSpace(endpoint) == "" {
		missing = append(missing, "R2 Endpoint or R2 Account ID")
	}
	if strings.TrimSpace(setting.R2Bucket) == "" {
		missing = append(missing, "R2 Bucket")
	}
	if strings.TrimSpace(setting.R2AccessKeyID) == "" {
		missing = append(missing, "R2 Access Key ID")
	}
	if strings.TrimSpace(setting.R2SecretAccessKey) == "" {
		missing = append(missing, "R2 Secret Access Key")
	}
	return missing
}
