package service

import (
	"fmt"
	"net/http"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsSubscriptionUnsupportedGroupError(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(
		fmt.Errorf("订阅套餐不支持当前分组: 套餐不支持在 pro 下使用"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)

	require.True(t, isSubscriptionUnsupportedGroupError(apiErr))
	require.False(t, isSubscriptionUnsupportedGroupError(types.NewErrorWithStatusCode(
		fmt.Errorf("invalid model"),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
	)))
}

func TestLocalQuotaErrorsUseBadRequestStatus(t *testing.T) {
	require.Equal(t, http.StatusBadRequest, types.NewErrorWithStatusCode(
		fmt.Errorf("用户额度不足, 剩余额度: ＄0.000000"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	).StatusCode)

	require.Equal(t, http.StatusBadRequest, types.NewErrorWithStatusCode(
		fmt.Errorf("token quota is not enough"),
		types.ErrorCodePreConsumeTokenQuotaFailed,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	).StatusCode)
}

func TestNewBillingSessionRejectsChannelWithoutPaymentMethods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	session, apiErr := NewBillingSession(ctx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			AllowSubscription: false,
			AllowWallet:       false,
		},
	}, 1)

	require.Nil(t, session)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
}
