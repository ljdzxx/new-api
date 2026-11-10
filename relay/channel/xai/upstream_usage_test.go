package xai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStreamUpstreamUsagePreservesReportedDetails(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "grok-3"},
		RelayFormat: types.RelayFormatOpenAI, IsStream: true,
	}
	info.SetEstimatePromptTokens(200653)
	body := "data: " + `{"id":"chatcmpl_xai","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":10}}}` + "\n\ndata: [DONE]\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
	_, apiErr := xAIStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	actual := info.GetUpstreamUsage()
	require.NotNil(t, actual)
	require.Equal(t, 100, actual.PromptTokens)
	require.Equal(t, 20, actual.CompletionTokens)
	require.Equal(t, 40, actual.PromptTokensDetails.CachedTokens)
	require.Equal(t, 10, actual.CompletionTokenDetails.ReasoningTokens)
}
