package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

func containsInsufficientQuotaKeyword(msg string) bool {
	if strings.TrimSpace(msg) == "" {
		return false
	}
	lower := strings.ToLower(msg)
	for _, keyword := range operation_setting.GetGlobalQuotaInsufficientKeywords() {
		if strings.Contains(lower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func ShouldMarkChannelQuotaInsufficient(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if err.GetErrorCode() == types.ErrorCodeInsufficientUserQuota ||
		err.GetErrorCode() == types.ErrorCodePreConsumeTokenQuotaFailed {
		return true
	}
	oai := err.ToOpenAIError()
	if containsInsufficientQuotaKeyword(oai.Type) ||
		containsInsufficientQuotaKeyword(oai.Message) ||
		containsInsufficientQuotaKeyword(strings.TrimSpace(anyToString(oai.Code))) {
		return true
	}
	if containsInsufficientQuotaKeyword(err.Error()) || containsInsufficientQuotaKeyword(err.ErrorWithStatusCode()) {
		return true
	}
	return false
}

func MarkChannelQuotaInsufficientDaily(channelID int, reason string) error {
	return model.MarkChannelQuotaInsufficientDaily(channelID, reason)
}

func ClearChannelQuotaInsufficientDailyMark(channelID int) error {
	return model.ClearChannelQuotaInsufficientDailyMark(channelID)
}

func CleanupExpiredChannelDailyMarks() error {
	return model.CleanupExpiredChannelDailyMarks()
}

func anyToString(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
