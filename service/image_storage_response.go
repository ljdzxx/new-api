package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/image_storage_setting"

	"github.com/gin-gonic/gin"
)

type ImageStorageResponseWriter struct {
	gin.ResponseWriter
	context        *gin.Context
	info           *relaycommon.RelayInfo
	buffer         bytes.Buffer
	status         int
	imageIndex     int
	storageError   error
	upstreamFailed bool
	completed      []json.RawMessage
	usage          json.RawMessage
	outputFormat   json.RawMessage
}

func NewImageStorageResponseWriter(c *gin.Context, info *relaycommon.RelayInfo) *ImageStorageResponseWriter {
	if !image_storage_setting.GetImageStorageSetting().R2Enabled || !isImageRelayPath(c) {
		return nil
	}
	return &ImageStorageResponseWriter{ResponseWriter: c.Writer, context: c, info: info, status: http.StatusOK}
}

func (writer *ImageStorageResponseWriter) Status() int {
	return writer.status
}

func (writer *ImageStorageResponseWriter) WriteHeader(status int) {
	if status > 0 && !writer.ResponseWriter.Written() {
		writer.status = status
	}
}

func (writer *ImageStorageResponseWriter) isStream() bool {
	return strings.Contains(strings.ToLower(writer.Header().Get("Content-Type")), "text/event-stream")
}

func (writer *ImageStorageResponseWriter) WriteHeaderNow() {
	if writer.isStream() {
		writer.Header().Del("Content-Length")
		writer.ResponseWriter.WriteHeader(writer.status)
		writer.ResponseWriter.WriteHeaderNow()
	}
}

func (writer *ImageStorageResponseWriter) WriteString(value string) (int, error) {
	return writer.Write([]byte(value))
}

func (writer *ImageStorageResponseWriter) Write(data []byte) (int, error) {
	if writer.storageError != nil {
		return len(data), nil
	}
	writer.buffer.Write(data)
	if writer.isStream() {
		writer.drainFrames(false)
	}
	return len(data), nil
}

func (writer *ImageStorageResponseWriter) Flush() {
	if writer.isStream() {
		writer.WriteHeaderNow()
		writer.ResponseWriter.Flush()
	}
}

func (writer *ImageStorageResponseWriter) drainFrames(final bool) {
	for writer.buffer.Len() > 0 && writer.storageError == nil {
		pending := writer.buffer.Bytes()
		boundary, delimiterSize := bytes.Index(pending, []byte("\n\n")), 2
		if crlf := bytes.Index(pending, []byte("\r\n\r\n")); crlf >= 0 && (boundary < 0 || crlf < boundary) {
			boundary, delimiterSize = crlf, 4
		}
		if boundary < 0 {
			if !final {
				return
			}
			boundary, delimiterSize = len(pending), 0
		}
		frame := append([]byte(nil), writer.buffer.Next(boundary+delimiterSize)...)
		writer.storageError = writer.writeFrame(frame)
	}
}

func (writer *ImageStorageResponseWriter) writeFrame(frame []byte) error {
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(frame), "\r\n", "\n"), "\n"), "\n")
	var dataLines, otherLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		} else {
			otherLines = append(otherLines, line)
		}
	}
	payload := []byte(strings.Join(dataLines, "\n"))
	if len(payload) > 0 && string(payload) != "[DONE]" {
		rewritten, err := storeImageResultsToR2(writer.context, writer.info, payload, writer.imageIndex)
		if err != nil {
			return err
		}
		writer.imageIndex++
		var event map[string]json.RawMessage
		if err := common.Unmarshal(rewritten, &event); err != nil {
			return err
		}
		eventType := jsonStringValue(event["type"])
		if eventType == "error" || eventType == "upstream_error" || len(event["error"]) > 0 {
			writer.upstreamFailed = true
		}
		if eventType == "image_generation.completed" || eventType == "image_edit.completed" {
			if len(event["output_format"]) > 0 {
				writer.outputFormat = append(json.RawMessage(nil), event["output_format"]...)
			}
			if len(event["data"]) > 0 {
				var items []json.RawMessage
				if err := common.Unmarshal(event["data"], &items); err != nil {
					return err
				}
				writer.completed = append(writer.completed, items...)
			} else if jsonStringValue(event["url"]) != "" {
				writer.completed = append(writer.completed, rewritten)
			}
		}
		if len(event["usage"]) > 0 {
			writer.usage = append(json.RawMessage(nil), event["usage"]...)
		}
		otherLines = append(otherLines, "data: "+string(rewritten))
		frame = []byte(strings.Join(otherLines, "\n") + "\n\n")
	}
	writer.WriteHeaderNow()
	_, err := writer.ResponseWriter.Write(frame)
	return err
}

func (writer *ImageStorageResponseWriter) Finish() error {
	if writer.isStream() {
		writer.drainFrames(true)
		if writer.info != nil && writer.info.StreamStatus != nil {
			reason := writer.info.StreamStatus.EndReason
			if reason != relaycommon.StreamEndReasonDone && reason != relaycommon.StreamEndReasonEOF {
				writer.upstreamFailed = true
			}
		}
		if writer.storageError == nil && !writer.upstreamFailed && len(writer.completed) > 0 {
			result := map[string]any{"data": writer.completed}
			if len(writer.usage) > 0 {
				result["usage"] = writer.usage
			}
			if len(writer.outputFormat) > 0 {
				result["output_format"] = writer.outputFormat
			}
			body, err := common.Marshal(result)
			if err != nil {
				writer.storageError = err
			} else {
				MarkImageRecordSuccess(writer.context, body)
			}
		} else if writer.upstreamFailed {
			MarkImageRecordFailure(writer.context, fmt.Errorf("upstream image stream failed"))
		}
	} else {
		body := writer.buffer.Bytes()
		if writer.status >= 200 && writer.status < 300 {
			body, writer.storageError = StoreImageResultsToR2(writer.context, writer.info, body)
			if writer.storageError == nil {
				MarkImageRecordSuccess(writer.context, body)
			}
		}
		if writer.storageError == nil {
			writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
			writer.ResponseWriter.WriteHeader(writer.status)
			_, writer.storageError = writer.ResponseWriter.Write(body)
		}
	}
	if writer.storageError != nil {
		MarkImageRecordFailure(writer.context, writer.storageError)
		body, _ := common.Marshal(map[string]any{"type": "error", "error": map[string]string{"type": "image_storage_error", "message": "Image could not be stored in R2"}})
		writer.Header().Del("Content-Length")
		if writer.isStream() {
			_, _ = writer.ResponseWriter.Write(append(append([]byte("event: error\ndata: "), body...), []byte("\n\ndata: [DONE]\n\n")...))
		} else {
			writer.Header().Set("Content-Type", "application/json")
			writer.ResponseWriter.WriteHeader(http.StatusBadGateway)
			_, _ = writer.ResponseWriter.Write(body)
		}
	}
	writer.ResponseWriter.Flush()
	return writer.storageError
}
