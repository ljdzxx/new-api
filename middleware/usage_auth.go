package middleware

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UsageTokenAuth implements sub2api's read-only /v1/usage authentication contract.
// Expiry and exhausted quota do not prevent reading usage; disabling a key does.
func UsageTokenAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		fail := func(status int, code, message string) {
			c.AbortWithStatusJSON(status, gin.H{"code": code, "message": message})
		}
		const maxKeyBytes = 128
		if len(c.GetHeader("Authorization")) > maxKeyBytes+128 || len(c.GetHeader("x-api-key")) > maxKeyBytes || len(c.GetHeader("x-goog-api-key")) > maxKeyBytes {
			fail(http.StatusUnauthorized, "INVALID_API_KEY", "Invalid API key")
			return
		}
		if strings.TrimSpace(c.Query("key")) != "" || strings.TrimSpace(c.Query("api_key")) != "" {
			fail(http.StatusBadRequest, "api_key_in_query_deprecated", "API key in query parameter is deprecated. Please use Authorization header instead.")
			return
		}
		var key string
		parts := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			key = strings.TrimSpace(parts[1])
		}
		if key == "" {
			key = c.GetHeader("x-api-key")
		}
		if key == "" {
			key = c.GetHeader("x-goog-api-key")
		}
		if key == "" {
			fail(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required in Authorization header (Bearer scheme), x-api-key header, or x-goog-api-key header")
			return
		}
		if len(key) > maxKeyBytes {
			fail(http.StatusUnauthorized, "INVALID_API_KEY", "Invalid API key")
			return
		}
		// Only strip the presentation prefix; never accept arbitrary suffixes of a key.
		token, err := model.GetTokenForUsage(c.Request.Context(), strings.TrimPrefix(key, "sk-"))
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				fail(http.StatusUnauthorized, "INVALID_API_KEY", "Invalid API key")
			} else {
				fail(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to validate API key")
			}
			return
		}
		if token.Status != common.TokenStatusEnabled && token.Status != common.TokenStatusExpired && token.Status != common.TokenStatusExhausted {
			fail(http.StatusUnauthorized, "API_KEY_DISABLED", "API key is disabled")
			return
		}
		if allowed := token.GetIpLimits(); len(allowed) > 0 {
			clientIP := c.ClientIP()
			ip := net.ParseIP(clientIP)
			if ip == nil || !common.IsIpInCIDRList(ip, allowed) {
				if clientIP == "" {
					clientIP = "unknown"
				}
				fail(http.StatusForbidden, "ACCESS_DENIED", fmt.Sprintf("Access denied. Your IP is %s", clientIP))
				return
			}
		}
		user, err := model.GetUserById(token.UserId, false)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				fail(http.StatusUnauthorized, "USER_NOT_FOUND", "User associated with API key not found")
			} else {
				fail(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to validate API key")
			}
			return
		}
		if user.Status != common.UserStatusEnabled {
			fail(http.StatusUnauthorized, "USER_INACTIVE", "User account is not active")
			return
		}
		if token.Group != "" && token.Group != "auto" {
			if !ratio_setting.ContainsGroupRatio(token.Group) {
				fail(http.StatusForbidden, "GROUP_DELETED", "API Key 所属分组已删除")
				return
			}
			if _, ok := service.GetUserUsableGroups(user.Group)[token.Group]; !ok {
				fail(http.StatusForbidden, "GROUP_NOT_ALLOWED", "API Key 所属专属分组不再允许当前用户使用")
				return
			}
		}
		c.Set("usage_token", token)
		c.Set("usage_user", user)
		c.Set("id", user.Id)
		c.Set("token_id", token.Id)
		c.Next()
	}
}
