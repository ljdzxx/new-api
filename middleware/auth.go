package middleware

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func tokenAuthDebugEnabled(c *gin.Context) bool {
	return common.DebugEnabled || common.DebugTraceEnabled
}

func tokenAuthMaskForLog(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "<empty>"
	}
	if len(value) <= 8 {
		return fmt.Sprintf("%s(len=%d)", value, len(value))
	}
	return fmt.Sprintf("%s...%s(len=%d)", value[:4], value[len(value)-4:], len(value))
}

func tokenAuthFingerprintForLog(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "<empty>"
	}
	hash := common.Sha1([]byte(value))
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

func logTokenAuthDebug(c *gin.Context, format string, args ...any) {
	if !tokenAuthDebugEnabled(c) {
		return
	}
	logger.LogInfo(c, "[token auth] "+fmt.Sprintf(format, args...))
}

func logTokenAuthRawMessagesRequest(c *gin.Context) {
	if c == nil || c.Request == nil || !tokenAuthDebugEnabled(c) || !strings.Contains(c.Request.URL.Path, "/v1/messages") {
		return
	}
	dump, dumpErr := httputil.DumpRequest(c.Request, false)
	if dumpErr != nil {
		logger.LogError(c, "[token auth raw] dump request metadata failed: "+dumpErr.Error())
	} else {
		logger.LogInfo(c, "[token auth raw] request metadata and headers:\n"+string(dump))
	}
	storage, bodyErr := common.GetBodyStorage(c)
	if bodyErr != nil {
		logger.LogError(c, "[token auth raw] read request body failed: "+bodyErr.Error())
		return
	}
	body, bodyErr := storage.Bytes()
	if bodyErr != nil {
		logger.LogError(c, "[token auth raw] copy request body failed: "+bodyErr.Error())
		return
	}
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		logger.LogError(c, "[token auth raw] reset request body storage failed: "+seekErr.Error())
	}
	logger.LogInfo(c, fmt.Sprintf("[token auth raw] request body bytes=%d:\n%s", len(body), string(body)))
}

func normalizeBearerToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "Bearer ") || strings.HasPrefix(raw, "bearer ") {
		return strings.TrimSpace(raw[7:])
	}
	return raw
}

func tokenAuthCandidateKeys(rawToken string) []string {
	key := strings.TrimPrefix(strings.TrimSpace(rawToken), "sk-")
	if key == "" {
		return nil
	}
	candidates := []string{key}
	if strings.Contains(key, "-") {
		legacyKey := strings.Split(key, "-")[0]
		if legacyKey != "" && legacyKey != key {
			candidates = append(candidates, legacyKey)
		}
	}
	return candidates
}

func validUserInfo(username string, role int) bool {
	// check username is empty
	if strings.TrimSpace(username) == "" {
		return false
	}
	if !common.IsValidateRole(role) {
		return false
	}
	return true
}

func authHelper(c *gin.Context, minRole int) {
	session := sessions.Default(c)
	username := session.Get("username")
	role := session.Get("role")
	id := session.Get("id")
	status := session.Get("status")
	useAccessToken := false
	if username == nil {
		// Check access token
		accessToken := c.Request.Header.Get("Authorization")
		if accessToken == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，未登录且未提供 access token",
			})
			c.Abort()
			return
		}
		user := model.ValidateAccessToken(accessToken)
		if user != nil && user.Username != "" {
			if !validUserInfo(user.Username, user.Role) {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "无权进行此操作，用户信息无效",
				})
				c.Abort()
				return
			}
			// Token is valid
			username = user.Username
			role = user.Role
			id = user.Id
			status = user.Status
			useAccessToken = true
		} else {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "无权进行此操作，access token 无效",
			})
			c.Abort()
			return
		}
	}
	// get header New-Api-User
	apiUserIdStr := c.Request.Header.Get("New-Api-User")
	if apiUserIdStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "无权进行此操作，未提供 New-Api-User",
		})
		c.Abort()
		return
	}
	apiUserId, err := strconv.Atoi(apiUserIdStr)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "无权进行此操作，New-Api-User 格式错误",
		})
		c.Abort()
		return

	}
	if id != apiUserId {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "无权进行此操作，New-Api-User 与登录用户不匹配",
		})
		c.Abort()
		return
	}
	if status.(int) == common.UserStatusDisabled {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "用户已被封禁",
		})
		c.Abort()
		return
	}
	if role.(int) < minRole {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权进行此操作，权限不足",
		})
		c.Abort()
		return
	}
	if !validUserInfo(username.(string), role.(int)) {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无权进行此操作，用户信息无效",
		})
		c.Abort()
		return
	}
	// 防止不同newapi版本冲突，导致数据不通用
	c.Header("Auth-Version", "864b7076dbcd0a3c01b5520316720ebf")
	c.Set("username", username)
	c.Set("role", role)
	c.Set("id", id)
	c.Set("group", session.Get("group"))
	c.Set("user_group", session.Get("group"))
	c.Set("use_access_token", useAccessToken)

	c.Next()
}

func TryUserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		id := session.Get("id")
		if id != nil {
			c.Set("id", id)
		}
		c.Next()
	}
}

func UserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleCommonUser)
	}
}

func AdminAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleAdminUser)
	}
}

func RootAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		authHelper(c, common.RoleRootUser)
	}
}

func WssAuth(c *gin.Context) {

}

// TokenOrUserAuth allows either session-based user auth or API token auth.
// Used for endpoints that need to be accessible from both the dashboard and API clients.
func TokenOrUserAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		// Try session auth first (dashboard users)
		session := sessions.Default(c)
		if id := session.Get("id"); id != nil {
			if status, ok := session.Get("status").(int); ok && status == common.UserStatusEnabled {
				c.Set("id", id)
				c.Next()
				return
			}
		}
		// Fall back to token auth (API clients)
		TokenAuth()(c)
	}
}

// TokenAuthReadOnly 宽松版本的令牌认证中间件，用于只读查询接口。
// 只验证令牌 key 是否存在，不检查令牌状态、过期时间和额度。
// 即使令牌已过期、已耗尽或已禁用，也允许访问。
// 仍然检查用户是否被封禁。
func TokenAuthReadOnly() func(c *gin.Context) {
	return func(c *gin.Context) {
		key := c.Request.Header.Get("Authorization")
		if key == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "未提供 Authorization 请求头",
			})
			c.Abort()
			return
		}
		if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		key = strings.TrimPrefix(key, "sk-")
		parts := strings.Split(key, "-")
		key = parts[0]

		token, err := model.GetTokenByKey(key, false)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无效的令牌",
			})
			c.Abort()
			return
		}

		userCache, err := model.GetUserCache(token.UserId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": err.Error(),
			})
			c.Abort()
			return
		}
		if userCache.Status != common.UserStatusEnabled {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "用户已被封禁",
			})
			c.Abort()
			return
		}

		c.Set("id", token.UserId)
		c.Set("token_id", token.Id)
		c.Set("token_key", token.Key)
		c.Next()
	}
}

func TokenAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		logTokenAuthRawMessagesRequest(c)
		tokenSource := "authorization"
		// 先检测是否为ws
		if c.Request.Header.Get("Sec-WebSocket-Protocol") != "" {
			// Sec-WebSocket-Protocol: realtime, openai-insecure-api-key.sk-xxx, openai-beta.realtime-v1
			// read sk from Sec-WebSocket-Protocol
			key := c.Request.Header.Get("Sec-WebSocket-Protocol")
			parts := strings.Split(key, ",")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "openai-insecure-api-key") {
					key = strings.TrimPrefix(part, "openai-insecure-api-key.")
					tokenSource = "sec-websocket-protocol"
					break
				}
			}
			c.Request.Header.Set("Authorization", "Bearer "+key)
		}
		// 检查path包含/v1/messages 或 /v1/models
		if strings.Contains(c.Request.URL.Path, "/v1/messages") || strings.Contains(c.Request.URL.Path, "/v1/models") {
			anthropicKey := c.Request.Header.Get("x-api-key")
			authorizationKey := strings.TrimSpace(c.Request.Header.Get("Authorization"))
			if anthropicKey != "" && authorizationKey == "" {
				c.Request.Header.Set("Authorization", "Bearer "+anthropicKey)
				tokenSource = "x-api-key"
			} else if anthropicKey != "" {
				logTokenAuthDebug(c, "x-api-key ignored for user token auth because Authorization is present: authorization=%q x_api_key=%q", authorizationKey, anthropicKey)
			}
		}
		// gemini api 从query中获取key
		if strings.HasPrefix(c.Request.URL.Path, "/v1beta/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1beta/openai/models") ||
			strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
			skKey := c.Query("key")
			if skKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+skKey)
				tokenSource = "query:key"
			}
			// 从x-goog-api-key header中获取key
			xGoogKey := c.Request.Header.Get("x-goog-api-key")
			if xGoogKey != "" {
				c.Request.Header.Set("Authorization", "Bearer "+xGoogKey)
				tokenSource = "x-goog-api-key"
			}
		}
		rawToken := normalizeBearerToken(c.Request.Header.Get("Authorization"))
		if rawToken == "" || rawToken == "midjourney-proxy" {
			rawToken = normalizeBearerToken(c.Request.Header.Get("mj-api-secret"))
			tokenSource = "mj-api-secret"
		}
		candidates := tokenAuthCandidateKeys(rawToken)
		logTokenAuthDebug(c, "token received: source=%s raw_token=%q raw_len=%d raw_sha1=%s candidates=%d path=%s", tokenSource, rawToken, len(rawToken), tokenAuthFingerprintForLog(rawToken), len(candidates), c.Request.URL.Path)

		var (
			token        *model.Token
			err          error
			selectedKey  string
			attemptErrs  []string
			attemptIndex int
		)
		if len(candidates) == 0 {
			err = fmt.Errorf("未提供令牌")
		}
		for attemptIndex, candidateKey := range candidates {
			logTokenAuthDebug(c, "validating token candidate: source=%s attempt=%d key=%q key_len=%d key_sha1=%s legacy_fallback=%t", tokenSource, attemptIndex+1, candidateKey, len(candidateKey), tokenAuthFingerprintForLog(candidateKey), attemptIndex > 0)
			token, err = model.ValidateUserToken(candidateKey)
			if err == nil {
				selectedKey = candidateKey
				break
			}
			attemptErrs = append(attemptErrs, fmt.Sprintf("attempt=%d key=%q key_len=%d key_sha1=%s err=%s", attemptIndex+1, candidateKey, len(candidateKey), tokenAuthFingerprintForLog(candidateKey), err.Error()))
		}
		if token != nil {
			id := c.GetInt("id")
			if id == 0 {
				c.Set("id", token.UserId)
			}
		}
		if err != nil {
			logTokenAuthDebug(c, "token validation failed: source=%s raw_token=%q raw_len=%d raw_sha1=%s attempts=[%s]", tokenSource, rawToken, len(rawToken), tokenAuthFingerprintForLog(rawToken), strings.Join(attemptErrs, "; "))
			abortWithOpenAiMessage(c, http.StatusUnauthorized, err.Error())
			return
		}
		logTokenAuthDebug(c, "token validation succeeded: source=%s selected_attempt=%d selected_key=%q selected_key_len=%d selected_key_sha1=%s token_id=%d token_db_key=%q user_id=%d group=%s", tokenSource, attemptIndex+1, selectedKey, len(selectedKey), tokenAuthFingerprintForLog(selectedKey), token.Id, token.Key, token.UserId, token.Group)

		allowIps := token.GetIpLimits()
		if len(allowIps) > 0 {
			clientIp := c.ClientIP()
			logger.LogDebug(c, "Token has IP restrictions, checking client IP %s", clientIp)
			ip := net.ParseIP(clientIp)
			if ip == nil {
				abortWithOpenAiMessage(c, http.StatusForbidden, "无法解析客户端 IP 地址")
				return
			}
			if common.IsIpInCIDRList(ip, allowIps) == false {
				abortWithOpenAiMessage(c, http.StatusForbidden, "您的 IP 不在令牌允许访问的列表中", types.ErrorCodeAccessDenied)
				return
			}
			logger.LogDebug(c, "Client IP %s passed the token IP restrictions check", clientIp)
		}

		userCache, err := model.GetUserCache(token.UserId)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, err.Error())
			return
		}
		userEnabled := userCache.Status == common.UserStatusEnabled
		if !userEnabled {
			abortWithOpenAiMessage(c, http.StatusForbidden, "用户已被封禁")
			return
		}

		userCache.WriteContext(c)

		userGroup := userCache.Group
		tokenGroup := token.Group
		if tokenGroup != "" {
			// check common.UserUsableGroups[userGroup]
			if _, ok := service.GetUserUsableGroups(userGroup)[tokenGroup]; !ok {
				abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("无权访问 %s 分组", tokenGroup))
				return
			}
			// check group in common.GroupRatio
			if !ratio_setting.ContainsGroupRatio(tokenGroup) {
				if tokenGroup != "auto" {
					abortWithOpenAiMessage(c, http.StatusForbidden, fmt.Sprintf("分组 %s 已被弃用", tokenGroup))
					return
				}
			}
			userGroup = tokenGroup
		}
		common.SetContextKey(c, constant.ContextKeyUsingGroup, userGroup)

		parts := strings.Split(selectedKey, "-")
		err = SetupContextForToken(c, token, parts...)
		if err != nil {
			return
		}
		c.Next()
	}
}

func SetupContextForToken(c *gin.Context, token *model.Token, parts ...string) error {
	if token == nil {
		return fmt.Errorf("token is nil")
	}
	c.Set("id", token.UserId)
	c.Set("token_id", token.Id)
	c.Set("token_key", token.Key)
	c.Set("token_name", token.Name)
	c.Set("token_unlimited_quota", token.UnlimitedQuota)
	if !token.UnlimitedQuota {
		c.Set("token_quota", token.RemainQuota)
	}
	if token.ModelLimitsEnabled {
		c.Set("token_model_limit_enabled", true)
		c.Set("token_model_limit", token.GetModelLimitsMap())
	} else {
		c.Set("token_model_limit_enabled", false)
	}
	common.SetContextKey(c, constant.ContextKeyTokenGroup, token.Group)
	common.SetContextKey(c, constant.ContextKeyTokenCrossGroupRetry, token.CrossGroupRetry)
	if len(parts) > 1 {
		if model.IsAdmin(token.UserId) {
			c.Set("specific_channel_id", parts[1])
		} else {
			c.Header("specific_channel_version", "701e3ae1dc3f7975556d354e0675168d004891c8")
			abortWithOpenAiMessage(c, http.StatusForbidden, "普通用户不支持指定渠道")
			return fmt.Errorf("普通用户不支持指定渠道")
		}
	}
	return nil
}
