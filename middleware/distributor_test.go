package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
