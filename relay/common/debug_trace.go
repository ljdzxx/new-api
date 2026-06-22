package common

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	rootcommon "github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
)

const debugTraceClientRequestLoggedKey = "debug_trace_client_request_logged"

type debugTraceReadCloser struct {
	rc       io.ReadCloser
	ctx      *gin.Context
	info     *RelayInfo
	label    string
	method   string
	url      string
	status   int
	header   http.Header
	buf      bytes.Buffer
	once     sync.Once
	closeErr error
}

func (r *debugTraceReadCloser) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if n > 0 {
		_, _ = r.buf.Write(p[:n])
	}
	if err == io.EOF {
		r.log()
	}
	return n, err
}

func (r *debugTraceReadCloser) Close() error {
	r.closeErr = r.rc.Close()
	r.log()
	return r.closeErr
}

func (r *debugTraceReadCloser) log() {
	r.once.Do(func() {
		logBodyTrace(r.ctx, r.info, r.label, r.method, r.url, r.status, r.header, r.buf.Bytes(), r.closeErr)
	})
}

func LogClientRequestTrace(c *gin.Context, info *RelayInfo) {
	if !rootcommon.DebugTraceEnabled || c == nil || c.Request == nil {
		return
	}
	if _, ok := c.Get(debugTraceClientRequestLoggedKey); ok {
		return
	}
	c.Set(debugTraceClientRequestLoggedKey, true)

	storage, err := rootcommon.GetBodyStorage(c)
	if err != nil {
		logger.LogWarn(c, fmt.Sprintf("[debug trace] client request body read failed: %s", err.Error()))
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		logger.LogWarn(c, fmt.Sprintf("[debug trace] client request body read failed: %s", err.Error()))
		return
	}
	urlText := ""
	if c.Request.URL != nil {
		urlText = c.Request.URL.RequestURI()
	}
	logBodyTrace(c, info, "client request", c.Request.Method, urlText, 0, c.Request.Header, body, nil)
}

func WrapRequestBodyForDebugTrace(c *gin.Context, req *http.Request, info *RelayInfo) {
	if !rootcommon.DebugTraceEnabled || c == nil || req == nil || req.Body == nil {
		return
	}
	urlText := ""
	if req.URL != nil {
		urlText = req.URL.String()
	}
	req.Body = &debugTraceReadCloser{
		rc:     req.Body,
		ctx:    c,
		info:   info,
		label:  "upstream request",
		method: req.Method,
		url:    urlText,
		header: req.Header.Clone(),
	}
}

func WrapResponseBodyForDebugTrace(c *gin.Context, resp *http.Response, info *RelayInfo) {
	if !rootcommon.DebugTraceEnabled || c == nil || resp == nil || resp.Body == nil {
		return
	}
	method := ""
	urlText := ""
	if resp.Request != nil {
		method = resp.Request.Method
		if resp.Request.URL != nil {
			urlText = resp.Request.URL.String()
		}
	}
	resp.Body = &debugTraceReadCloser{
		rc:     resp.Body,
		ctx:    c,
		info:   info,
		label:  "upstream response",
		method: method,
		url:    urlText,
		status: resp.StatusCode,
		header: resp.Header.Clone(),
	}
}

func logBodyTrace(c *gin.Context, info *RelayInfo, label, method, urlText string, status int, header http.Header, body []byte, closeErr error) {
	var b strings.Builder
	b.WriteString("[debug trace] ")
	b.WriteString(label)
	b.WriteString(":")
	if method != "" {
		b.WriteString(" method=")
		b.WriteString(method)
	}
	if urlText != "" {
		b.WriteString(" url=")
		b.WriteString(fmt.Sprintf("%q", urlText))
	}
	if status > 0 {
		b.WriteString(fmt.Sprintf(" status=%d", status))
	}
	if info != nil {
		channelID, channelType, apiType, upstreamModelName := debugTraceRelayInfoFields(c, info)
		b.WriteString(fmt.Sprintf(
			" channel_id=%d channel_type=%d api_type=%d origin_model=%q upstream_model=%q retry=%d stream=%t",
			channelID,
			channelType,
			apiType,
			info.OriginModelName,
			upstreamModelName,
			info.RetryIndex,
			info.IsStream,
		))
	}
	if closeErr != nil {
		b.WriteString(" close_error=")
		b.WriteString(fmt.Sprintf("%q", closeErr.Error()))
	}
	b.WriteString(fmt.Sprintf("\nheaders: %s", formatDebugTraceHeaders(header)))

	encoding, text := debugTraceBodyText(header, body)
	b.WriteString(fmt.Sprintf("\nbody_bytes=%d body_encoding=%s\n", len(body), encoding))
	b.WriteString(text)
	logger.LogInfo(c, b.String())
}

func debugTraceRelayInfoFields(c *gin.Context, info *RelayInfo) (channelID int, channelType int, apiType int, upstreamModelName string) {
	if info == nil {
		return 0, 0, 0, ""
	}
	upstreamModelName = info.OriginModelName
	if info.ChannelMeta != nil {
		channelID = info.ChannelId
		channelType = info.ChannelType
		apiType = info.ApiType
		if info.UpstreamModelName != "" {
			upstreamModelName = info.UpstreamModelName
		}
		return channelID, channelType, apiType, upstreamModelName
	}
	if c != nil {
		channelID = rootcommon.GetContextKeyInt(c, appconstant.ContextKeyChannelId)
		channelType = rootcommon.GetContextKeyInt(c, appconstant.ContextKeyChannelType)
		apiType, _ = rootcommon.ChannelType2APIType(channelType)
	}
	return channelID, channelType, apiType, upstreamModelName
}

func debugTraceBodyText(header http.Header, body []byte) (string, string) {
	if len(body) == 0 {
		return "text", ""
	}
	if !isBinaryContentType(header.Get("Content-Type")) && isTextBody(body) {
		return "text", string(body)
	}
	return "base64", base64.StdEncoding.EncodeToString(body)
}

func isBinaryContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if contentType == "" {
		return false
	}
	if strings.HasPrefix(contentType, "text/") {
		return false
	}
	switch contentType {
	case "application/json",
		"application/x-ndjson",
		"application/xml",
		"application/x-www-form-urlencoded",
		"application/javascript",
		"application/problem+json":
		return false
	}
	return strings.HasPrefix(contentType, "image/") ||
		strings.HasPrefix(contentType, "audio/") ||
		strings.HasPrefix(contentType, "video/") ||
		contentType == "application/octet-stream" ||
		contentType == "application/pdf" ||
		contentType == "application/zip" ||
		contentType == "application/gzip"
}

func isTextBody(body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	for len(body) > 0 {
		r, size := utf8.DecodeRune(body)
		if r == utf8.RuneError && size == 1 {
			return false
		}
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
		body = body[size:]
	}
	return true
}

func formatDebugTraceHeaders(header http.Header) string {
	if len(header) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(header))
	for k := range header {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		values := append([]string(nil), header.Values(k)...)
		for i, value := range values {
			values[i] = formatDebugTraceHeaderValue(k, value)
		}
		parts = append(parts, fmt.Sprintf("%s=%q", k, strings.Join(values, ",")))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func formatDebugTraceHeaderValue(key, value string) string {
	if isSensitiveDebugTraceHeader(key) {
		value = strings.TrimSpace(value)
		if value == "" {
			return "<empty>"
		}
		hash := rootcommon.Sha1([]byte(value))
		if len(hash) > 12 {
			hash = hash[:12]
		}
		return fmt.Sprintf("<present len=%d sha1=%s>", len(value), hash)
	}
	return value
}

func isSensitiveDebugTraceHeader(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	return lower == "authorization" ||
		lower == "proxy-authorization" ||
		lower == "api-key" ||
		lower == "x-api-key" ||
		lower == "x-goog-api-key" ||
		lower == "cookie" ||
		lower == "set-cookie"
}
