package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiThinkingPassthroughPreservesExplicitFalse(t *testing.T) {
	for _, body := range []string{
		"{\"generationConfig\":{\"thinkingConfig\":{\"includeThoughts\":true}},\"extension\":1}",
		"{\"generation_config\":{\"thinking_config\":{\"include_thoughts\":true}},\"extension\":1}",
	} {
		got, err := setGeminiThinkingConfig([]byte(body), &dto.GeminiThinkingConfig{IncludeThoughts: common.GetPointer(false)})
		require.NoError(t, err)
		path := "generationConfig.thinkingConfig.includeThoughts"
		if gjson.Get(body, "generation_config").Exists() {
			path = "generation_config.thinking_config.include_thoughts"
		}
		require.Equal(t, "false", gjson.GetBytes(got, path).Raw)
		require.EqualValues(t, 1, gjson.GetBytes(got, "extension").Int())
	}
}
