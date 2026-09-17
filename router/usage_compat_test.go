package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupUsageCompat(t *testing.T) (*gin.Engine, *gorm.DB, *model.Token, *model.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	oldDB, oldLogDB, oldRedis, oldQuota := model.DB, model.LOG_DB, common.RedisEnabled, common.QuotaPerUnit
	model.DB, model.LOG_DB, common.RedisEnabled, common.QuotaPerUnit = db, db, false, 500000
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled, common.QuotaPerUnit = oldDB, oldLogDB, oldRedis, oldQuota
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	if err := db.AutoMigrate(&model.Token{}, &model.User{}, &model.Log{}, &model.UserSubscription{}, &model.SubscriptionPlan{}); err != nil {
		t.Fatal(err)
	}
	user := &model.User{Username: "usage-owner", Status: common.UserStatusEnabled, Quota: 2500000, Group: "default", Setting: `{"billing_preference":"wallet_only"}`}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	token := &model.Token{UserId: user.Id, Key: strings.Repeat("a", 48), Name: "usage-key", Status: common.TokenStatusEnabled, RemainQuota: 750000, UsedQuota: 250000, ExpiredTime: -1}
	if err := db.Create(token).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	SetRelayRouter(router)
	return router, db, token, user
}

func requestUsageCompat(t *testing.T, router *gin.Engine, query string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/usage"+query, nil)
	req.RemoteAddr = "192.0.2.10:1234"
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	var body map[string]any
	if err := common.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("non-JSON response %d: %s", response.Code, response.Body.String())
	}
	return response.Code, body
}

func TestUsageCompatAuthentication(t *testing.T) {
	router, db, token, user := setupUsageCompat(t)
	key := "sk-" + token.Key
	cases := []struct {
		name, query string
		headers     map[string]string
		status      int
		code        string
	}{
		{"missing", "", nil, 401, "API_KEY_REQUIRED"},
		{"bearer", "", map[string]string{"Authorization": "Bearer " + key}, 200, ""},
		{"case-insensitive bearer", "", map[string]string{"Authorization": "bEaReR  " + key + " "}, 200, ""},
		{"anthropic", "", map[string]string{"x-api-key": key}, 200, ""},
		{"gemini", "", map[string]string{"x-goog-api-key": key}, 200, ""},
		{"invalid bearer falls back", "", map[string]string{"Authorization": "Basic junk", "x-api-key": key}, 200, ""},
		{"bearer takes priority", "", map[string]string{"Authorization": "Bearer invalid", "x-api-key": key}, 401, "INVALID_API_KEY"},
		{"anthropic takes priority", "", map[string]string{"x-api-key": "invalid", "x-goog-api-key": key}, 401, "INVALID_API_KEY"},
		{"query key rejected", "?key=secret", map[string]string{"Authorization": "Bearer " + key}, 400, "api_key_in_query_deprecated"},
		{"query api_key rejected", "?api_key=secret", nil, 400, "api_key_in_query_deprecated"},
		{"blank query accepted", "?key=%20", map[string]string{"x-api-key": key}, 200, ""},
		{"raw authorization rejected", "", map[string]string{"Authorization": key}, 401, "API_KEY_REQUIRED"},
		{"oversized key", "", map[string]string{"x-api-key": strings.Repeat("x", 129)}, 401, "INVALID_API_KEY"},
		{"oversized bearer", "", map[string]string{"Authorization": "Bearer " + strings.Repeat("x", 129)}, 401, "INVALID_API_KEY"},
		{"key suffix rejected", "", map[string]string{"x-api-key": key + "-suffix"}, 401, "INVALID_API_KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := requestUsageCompat(t, router, tc.query, tc.headers)
			if status != tc.status || (tc.code != "" && body["code"] != tc.code) {
				t.Fatalf("got %d %#v", status, body)
			}
			if tc.code != "" && len(body) != 2 {
				t.Fatalf("auth error envelope differs: %#v", body)
			}
		})
	}
	headers := map[string]string{"Authorization": "Bearer " + key}
	for _, state := range []struct {
		status int
		code   int
		name   string
	}{
		{common.TokenStatusExpired, 200, "expired"},
		{common.TokenStatusExhausted, 200, "quota_exhausted"},
		{common.TokenStatusDisabled, 401, ""},
		{99, 401, ""},
		{common.TokenStatusEnabled, 200, "active"},
	} {
		if err := db.Model(token).Updates(map[string]any{"status": state.status, "expired_time": time.Now().Add(-time.Hour).Unix(), "remain_quota": 0}).Error; err != nil {
			t.Fatal(err)
		}
		status, body := requestUsageCompat(t, router, "", headers)
		if status != state.code {
			t.Fatalf("state %d: %d %#v", state.status, status, body)
		}
		if status == 200 && (body["status"] != state.name || body["days_until_expiry"] != float64(0) || body["isValid"] != true) {
			t.Fatalf("state response %#v", body)
		}
	}
	if err := db.Model(token).Update("allow_ips", "198.51.100.0/24").Error; err != nil {
		t.Fatal(err)
	}
	status, body := requestUsageCompat(t, router, "", headers)
	if status != 403 || body["code"] != "ACCESS_DENIED" || body["message"] != "Access denied. Your IP is 192.0.2.10" {
		t.Fatalf("IP rejection: %d %#v", status, body)
	}
	if err := db.Model(token).Update("allow_ips", "192.0.2.0/24").Error; err != nil {
		t.Fatal(err)
	}
	status, _ = requestUsageCompat(t, router, "", headers)
	if status != 200 {
		t.Fatalf("allowed CIDR got %d", status)
	}
	if err := db.Model(user).Update("status", common.UserStatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	status, body = requestUsageCompat(t, router, "", headers)
	if status != 401 || body["code"] != "USER_INACTIVE" {
		t.Fatalf("inactive user: %d %#v", status, body)
	}
}

func TestUsageCompatBalanceAndParameters(t *testing.T) {
	router, db, token, user := setupUsageCompat(t)
	headers := map[string]string{"x-api-key": "sk-" + token.Key}
	status, body := requestUsageCompat(t, router, "", headers)
	if status != 200 || body["mode"] != "quota_limited" || body["remaining"] != 1.5 || body["unit"] != "USD" {
		t.Fatalf("quota response: %d %#v", status, body)
	}
	quota := body["quota"].(map[string]any)
	if quota["limit"] != float64(2) || quota["used"] != 0.5 || quota["remaining"] != 1.5 {
		t.Fatalf("quota: %#v", quota)
	}
	for _, field := range []string{"expires_at", "days_until_expiry", "balance", "planName", "rate_limits", "model_stats", "data", "success"} {
		if _, ok := body[field]; ok {
			t.Fatalf("unexpected field %s", field)
		}
	}
	if len(body["daily_usage"].([]any)) != 0 {
		t.Fatal("empty daily usage must be []")
	}
	for _, days := range []string{"0", "91", "-1", "1.5", "abc", "%202", "9999999999999999999999999"} {
		status, body = requestUsageCompat(t, router, "?days="+days, headers)
		if status != 400 || body["type"] != "error" {
			t.Fatalf("days=%s: %d %#v", days, status, body)
		}
		err := body["error"].(map[string]any)
		if err["type"] != "invalid_request_error" || err["message"] != "Invalid days, allowed range is 1-90" {
			t.Fatalf("wrong validation error: %#v", err)
		}
	}
	for _, query := range []string{"?days=1", "?days=90", "?days=%20", "?start_date=bad&end_date=bad&timezone=bad", "?start_date=2099-01-01&end_date=2000-01-01"} {
		status, body = requestUsageCompat(t, router, query, headers)
		if status != 200 {
			t.Fatalf("query %s: %d %#v", query, status, body)
		}
	}
	if err := db.Model(token).Update("unlimited_quota", true).Error; err != nil {
		t.Fatal(err)
	}
	status, body = requestUsageCompat(t, router, "", headers)
	if status != 200 || body["mode"] != "unrestricted" || body["balance"] != float64(5) || body["remaining"] != float64(5) || body["planName"] != "钱包余额" {
		t.Fatalf("wallet: %d %#v", status, body)
	}
	if err := db.Model(user).Update("quota", 0).Error; err != nil {
		t.Fatal(err)
	}
	status, body = requestUsageCompat(t, router, "", headers)
	if status != 200 || body["balance"] != float64(0) {
		t.Fatalf("empty wallet: %d %#v", status, body)
	}
	if err := db.Migrator().DropTable(&model.Log{}); err != nil {
		t.Fatal(err)
	}
	status, body = requestUsageCompat(t, router, "", headers)
	if status != 200 || body["balance"] != float64(0) {
		t.Fatalf("log failure must preserve balance: %d %#v", status, body)
	}
	if _, ok := body["usage"]; ok {
		t.Fatal("failed stats must be omitted")
	}
}

func TestUsageCompatStatsIsolationAndDateRanges(t *testing.T) {
	router, db, token, user := setupUsageCompat(t)
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	entries := []model.Log{
		{UserId: user.Id, TokenId: token.Id, Type: model.LogTypeConsume, CreatedAt: start.Add(-time.Second).Unix(), ModelName: "yesterday", PromptTokens: 100, CompletionTokens: 20, Quota: 500000, UseTime: 1, Other: `{"cache_tokens":30,"group_ratio":2}`},
		{UserId: user.Id, TokenId: token.Id, Type: model.LogTypeConsume, CreatedAt: start.Unix(), ModelName: "today", PromptTokens: 100, CompletionTokens: 20, Quota: 1000000, UseTime: 3, Other: `{"claude":true,"cache_tokens":30,"cache_creation_tokens":10,"group_ratio":2}`},
		{UserId: user.Id, TokenId: token.Id + 1, Type: model.LogTypeConsume, CreatedAt: now.Unix(), ModelName: "other-key", Quota: 9000000},
		{UserId: user.Id + 1, TokenId: token.Id, Type: model.LogTypeConsume, CreatedAt: now.Unix(), ModelName: "other-user", Quota: 9000000},
		{UserId: user.Id, TokenId: token.Id, Type: model.LogTypeError, CreatedAt: now.Unix(), ModelName: "error", Quota: 9000000},
	}
	if err := db.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}
	query := fmt.Sprintf("?days=1&start_date=%s&end_date=%s", start.Format("2006-01-02"), start.Format("2006-01-02"))
	status, body := requestUsageCompat(t, router, query, map[string]string{"x-api-key": token.Key})
	if status != 200 {
		t.Fatalf("%d %#v", status, body)
	}
	usage := body["usage"].(map[string]any)
	total := usage["total"].(map[string]any)
	today := usage["today"].(map[string]any)
	if total["requests"] != float64(2) || total["total_tokens"] != float64(280) || total["input_tokens"] != float64(170) || total["actual_cost"] != float64(3) || total["cost"] != 1.5 {
		t.Fatalf("total: %#v", total)
	}
	if today["requests"] != float64(1) || usage["average_duration_ms"] != float64(2000) {
		t.Fatalf("today/duration: %#v", usage)
	}
	daily := body["daily_usage"].([]any)
	if len(daily) != 1 || daily[0].(map[string]any)["cache_write_tokens"] != float64(10) {
		t.Fatalf("daily: %#v", daily)
	}
	models := body["model_stats"].([]any)
	if len(models) != 1 || models[0].(map[string]any)["model"] != "today" {
		t.Fatalf("models: %#v", models)
	}
}

func TestUsageCompatSubscription(t *testing.T) {
	router, db, token, user := setupUsageCompat(t)
	if err := db.Model(token).Update("unlimited_quota", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(user).Update("setting", `{"billing_preference":"subscription_only"}`).Error; err != nil {
		t.Fatal(err)
	}
	plan := model.SubscriptionPlan{Title: "Daily plan", QuotaResetPeriod: model.SubscriptionResetDaily, TotalAmount: 5000000}
	if err := db.Create(&plan).Error; err != nil {
		t.Fatal(err)
	}
	sub := model.UserSubscription{UserId: user.Id, PlanId: plan.Id, AmountTotal: 5000000, AmountUsed: 1000000, Status: "active", StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(24 * time.Hour).Unix()}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	status, body := requestUsageCompat(t, router, "", map[string]string{"x-api-key": token.Key})
	if status != 200 || body["planName"] != "Daily plan" || body["remaining"] != float64(8) || body["mode"] != "unrestricted" {
		t.Fatalf("subscription: %d %#v", status, body)
	}
	info := body["subscription"].(map[string]any)
	if info["daily_limit_usd"] != float64(10) || info["daily_usage_usd"] != float64(2) || info["weekly_limit_usd"] != nil || info["monthly_limit_usd"] != nil {
		t.Fatalf("subscription fields: %#v", info)
	}
	if _, ok := body["balance"]; ok {
		t.Fatal("subscription must not include wallet balance")
	}
}
