package gemini

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiUpstreamUsageExcludesPatchedCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, metadata string
		reported       bool
		input          int
	}{
		{name: "missing"},
		{name: "zero", metadata: `,"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0,"totalTokenCount":0}`, reported: true},
		{name: "input only", metadata: `,"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":0,"totalTokenCount":100}`, reported: true, input: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-2.5-flash"}}
			info.SetEstimatePromptTokens(200653)
			var response dto.GeminiChatResponse
			require.NoError(t, common.UnmarshalJsonStr(`{"candidates":[{"content":{"parts":[{"text":"answer"}]}}]`+tc.metadata+`}`, &response))
			buildUsageFromGeminiResponse(ctx, info, &response)
			require.Equal(t, tc.reported, info.UpstreamUsageReported)
			actual := info.GetUpstreamUsage()
			if !tc.reported {
				require.Nil(t, actual)
				return
			}
			require.Equal(t, tc.input, actual.PromptTokens)
			require.Zero(t, actual.CompletionTokens)
			if actual.BillingUsage != nil {
				require.False(t, actual.BillingUsage.Estimated)
				require.Zero(t, actual.BillingUsage.GeminiUsageMetadata.CandidatesTokenCount)
			}
		})
	}
}
