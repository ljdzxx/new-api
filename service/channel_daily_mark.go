package service

import (
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
)

var insufficientQuotaKeywords = []string{
	"insufficient",
	"insufficient_quota",
	"insufficient_user_quota",
	"quota",
	"\u4f59\u989d\u4e0d\u8db3",
	"\u989d\u5ea6\u4e0d\u8db3",
}

func containsInsufficientQuotaKeyword(msg string) bool {
	if strings.TrimSpace(msg) == "" {
		return false
	}
	lower := strings.ToLower(msg)
	for _, keyword := range insufficientQuotaKeywords {
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
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}
