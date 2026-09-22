package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRegistrationControllerTest(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	db, logDB := model.DB, model.LOG_DB
	rdb, redisEnabled := common.RDB, common.RedisEnabled
	registrationConfig := common.GetRegisterRiskConfig()
	register, password, email := common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled
	quota, defaultToken := common.QuotaForNewUser, constant.GenerateDefaultToken
	sqliteFlag, mysqlFlag, pgFlag := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&model.User{}, &model.Token{}, &model.UserRegistrationProfile{}, &model.InviteRewardAudit{}, &model.Log{}, &model.UserOAuthBinding{}))
	model.DB, model.LOG_DB = testDB, testDB
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RDB, common.RedisEnabled = client, true
	common.RegistrationRisk = common.RegisterRiskConfig{Enabled: true, CooldownHours: 24, HitThreshold: 1, RejectMessage: "configured rejection"}
	common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = true, true, false
	common.QuotaForNewUser, constant.GenerateDefaultToken = 0, true
	t.Cleanup(func() {
		client.Close()
		sqlDB, _ := testDB.DB()
		sqlDB.Close()
		model.DB, model.LOG_DB = db, logDB
		common.RDB, common.RedisEnabled = rdb, redisEnabled
		common.RegistrationRisk = registrationConfig
		common.RegisterEnabled, common.PasswordRegisterEnabled, common.EmailVerificationEnabled = register, password, email
		common.QuotaForNewUser, constant.GenerateDefaultToken = quota, defaultToken
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = sqliteFlag, mysqlFlag, pgFlag
	})
	return server
}

func seedRegistrationToken(t *testing.T, server *miniredis.Miniredis) string {
	t.Helper()
	hash := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	parts := []string{hash("canvas"), hash("webgl"), hash("audio"), hash("fonts"), hash("ua"), hash("locale"), hash("screen"), hash("hardware")}
	fp := model.RegistrationFingerprint{FingerprintHash: hash(strings.Join(parts, "|")), CanvasHash: parts[0], WebGLHash: parts[1], AudioHash: parts[2], FontsHash: parts[3], UAHash: parts[4], LocaleHash: parts[5], ScreenHash: parts[6], HardwareHash: parts[7]}
	id := common.GetUUID()
	data, err := common.Marshal(map[string]any{
		"TokenId": id, "IPHash": common.GenerateHMAC("register_risk:ip:203.0.113.1"),
		"UAHash": common.GenerateHMAC("register_risk:ua:browser"), "Fingerprint": fp, "ExpiresAt": time.Now().Add(15 * time.Minute).Unix(),
	})
	require.NoError(t, err)
	require.NoError(t, server.Set("register_risk:token:"+id, string(data)))
	server.SetTTL("register_risk:token:"+id, 15*time.Minute)
	return id + "." + common.GenerateHMAC("register_risk_token:"+id)
}

func submitRiskRegistration(t *testing.T, username, token string) tokenAPIResponse {
	t.Helper()
	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/user/register", map[string]any{"username": username, "password": "Password123", "risk_token": token}, 0)
	ctx.Request.RemoteAddr = "203.0.113.1:1234"
	ctx.Request.Header.Set("User-Agent", "browser")
	Register(ctx)
	var response tokenAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestRegistrationRiskRejectsBeforeUserAndTokenCreation(t *testing.T) {
	server := setupRegistrationControllerTest(t)
	first := submitRiskRegistration(t, "risk_first", seedRegistrationToken(t, server))
	require.True(t, first.Success, first.Message)
	second := submitRiskRegistration(t, "risk_second", seedRegistrationToken(t, server))
	require.False(t, second.Success)
	require.Equal(t, "configured rejection", second.Message)
	for _, table := range []any{&model.User{}, &model.Token{}, &model.UserRegistrationProfile{}} {
		var count int64
		require.NoError(t, model.DB.Model(table).Count(&count).Error)
		require.EqualValues(t, 1, count)
	}
	missing := submitRiskRegistration(t, "risk_missing", "")
	require.False(t, missing.Success)
	server.FastForward(24 * time.Hour)
	third := submitRiskRegistration(t, "risk_third", seedRegistrationToken(t, server))
	require.True(t, third.Success, third.Message)
}

func TestRegistrationRiskDatabaseFailureCompensates(t *testing.T) {
	server := setupRegistrationControllerTest(t)
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("risk_test_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.AddError(fmt.Errorf("injected create failure"))
		}
	}))
	failed := submitRiskRegistration(t, "risk_failed", seedRegistrationToken(t, server))
	require.False(t, failed.Success)
	require.NoError(t, model.DB.Callback().Create().Remove("risk_test_fail"))
	retried := submitRiskRegistration(t, "risk_retry", seedRegistrationToken(t, server))
	require.True(t, retried.Success, retried.Message)
}

func TestOAuthRegistrationRiskAndExistingLogin(t *testing.T) {
	server := setupRegistrationControllerTest(t)
	router := gin.New()
	router.Use(sessions.Sessions("registration_test", cookie.NewStore([]byte("registration-test-session-secret"))))
	provider := &oauth.GitHubProvider{}
	var currentToken string
	var currentIdentity string
	var user *model.User
	var creationErr error
	router.GET("/test", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("risk_token", currentToken)
		user, creationErr = findOrCreateOAuthUser(c, provider, &oauth.OAuthUser{ProviderUserID: currentIdentity, Username: "oauth_" + currentIdentity}, session)
	})
	call := func(id, token string) {
		currentToken, currentIdentity = token, id
		request := httptest.NewRequest(http.MethodGet, "/test", nil)
		request.RemoteAddr = "203.0.113.1:1234"
		request.Header.Set("User-Agent", "browser")
		router.ServeHTTP(httptest.NewRecorder(), request)
	}
	call("101", seedRegistrationToken(t, server))
	require.NoError(t, creationErr)
	firstID := user.Id
	call("102", seedRegistrationToken(t, server))
	var riskErr *model.RegisterRiskError
	require.ErrorAs(t, creationErr, &riskErr)
	require.Equal(t, "hit_threshold", riskErr.Reason)
	call("101", "") // Existing logins need neither a token nor a free registration slot.
	require.NoError(t, creationErr)
	require.Equal(t, firstID, user.Id)
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
