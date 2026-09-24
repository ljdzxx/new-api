package common

import (
	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// MarkResponsesEncryptedFailure classifies a failed request for usage logs;
// it does not change retry decisions. Inspect only typed input items, including
// for body passthrough, and only on a logged 5xx failure. No payload is cached.
// Use the original input: a recovery may already have removed its reasoning.
func MarkResponsesEncryptedFailure(c *gin.Context, input []byte, apiErr *types.NewAPIError) {
	if c == nil || apiErr == nil || apiErr.StatusCode < 500 || apiErr.StatusCode > 599 {
		return
	}
	items := gjson.ParseBytes(input)
	if !items.IsArray() {
		return
	}
	reasoning, compaction := false, false
	items.ForEach(func(_, item gjson.Result) bool {
		ciphertext := item.Get("encrypted_content")
		if ciphertext.Type != gjson.String || ciphertext.Str == "" {
			return true
		}
		switch item.Get("type").String() {
		case "reasoning":
			reasoning = true
		case "compaction":
			compaction = true
		}
		return !reasoning || !compaction
	})
	badges := basecommon.GetContextKeyStringSlice(c, constant.ContextKeyResponsesLogBadges)
	for _, flag := range []struct {
		code string
		set  bool
	}{{"E1", reasoning}, {"E2", compaction}} {
		if flag.set && !basecommon.StringsContains(badges, flag.code) {
			badges = append(badges, flag.code)
		}
	}
	basecommon.SetContextKey(c, constant.ContextKeyResponsesLogBadges, badges)
}
