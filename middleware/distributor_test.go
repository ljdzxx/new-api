package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
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

func TestDistributeRejectsImageModelOnNonImageEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	original := common.DisableImageGenerationOnNonImageEndpoints
	common.DisableImageGenerationOnNonImageEndpoints = true
	t.Cleanup(func() {
		common.DisableImageGenerationOnNonImageEndpoints = original
	})

	for _, path := range []string{"/v1/chat/completions", "/pg/chat/completions", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			nextCalled := false
			router := gin.New()
			router.POST(path, Distribute(), func(c *gin.Context) {
				nextCalled = true
			})
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"model":"gpt-image-2"}`))
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Equal(t, "false", w.Header().Get("x-should-retry"))
			require.Contains(t, w.Body.String(), "gpt-image-2")
			require.False(t, nextCalled)
		})
	}
}

func TestImageModelEndpointRestriction(t *testing.T) {
	original := common.DisableImageGenerationOnNonImageEndpoints
	t.Cleanup(func() {
		common.DisableImageGenerationOnNonImageEndpoints = original
	})

	common.DisableImageGenerationOnNonImageEndpoints = true
	require.True(t, shouldRejectImageGenerationModel("/v1/chat/completions", "gpt-image-2"))
	require.False(t, shouldRejectImageGenerationModel("/v1/images/generations", "gpt-image-2"))
	require.False(t, shouldRejectImageGenerationModel("/v1/images/edits", "gpt-image-2"))
	require.False(t, shouldRejectImageGenerationModel("/v1/chat/completions", "gpt-4.1"))

	common.DisableImageGenerationOnNonImageEndpoints = false
	require.False(t, shouldRejectImageGenerationModel("/v1/chat/completions", "gpt-image-2"))
}

func TestDistributeRejectsCompactVirtualModelOnNormalEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())

	for _, path := range []string{"/v1/chat/completions", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			nextCalled := false
			router := gin.New()
			router.POST(path, Distribute(), func(c *gin.Context) {
				nextCalled = true
			})
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"model":"gpt-5.4-openai-compact"}`))
			req.Header.Set("Content-Type", "application/json")

			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusBadRequest, w.Code)
			require.Equal(t, "false", w.Header().Get("x-should-retry"))
			require.Contains(t, w.Body.String(), "gpt-5.4-openai-compact")
			require.False(t, nextCalled)
		})
	}
}

func TestCompactVirtualModelEndpointRestriction(t *testing.T) {
	require.True(t, shouldRejectClientCompactModel("/v1/chat/completions", "gpt-5.4-openai-compact"))
	require.True(t, shouldRejectClientCompactModel("/v1/responses", "gpt-5.4-openai-compact"))
	require.True(t, shouldRejectClientCompactModel("/v1/responses/compact", "gpt-5.4-openai-compact"))
	require.False(t, shouldRejectClientCompactModel("/v1/responses", "gpt-5.4"))
	require.False(t, shouldRejectClientCompactModel("/v1/responses/compact", "gpt-5.4"))
}

func TestGetModelRequestCapturesCompactClientModelBeforeRoutingSuffix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewBufferString(`{"model":"gpt-5.4"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	request, _, err := getModelRequest(ctx)

	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", common.GetContextKeyString(ctx, constant.ContextKeyClientModel))
	require.Equal(t, "gpt-5.4-openai-compact", request.Model)
	require.False(t, shouldRejectClientCompactModel(
		ctx.Request.URL.Path,
		common.GetContextKeyString(ctx, constant.ContextKeyClientModel),
	))
}

func TestDistributeRejectsCompactVirtualModelOnCompactEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	w := httptest.NewRecorder()
	nextCalled := false
	router := gin.New()
	router.POST("/v1/responses/compact", Distribute(), func(c *gin.Context) {
		nextCalled = true
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewBufferString(`{"model":"gpt-5.4-openai-compact"}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "false", w.Header().Get("x-should-retry"))
	require.Contains(t, w.Body.String(), "gpt-5.4-openai-compact")
	require.False(t, nextCalled)
}
