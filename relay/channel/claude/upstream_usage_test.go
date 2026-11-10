package claude

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaudeUpstreamUsageExcludesInterruptedStreamEstimates(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-4"}, RelayFormat: types.RelayFormatClaude}
		info.SetEstimatePromptTokens(200653)
		state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
		data := `{"type":"message_start","message":{"id":"msg_usage","model":"claude-sonnet-4","usage":{"input_tokens":100,"cache_read_input_tokens":20,"output_tokens":0}}}`
		if passthrough {
			updateClaudePassThroughUsage(ctx, info, state, []byte("data: "+data))
		} else {
			require.Nil(t, HandleStreamResponseData(ctx, info, state, data))
		}
		state.ResponseText.WriteString("partial output without reported completion usage")
		FinalizeStreamUsage(ctx, info, state)
		actual := info.GetUpstreamUsage()
		require.NotNil(t, actual)
		require.Equal(t, 100, actual.PromptTokens)
		require.Equal(t, 20, actual.PromptTokensDetails.CachedTokens)
		require.Zero(t, actual.CompletionTokens)
		require.Zero(t, actual.BillingUsage.ClaudeUsage.OutputTokens)
	}
}
