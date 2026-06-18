package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func withModelRateLimitTestState(t *testing.T, policies string) {
	t.Helper()
	oldPolicies := setting.UserLevelPolicies2JSONString()
	oldRedisEnabled := common.RedisEnabled
	oldEnabled := setting.ModelRequestRateLimitEnabled
	oldDuration := setting.ModelRequestRateLimitDurationMinutes
	oldCount := setting.ModelRequestRateLimitCount
	oldSuccessCount := setting.ModelRequestRateLimitSuccessCount
	oldGroup := setting.ModelRequestRateLimitGroup2JSONString()

	common.RedisEnabled = false
	if err := setting.UpdateUserLevelPoliciesByJSONString(policies); err != nil {
		t.Fatalf("set user level policies: %v", err)
	}
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		setting.ModelRequestRateLimitEnabled = oldEnabled
		setting.ModelRequestRateLimitDurationMinutes = oldDuration
		setting.ModelRequestRateLimitCount = oldCount
		setting.ModelRequestRateLimitSuccessCount = oldSuccessCount
		_ = setting.UpdateModelRequestRateLimitGroupByJSONString(oldGroup)
		_ = setting.UpdateUserLevelPoliciesByJSONString(oldPolicies)
	})
}

func buildModelRateLimitTestRouter(
	userID int,
	levelID int,
	userRateLimitEnabled bool,
	durationMinutes int,
	totalCount int,
	successCount int,
) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", userID)
		common.SetContextKey(c, constant.ContextKeyUserLevelID, levelID)
		common.SetContextKey(c, constant.ContextKeyUserRateLimitEnabled, userRateLimitEnabled)
		common.SetContextKey(c, constant.ContextKeyUserRateLimitDurationMinutes, durationMinutes)
		common.SetContextKey(c, constant.ContextKeyUserRateLimitCount, totalCount)
		common.SetContextKey(c, constant.ContextKeyUserRateLimitSuccessCount, successCount)
		c.Next()
	})
	router.Use(ModelRequestRateLimit())
	router.GET("/", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return router
}

func assertModelRateLimitStatuses(t *testing.T, router *gin.Engine, expected []int) {
	t.Helper()
	for i, want := range expected {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != want {
			t.Fatalf("request %d status = %d, want %d", i+1, recorder.Code, want)
		}
		if want == http.StatusTooManyRequests {
			assertOpenAISystemRateLimitBody(t, recorder)
		}
	}
}

func assertOpenAISystemRateLimitBody(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	var body map[string]any
	if err := common.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal rate limit response: %v, body=%q", err, recorder.Body.String())
	}
	errorBody, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error object in response: %#v", body)
	}
	if code := errorBody["code"]; code != string(types.ErrorCodeSystemRateLimit) {
		t.Fatalf("error.code = %#v, want %q", code, types.ErrorCodeSystemRateLimit)
	}
	if typ := errorBody["type"]; typ != "new_api_error" {
		t.Fatalf("error.type = %#v, want %q", typ, "new_api_error")
	}
	message, _ := errorBody["message"].(string)
	if !strings.Contains(message, "本系统速率限制已触发") {
		t.Fatalf("error.message = %q, want system rate limit marker", message)
	}
}

func TestModelRequestRateLimitUserSettingAppliesAndOverridesOtherLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withModelRateLimitTestState(t, `[{"id":77,"level":"Limited","rate":1}]`)

	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	setting.ModelRequestRateLimitCount = 1
	setting.ModelRequestRateLimitSuccessCount = 1

	router := buildModelRateLimitTestRouter(
		987654321,
		77,
		true,
		1,
		2,
		1000,
	)

	assertModelRateLimitStatuses(t, router, []int{
		http.StatusOK,
		http.StatusOK,
		http.StatusTooManyRequests,
	})
}

func TestModelRequestRateLimitFallsBackToLevelRateWhenUserSettingDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withModelRateLimitTestState(t, `[{"id":78,"level":"Limited","rate":1}]`)

	router := buildModelRateLimitTestRouter(
		987654322,
		78,
		false,
		1,
		2,
		1000,
	)

	assertModelRateLimitStatuses(t, router, []int{
		http.StatusOK,
		http.StatusTooManyRequests,
	})
}
