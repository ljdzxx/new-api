package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeminiArgonFieldsRoundTrip(t *testing.T) {
	for _, snake := range []bool{false, true} {
		continuation, media := "continuationToken", "mediaProcessing"
		if snake {
			continuation, media = "continuation_token", "media_processing"
		}
		payload := `{"` + continuation + `":{"opaque":false},"contents":[{"parts":[{"` + media + `":{"fps":0}}]}],"generationConfig":{"` + media + `":{"enabled":false},"topP":0}}`
		var request GeminiChatRequest
		require.NoError(t, common.UnmarshalJsonStr(payload, &request))
		wire, err := common.Marshal(request)
		require.NoError(t, err)
		for _, path := range []string{"continuationToken.opaque", "contents.0.parts.0.mediaProcessing.fps", "generationConfig.mediaProcessing.enabled", "generationConfig.topP"} {
			require.True(t, gjson.GetBytes(wire, path).Exists(), path)
		}
		var response GeminiChatResponse
		require.NoError(t, common.UnmarshalJsonStr(`{"candidates":[{"continuationToken":"opaque","finishReason":"CONTINUATION"}]}`, &response))
		wire, err = common.Marshal(response)
		require.NoError(t, err)
		require.Equal(t, "opaque", gjson.GetBytes(wire, "candidates.0.continuationToken").String())
	}
}
