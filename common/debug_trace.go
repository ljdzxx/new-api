package common

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func NormalizeDebugTraceToken(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "Bearer ") || strings.HasPrefix(value, "bearer ") {
		value = strings.TrimSpace(value[7:])
	}
	value = strings.TrimPrefix(value, "sk-")
	return strings.TrimSpace(value)
}

func DebugTraceTokenMatches(value string) bool {
	filter := NormalizeDebugTraceToken(DebugTraceToken)
	if filter == "" {
		return true
	}
	value = NormalizeDebugTraceToken(value)
	if value == "" {
		return false
	}
	if value == filter {
		return true
	}
	if idx := strings.Index(value, "-"); idx > 0 && value[:idx] == filter {
		return true
	}
	return false
}

func DebugTraceEnabledForContext(c *gin.Context) bool {
	if !DebugTraceEnabled {
		return false
	}
	if NormalizeDebugTraceToken(DebugTraceToken) == "" {
		return true
	}
	if c == nil {
		return false
	}
	for _, value := range []string{
		c.GetString("token_key"),
		c.GetString("debug_trace_token_key"),
	} {
		if DebugTraceTokenMatches(value) {
			return true
		}
	}
	if c.Request == nil {
		return false
	}
	headers := c.Request.Header
	for _, value := range []string{
		headers.Get("Authorization"),
		headers.Get("x-api-key"),
		headers.Get("x-goog-api-key"),
		headers.Get("mj-api-secret"),
		c.Query("key"),
	} {
		if DebugTraceTokenMatches(value) {
			return true
		}
	}
	for _, part := range strings.Split(headers.Get("Sec-WebSocket-Protocol"), ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "openai-insecure-api-key.") &&
			DebugTraceTokenMatches(strings.TrimPrefix(part, "openai-insecure-api-key.")) {
			return true
		}
	}
	return false
}
