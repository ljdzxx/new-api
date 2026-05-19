package service

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

var imageTraceRedactedKeys = map[string]struct{}{
	"b64_json":             {},
	"b64":                  {},
	"base64":               {},
	"base64_json":          {},
	"data_uri":             {},
	"image":                {},
	"images":               {},
	"image[]":              {},
	"image_url":            {},
	"image_urls":           {},
	"url":                  {},
	"urls":                 {},
	"mask":                 {},
	"bytesBase64Encoded":   {},
	"bytes_base64_encoded": {},
	"inline_data":          {},
	"inlineData":           {},
	"mime_type":            {},
	"mimeType":             {},
}

func isImageTraceEnabled(c *gin.Context) bool {
	return common.DebugTraceEnabled && isImageRelayPath(c)
}

func isImageRelayPath(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	path := c.Request.URL.Path
	return strings.HasPrefix(path, "/v1/images/generations") ||
		strings.HasPrefix(path, "/v1/images/edits")
}

func isRedactedImageTraceKey(key string) bool {
	if _, ok := imageTraceRedactedKeys[key]; ok {
		return true
	}
	lower := strings.ToLower(key)
	if strings.Contains(lower, "b64") ||
		strings.Contains(lower, "base64") ||
		strings.Contains(lower, "image") ||
		strings.Contains(lower, "mask") {
		return true
	}
	return false
}

func sanitizeImageTraceValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeImageTraceMap(typed)
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, sanitizeImageTraceValue(item))
		}
		return items
	default:
		return value
	}
}

func sanitizeImageTraceMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		if isRedactedImageTraceKey(key) {
			output[key] = "[redacted image payload]"
			continue
		}
		output[key] = sanitizeImageTraceValue(value)
	}
	return output
}

func sanitizeImageTraceJSON(data []byte) any {
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{"bytes": 0}
	}
	var obj map[string]any
	if err := common.Unmarshal(data, &obj); err == nil {
		return sanitizeImageTraceMap(obj)
	}
	var arr []any
	if err := common.Unmarshal(data, &arr); err == nil {
		return sanitizeImageTraceValue(arr)
	}
	return map[string]any{
		"bytes": len(data),
		"note":  "non-json body omitted",
	}
}

func logImageTrace(c *gin.Context, label string, payload any) {
	if !isImageTraceEnabled(c) {
		return
	}
	body, err := common.Marshal(payload)
	if err != nil {
		logger.LogInfo(c, fmt.Sprintf("[image trace] %s marshal failed: %s", label, err.Error()))
		return
	}
	logger.LogInfo(c, fmt.Sprintf("[image trace] %s: %s", label, string(body)))
}

// LogImageRelayJSONRequestTrace records image request fields except image payloads
// when performance debug trace logging is enabled.
func LogImageRelayJSONRequestTrace(c *gin.Context, source string, data []byte) {
	if !isImageTraceEnabled(c) {
		return
	}
	logImageTrace(c, "request "+source, sanitizeImageTraceJSON(data))
}

// LogImageRelayMultipartRequestTrace records multipart fields and file metadata,
// but never logs uploaded file bytes.
func LogImageRelayMultipartRequestTrace(c *gin.Context, source string, form *multipart.Form) {
	if !isImageTraceEnabled(c) || form == nil {
		return
	}
	fields := make(map[string]any)
	for key, values := range form.Value {
		if isRedactedImageTraceKey(key) {
			fields[key] = "[redacted image payload]"
			continue
		}
		if len(values) == 1 {
			fields[key] = values[0]
		} else {
			copied := append([]string(nil), values...)
			fields[key] = copied
		}
	}
	files := make(map[string][]map[string]any)
	for key, fileHeaders := range form.File {
		items := make([]map[string]any, 0, len(fileHeaders))
		for _, fileHeader := range fileHeaders {
			if fileHeader == nil {
				continue
			}
			items = append(items, map[string]any{
				"filename": fileHeader.Filename,
				"size":     fileHeader.Size,
				"headers":  fileHeader.Header,
			})
		}
		files[key] = items
	}
	logImageTrace(c, "request "+source, map[string]any{
		"fields": fields,
		"files":  files,
	})
}

// LogImageRelayResponseTrace records image response fields except image payloads
// when performance debug trace logging is enabled.
func LogImageRelayResponseTrace(c *gin.Context, source string, resp *http.Response, data []byte) {
	if !isImageTraceEnabled(c) {
		return
	}
	statusCode := 0
	headers := http.Header{}
	if resp != nil {
		statusCode = resp.StatusCode
		headers = resp.Header
	}
	logImageTrace(c, "response "+source, map[string]any{
		"status_code": statusCode,
		"headers":     headers,
		"body":        sanitizeImageTraceJSON(data),
	})
}
