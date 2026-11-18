package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var epayPollingTestIP atomic.Uint32

func TestEpayPollingExemptFromSharedAPIBudget(t *testing.T) {
	oldDuration := common.GlobalApiRateLimitDuration
	common.GlobalApiRateLimitDuration = 60
	oldEnable, oldNum, oldRedis := common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.RedisEnabled
	common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.RedisEnabled = true, 1, false
	t.Cleanup(func() {
		common.GlobalApiRateLimitDuration = oldDuration
		common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.RedisEnabled = oldEnable, oldNum, oldRedis
	})
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("epay-router-test"))))
	SetApiRouter(engine)
	clientAddr := fmt.Sprintf("[2001:db8::%x]:8080", epayPollingTestIP.Add(1))
	request := func(method, path string, expected int) {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = clientAddr
		out := httptest.NewRecorder()
		engine.ServeHTTP(out, req)
		require.Equal(t, expected, out.Code, "%s %s", method, path)
	}
	poll := func() {
		request(http.MethodGet, "/api/payment/epay/order?trade_no=test&scene=topup", http.StatusUnauthorized)
		request(http.MethodGet, "/api/payment/epay/pending", http.StatusUnauthorized)
	}
	poll()
	request(http.MethodGet, "/api/payment/topup/meta", http.StatusUnauthorized)
	poll()
	for _, path := range []string{"/api/payment/epay/cancel", "/api/payment/topup/checkout", "/api/payment/subscription/checkout"} {
		request(http.MethodPost, path, http.StatusTooManyRequests)
	}
}
