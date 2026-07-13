package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendResponsesKeepAliveWritesIgnoredResponsesEvent(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	err := sendResponsesKeepAlive(c)
	require.NoError(t, err)

	body := recorder.Body.String()
	assert.Contains(t, body, "event: response.keepalive\n")
	assert.Contains(t, body, `data: {"type":"response.keepalive"}`)
	assert.True(t, strings.HasSuffix(body, "\n\n"), "SSE event must end with a blank line")
}

func TestOaiResponsesHandlerPreservesInputTokenDetailsForBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":100,"output_tokens":9,"total_tokens":109,"input_tokens_details":{"cached_tokens":70,"cache_write_tokens":6,"text_tokens":20,"image_tokens":3,"audio_tokens":1}}}`)),
	}

	usage, apiErr := OaiResponsesHandler(c, &relaycommon.RelayInfo{}, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 70, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 6, usage.PromptTokensDetails.CacheWriteTokens)
	require.NotNil(t, usage.BillingUsage)
	require.Equal(t, 70, usage.BillingUsage.OpenAIUsage.InputTokensDetails.CachedTokens)
}

func TestOaiResponsesStreamHandlerPreservesInputTokenDetailsForBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := "event: response.completed\n" +
		`data: {"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":9,"total_tokens":109,"input_tokens_details":{"cached_tokens":70,"cache_write_tokens":6,"text_tokens":20,"image_tokens":3,"audio_tokens":1}}}}` + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, apiErr := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{}, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 70, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 6, usage.PromptTokensDetails.CacheWriteTokens)
	require.NotNil(t, usage.BillingUsage)
	require.Equal(t, 70, usage.BillingUsage.OpenAIUsage.InputTokensDetails.CachedTokens)
}
