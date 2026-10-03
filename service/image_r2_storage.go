package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
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

const imageR2UploadTimeout = 5 * time.Minute

var imageR2CloudflareErrorPattern = regexp.MustCompile(`(?i)\berror code:\s*([0-9]{3,5})\b`)

func StoreImageResultsToR2(c *gin.Context, info *relaycommon.RelayInfo, responseBody []byte) ([]byte, error) {
	return storeImageResultsToR2(c, info, responseBody, 0)
}

func storeImageResultsToR2(c *gin.Context, info *relaycommon.RelayInfo, responseBody []byte, firstIndex int) ([]byte, error) {
	setting := image_storage_setting.GetImageStorageSetting()
	if setting == nil || !setting.R2Enabled {
		return responseBody, nil
	}

	var root map[string]json.RawMessage
	if err := common.Unmarshal(responseBody, &root); err != nil {
		return nil, fmt.Errorf("parse image response for r2 storage failed: %w", err)
	}
	var imageData []map[string]json.RawMessage
	dataRaw, hasData := root["data"]
	if hasData && len(dataRaw) > 0 {
		if err := common.Unmarshal(dataRaw, &imageData); err != nil {
			return nil, fmt.Errorf("parse image response data for r2 storage failed: %w", err)
		}
	} else {
		imageData = []map[string]json.RawMessage{root}
	}
	if len(imageData) == 0 {
		return responseBody, nil
	}

	storedCount := 0
	for i := range imageData {
		item := imageData[i]
		payload := jsonStringValue(item["b64_json"])
		urlValue := jsonStringValue(item["url"])
		if payload == "" && isImageDataURL(urlValue) {
			payload = urlValue
		}
		if payload == "" && urlValue == "" {
			continue
		}
		ctx := context.Background()
		if c != nil && c.Request != nil {
			ctx = c.Request.Context()
		}
		uploadCtx, cancel := context.WithTimeout(ctx, imageR2UploadTimeout)
		storedURL, err := storeImagePayloadToR2(uploadCtx, c, info, setting, payload, urlValue, firstIndex+i)
		cancel()
		if err != nil {
			if payload == "" && urlValue != "" {
				logger.LogWarn(c, fmt.Sprintf("[image r2] remote image transfer failed; returning upstream URL: index=%d err=%s", firstIndex+i, err.Error()))
				continue
			}
			return nil, err
		}

		urlRaw, err := common.Marshal(storedURL)
		if err != nil {
			return nil, fmt.Errorf("marshal R2 image URL failed: %w", err)
		}
		item["url"] = urlRaw
		delete(item, "b64_json")
		storedCount++
	}

	if storedCount == 0 {
		return responseBody, nil
	}

	if hasData {
		newDataRaw, err := common.Marshal(imageData)
		if err != nil {
			return nil, fmt.Errorf("marshal r2 image response data failed: %w", err)
		}
		root["data"] = newDataRaw
	}

	rewritten, err := common.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("marshal r2 image response failed: %w", err)
	}
	logger.LogInfo(c, fmt.Sprintf("[image r2] stored image result(s) to R2: count=%d", storedCount))
	return rewritten, nil
}

func storeImagePayloadToR2(ctx context.Context, c *gin.Context, info *relaycommon.RelayInfo, setting *image_storage_setting.ImageStorageSetting, payload, sourceURL string, index int) (string, error) {
	requestID := requestIDForObjectKey(c, info) + "-" + common.GetRandomString(12)
	if payload != "" {
		imageBytes, err := decodeImageBase64(payload)
		if err != nil {
			return "", fmt.Errorf("decode image payload failed: %w", err)
		}
		contentType := http.DetectContentType(imageBytes)
		if !strings.HasPrefix(contentType, "image/") {
			return "", fmt.Errorf("image payload is not a supported image")
		}
		objectKey := buildR2ImageObjectKey(setting.ObjectPrefix(), requestID, userIDForObjectKey(c, info), index, contentType)
		return uploadImageToR2(ctx, setting, objectKey, imageBytes, contentType)
	}
	objectKey := buildR2ImageObjectKey(setting.ObjectPrefix(), requestID, userIDForObjectKey(c, info), index, "image/png")
	return importRemoteImageToR2(ctx, setting, sourceURL, objectKey)
}

func importRemoteImageToR2(ctx context.Context, setting *image_storage_setting.ImageStorageSetting, sourceURL, objectKey string) (string, error) {
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", fmt.Errorf("invalid remote image URL")
	}
	workerURL, err := url.Parse(strings.TrimSpace(setting.R2WorkerURL))
	if err != nil || workerURL.Scheme != "https" || workerURL.Host == "" || workerURL.User != nil || len(strings.TrimSpace(setting.R2WorkerSecret)) < 32 {
		return "", fmt.Errorf("R2 remote image storage requires an HTTPS Worker URL and a Worker secret of at least 32 characters")
	}
	client, err := newR2S3Client(setting)
	if err != nil {
		return "", err
	}
	body, err := common.Marshal(map[string]string{"source_url": sourceURL, "object_key": objectKey, "bucket": setting.R2Bucket})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, workerURL.String(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(setting.R2WorkerSecret))
	req.Header.Set("Content-Type", "application/json")
	workerClient := &http.Client{Timeout: imageR2UploadTimeout, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := workerClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("R2 Worker request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", imageR2WorkerResponseError(resp)
	}
	var result struct {
		ObjectKey string `json:"object_key"`
	}
	if err := common.DecodeJson(io.LimitReader(resp.Body, 64*1024), &result); err != nil || result.ObjectKey != objectKey {
		return "", fmt.Errorf("invalid R2 Worker import response")
	}
	return presignR2Image(ctx, client, setting, objectKey)
}

func imageR2WorkerResponseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var diagnostic struct {
		Error          string `json:"error"`
		Stage          string `json:"stage"`
		Reason         string `json:"reason"`
		UpstreamStatus int    `json:"upstream_status"`
	}
	if resp.StatusCode == http.StatusBadGateway && common.Unmarshal(body, &diagnostic) == nil && diagnostic.Error == "image_import_failed" {
		stages := map[string]bool{
			"request_body": true, "request_parse": true, "source_validation": true,
			"dns_lookup": true, "source_download": true, "redirect_validation": true,
			"image_read": true, "image_validation": true, "r2_upload": true,
		}
		reasons := map[string]bool{
			"request_read_failed": true, "invalid_request_json": true, "source_validation_failed": true,
			"invalid_source_url": true, "source_not_allowed": true, "source_host_not_allowed": true,
			"dns_fetch_failed": true, "dns_http_error": true, "invalid_dns_response": true,
			"dns_lookup_failed": true, "non_public_dns": true, "source_fetch_failed": true,
			"invalid_redirect": true, "source_http_error": true, "empty_source_body": true,
			"image_read_failed": true, "image_too_large": true, "image_validation_failed": true,
			"unsupported_image": true, "r2_put_failed": true, "source_timeout": true,
		}
		if stages[diagnostic.Stage] && reasons[diagnostic.Reason] {
			if diagnostic.UpstreamStatus >= 100 && diagnostic.UpstreamStatus <= 599 {
				return fmt.Errorf("R2 Worker import failed: status 502, stage=%s, reason=%s, upstream_status=%d", diagnostic.Stage, diagnostic.Reason, diagnostic.UpstreamStatus)
			}
			return fmt.Errorf("R2 Worker import failed: status 502, stage=%s, reason=%s", diagnostic.Stage, diagnostic.Reason)
		}
	}
	if match := imageR2CloudflareErrorPattern.FindSubmatch(body); len(match) == 2 {
		return fmt.Errorf("R2 Worker import failed: status %d, cloudflare_error=%s", resp.StatusCode, match[1])
	}
	if resp.StatusCode == http.StatusNotFound && bytes.Equal(bytes.TrimSpace(body), []byte("Not found")) {
		return fmt.Errorf("R2 Worker import failed: status 404, Worker endpoint not found (expected /import)")
	}
	return fmt.Errorf("R2 Worker import failed: status %d", resp.StatusCode)
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
	client, err := newR2S3Client(setting)
	if err != nil {
		return "", err
	}

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(setting.R2Bucket),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("upload image to R2 failed: %w", err)
	}

	return presignR2Image(ctx, client, setting, objectKey)
}

func presignR2Image(ctx context.Context, client *s3.Client, setting *image_storage_setting.ImageStorageSetting, objectKey string) (string, error) {
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

func newR2S3Client(setting *image_storage_setting.ImageStorageSetting) (*s3.Client, error) {
	endpoint := setting.Endpoint()
	if missing := missingR2ImageStorageSettings(setting, endpoint); len(missing) > 0 {
		return nil, fmt.Errorf("R2 image storage is enabled but required settings are incomplete: missing %s", strings.Join(missing, ", "))
	}

	cfg := aws.Config{
		Region: "auto",
		Credentials: credentials.NewStaticCredentialsProvider(
			strings.TrimSpace(setting.R2AccessKeyID),
			strings.TrimSpace(setting.R2SecretAccessKey), ""),
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
		// Cloudflare R2 不支持新版 SDK 默认的 CRC32 请求校验和，
		// 需改为 WhenRequired，否则会出现 SignatureDoesNotMatch
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	return client, nil
}

func DeleteR2ImagesByResponseBody(ctx context.Context, responseBody []byte) (int, error) {
	setting := image_storage_setting.GetImageStorageSetting()
	if setting == nil || !setting.R2Enabled || len(responseBody) == 0 {
		return 0, nil
	}

	var root map[string]json.RawMessage
	if err := common.Unmarshal(responseBody, &root); err != nil {
		return 0, fmt.Errorf("parse image record result failed: %w", err)
	}
	dataRaw, ok := root["data"]
	if !ok || len(dataRaw) == 0 {
		return 0, nil
	}

	var imageData []map[string]json.RawMessage
	if err := common.Unmarshal(dataRaw, &imageData); err != nil {
		return 0, fmt.Errorf("parse image record data failed: %w", err)
	}
	keys := make([]string, 0, len(imageData))
	for _, item := range imageData {
		objectKey := r2ObjectKeyFromURL(jsonStringValue(item["url"]), setting)
		if objectKey != "" {
			keys = append(keys, objectKey)
		}
	}
	if len(keys) == 0 {
		return 0, nil
	}

	client, err := newR2S3Client(setting)
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, key := range keys {
		_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(setting.R2Bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return deleted, fmt.Errorf("delete image from R2 failed: key=%s: %w", key, err)
		}
		deleted++
	}
	return deleted, nil
}

func r2ObjectKeyFromURL(rawURL string, setting *image_storage_setting.ImageStorageSetting) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || setting == nil {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	endpoint, err := url.Parse(setting.Endpoint())
	if err != nil || endpoint == nil || !strings.EqualFold(parsed.Host, endpoint.Host) {
		return ""
	}
	cleanPath := strings.TrimLeft(parsed.EscapedPath(), "/")
	if cleanPath == "" {
		return ""
	}
	parts := strings.SplitN(cleanPath, "/", 2)
	if len(parts) != 2 || parts[0] != setting.R2Bucket {
		return ""
	}
	objectKey, err := url.PathUnescape(parts[1])
	if err != nil {
		return ""
	}
	return strings.TrimLeft(objectKey, "/")
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
