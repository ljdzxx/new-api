package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenAuthDebugLogsDoNotExposeCredentials(t *testing.T) {
	oldDB, oldRedis, oldDebug, oldWriter := model.DB, common.RedisEnabled, common.DebugEnabled, gin.DefaultWriter
	common.RedisEnabled = false
	common.DebugEnabled = true
	var logs bytes.Buffer
	gin.DefaultWriter = &logs
	t.Cleanup(func() {
		model.DB = oldDB
		common.RedisEnabled = oldRedis
		common.DebugEnabled = oldDebug
		gin.DefaultWriter = oldWriter
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.Close()
	model.DB = db
	// Initialize reserved column quoting through the production entry point.
	t.Setenv("SQL_DSN", "local")
	oldPath, oldMaster := common.SQLitePath, common.IsMasterNode
	common.SQLitePath, common.IsMasterNode = ":memory:", false
	t.Cleanup(func() { common.SQLitePath, common.IsMasterNode = oldPath, oldMaster })
	require.NoError(t, model.InitDB())
	bootstrap, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, bootstrap.Close())
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.User{}))
	validKey := "SyntheticValidCredential0123456789"
	// A valid token with a missing user is enough to cover successful token
	// validation logs, without configuring auth caches or production accounts.
	require.NoError(t, db.Create(&model.Token{UserId: 918273, Key: validKey, Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true}).Error)
	for _, key := range []string{validKey, "SyntheticInvalidCredential9876543210"} {
		logs.Reset()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{}`))
		c.Request.Header.Set("Authorization", "Bearer sk-"+key)
		TokenAuth()(c)
		require.NotContains(t, logs.String(), key)
		require.Contains(t, logs.String(), "redacted")
		require.Contains(t, logs.String(), tokenAuthFingerprintForLog(key))
		if key == validKey {
			require.Contains(t, logs.String(), "token validation succeeded")
		} else {
			require.Contains(t, logs.String(), "token validation failed")
		}
	}
}

func TestTokenAuthRawDumpRedactsHeadersAndQueryWithoutMutation(t *testing.T) {
	oldDebug, oldWriter := common.DebugEnabled, gin.DefaultWriter
	common.DebugEnabled = true
	var logs bytes.Buffer
	gin.DefaultWriter = &logs
	t.Cleanup(func() { common.DebugEnabled = oldDebug; gin.DefaultWriter = oldWriter })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages?key=query-credential&keep=yes", strings.NewReader(`{"messages":[]}`))
	for name, value := range map[string]string{"Authorization": "Bearer auth-credential", "x-api-key": "anthropic-credential", "Cookie": "session-credential", "Sec-WebSocket-Protocol": "openai-insecure-api-key.ws-credential"} {
		c.Request.Header.Set(name, value)
	}
	logTokenAuthRawMessagesRequest(c)
	for _, secret := range []string{"query-credential", "auth-credential", "anthropic-credential", "session-credential", "ws-credential"} {
		require.NotContains(t, logs.String(), secret)
	}
	require.Equal(t, "Bearer auth-credential", c.Request.Header.Get("Authorization"))
	require.Equal(t, "query-credential", c.Query("key"))
	require.Contains(t, logs.String(), "keep=yes")
	for _, short := range []string{"a", "secret7"} {
		require.NotContains(t, tokenAuthMaskForLog(short), short+"(")
	}
}
