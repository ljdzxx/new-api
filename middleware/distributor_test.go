package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldRecordChannelAffinityAfterRelaySkipsForwardedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Status(http.StatusOK)
	common.SetContextKey(ctx, constant.ContextKeyChannelForwardedApplied, true)

	require.False(t, shouldRecordChannelAffinityAfterRelay(ctx, &model.Channel{Id: 2}))
}

func TestShouldRecordChannelAffinityAfterRelayAllowsSuccessfulNonForwardedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Status(http.StatusOK)

	require.True(t, shouldRecordChannelAffinityAfterRelay(ctx, &model.Channel{Id: 2}))
}

func TestShouldRecordChannelAffinityAfterRelaySkipsFailedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Status(http.StatusBadGateway)

	require.False(t, shouldRecordChannelAffinityAfterRelay(ctx, &model.Channel{Id: 2}))
}

func TestSubscriptionGroupRestrictionSkippedForSubscriptionFirst(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{
		BillingPreference: "subscription_first",
	})
	common.SetContextKey(ctx, constant.ContextKeySubscriptionAllowedGroups, "codex-pro")

	require.NoError(t, ensureSubscriptionAllowsRequestedGroup(ctx, "pro"))
}

func TestSubscriptionGroupRestrictionAppliesForSubscriptionOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{
		BillingPreference: "subscription_only",
	})
	common.SetContextKey(ctx, constant.ContextKeySubscriptionAllowedGroups, "codex-pro")

	err := ensureSubscriptionAllowsRequestedGroup(ctx, "pro")
	require.Error(t, err)
	require.Equal(t, "套餐不支持在 pro 下使用", err.Error())
}

func TestSubscriptionUnsupportedGroupAbortUsesBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	abortWithSubscriptionUnsupportedGroup(ctx, subscriptionUnsupportedGroupError("pro"))

	require.Equal(t, "false", w.Header().Get("x-should-retry"))
	require.Equal(t, http.StatusBadRequest, w.Code)
}
