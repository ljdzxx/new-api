package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type unexpectedEOFReader struct{}

func (unexpectedEOFReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

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

func TestOaiResponsesCompactionHandlerMapsTruncatedUpstreamBodyToBadGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(io.MultiReader(
			strings.NewReader(`{"id":"cmp_`),
			unexpectedEOFReader{},
		)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	usage, apiErr := OaiResponsesCompactionHandler(c, resp, info)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeReadResponseBodyFailed, apiErr.GetErrorCode())
}

func TestOaiResponsesHandlerScalesClientUsageWithoutMutatingBillingUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":101,"output_tokens":9,"total_tokens":110,"input_tokens_details":{"cached_tokens":11}}}`)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{}},
		PriceData:   types.PriceData{GlobalModelRatio: 1.25},
	}

	usage, apiErr := OaiResponsesHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 101, usage.PromptTokens)
	assert.Equal(t, 9, usage.CompletionTokens)
	assert.Equal(t, 11, usage.PromptTokensDetails.CachedTokens)
	assert.EqualValues(t, 126, gjson.Get(recorder.Body.String(), "usage.input_tokens").Int())
	assert.EqualValues(t, 11, gjson.Get(recorder.Body.String(), "usage.output_tokens").Int())
	assert.EqualValues(t, 137, gjson.Get(recorder.Body.String(), "usage.total_tokens").Int())
	assert.EqualValues(t, 13, gjson.Get(recorder.Body.String(), "usage.input_tokens_details.cached_tokens").Int())
}

func TestOaiResponsesHandlerScalesClientUsageForPassThroughChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":101,"output_tokens":9,"total_tokens":110}}`)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true}},
		PriceData:   types.PriceData{GlobalModelRatio: 2},
	}

	_, apiErr := OaiResponsesHandler(c, info, resp)
	require.Nil(t, apiErr)
	assert.EqualValues(t, 202, gjson.Get(recorder.Body.String(), "usage.input_tokens").Int())
	assert.EqualValues(t, 18, gjson.Get(recorder.Body.String(), "usage.output_tokens").Int())
	assert.EqualValues(t, 220, gjson.Get(recorder.Body.String(), "usage.total_tokens").Int())
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
