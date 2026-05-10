package service

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestShouldMarkChannelQuotaInsufficientByConfiguredKeywords(t *testing.T) {
	original := operation_setting.GetMonitorSetting().GlobalQuotaInsufficientKeywords
	operation_setting.GetMonitorSetting().GlobalQuotaInsufficientKeywords = []string{"余额不足"}
	t.Cleanup(func() {
		operation_setting.GetMonitorSetting().GlobalQuotaInsufficientKeywords = original
	})

	err := types.WithOpenAIError(types.OpenAIError{
		Message: "上游返回: 当前账户余额不足，请充值",
		Type:    "upstream_error",
		Code:    "bad_request",
	}, 400)

	require.True(t, ShouldMarkChannelQuotaInsufficient(err))
	require.True(t, ShouldMatchGlobalQuotaInsufficientKeyword(err))
}

func TestShouldMatchGlobalQuotaInsufficientKeywordOnlyByConfiguredKeywords(t *testing.T) {
	original := operation_setting.GetMonitorSetting().GlobalQuotaInsufficientKeywords
	operation_setting.GetMonitorSetting().GlobalQuotaInsufficientKeywords = []string{"upstream_balance_empty"}
	t.Cleanup(func() {
		operation_setting.GetMonitorSetting().GlobalQuotaInsufficientKeywords = original
	})

	err := types.NewError(errors.New("local quota exhausted"), types.ErrorCodeInsufficientUserQuota)
	require.True(t, ShouldMarkChannelQuotaInsufficient(err))
	require.False(t, ShouldMatchGlobalQuotaInsufficientKeyword(err))
}

func TestShouldMarkChannelQuotaInsufficientByErrorCodeAndMessageCode(t *testing.T) {
	t.Run("new api error code insufficient_user_quota", func(t *testing.T) {
		err := types.NewError(errors.New("quota exceeded"), types.ErrorCodeInsufficientUserQuota)
		require.True(t, ShouldMarkChannelQuotaInsufficient(err))
	})

	t.Run("openai code insufficient_quota", func(t *testing.T) {
		original := operation_setting.GetMonitorSetting().GlobalQuotaInsufficientKeywords
		operation_setting.GetMonitorSetting().GlobalQuotaInsufficientKeywords = []string{"insufficient_quota"}
		t.Cleanup(func() {
			operation_setting.GetMonitorSetting().GlobalQuotaInsufficientKeywords = original
		})

		err := types.WithOpenAIError(types.OpenAIError{
			Message: "quota exceeded",
			Type:    "upstream_error",
			Code:    "insufficient_quota",
		}, 429)
		require.True(t, ShouldMarkChannelQuotaInsufficient(err))
	})
}
