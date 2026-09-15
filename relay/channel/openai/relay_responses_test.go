package openai

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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

func TestResponsesStreamFailureBoundaries(t *testing.T) {
	created := `{"type":"response.created","sequence_number":0,"response":{"id":"resp_original"}}`
	failed := `{"type":"response.failed","response":{"id":"resp_original","error":{"code":"server_error","message":"upstream failed"},"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`
	for _, tc := range []struct {
		name    string
		events  []string
		started bool
		message string
	}{
		{"empty stream", nil, false, "before response.completed"},
		{"heartbeat only", []string{`{"type":"response.keepalive"}`}, false, "before response.completed"},
		{"created then EOF", []string{created}, true, "before response.completed"},
		{"malformed event", []string{created, `{broken`}, true, "invalid upstream Responses event"},
		{"failed", []string{created, failed, `{"type":"response.output_text.delta","delta":"must not be forwarded"}`}, true, "upstream failed"},
		{"incomplete", []string{created, `{"type":"response.incomplete","response":{"id":"resp_original","incomplete_details":{"reason":"max_output_tokens"}}}`}, true, "response.incomplete"},
		{"flat error", []string{created, `{"type":"error","code":"server_error","message":"flat failure"}`}, true, "flat failure"},
		{"first event failure", []string{failed}, false, "upstream failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(r)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			var body strings.Builder
			for _, event := range tc.events {
				body.WriteString("data: " + event + "\n\n")
			}
			resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body.String()))}
			usage, apiErr := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{}, resp)
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			require.Contains(t, apiErr.Error(), tc.message)
			require.Equal(t, tc.started, helper.ResponsesStreamStarted(c))
			require.Equal(t, tc.started, types.IsSkipRetryError(apiErr))
			require.NotContains(t, r.Body.String(), "must not be forwarded")
			require.NotContains(t, r.Body.String(), "event: response.failed", "controller owns final failure framing")
			if tc.name == "failed" {
				require.Equal(t, 12, usage.PromptTokens)
				require.Equal(t, 3, usage.CompletionTokens)
			}
		})
	}
}

// Exercise real TCP cancellation with the production ping interval. The upstream
// stays open after 15 non-text events; only closing the downstream ends the relay.
func TestResponsesStreamClientDisconnectIsNotUpstreamFailure(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	oldEnabled, oldInterval := settings.PingIntervalEnabled, settings.PingIntervalSeconds
	settings.PingIntervalEnabled, settings.PingIntervalSeconds = true, 15
	t.Cleanup(func() {
		settings.PingIntervalEnabled, settings.PingIntervalSeconds = oldEnabled, oldInterval
	})
	upstreamClosed := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamClosed)
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 15; i++ {
			fmt.Fprintf(w, "data: {\"type\":\"response.reasoning_summary_text.delta\",\"sequence_number\":%d,\"delta\":\"thinking\"}\n\n", i)
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()
	resp, err := http.Get(upstream.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	type result struct {
		usage *dto.Usage
		err   *types.NewAPIError
	}
	finished := make(chan result, 1)
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		usage, apiErr := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{}, resp)
		finished <- result{usage, apiErr}
	}))
	defer downstream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, downstream.URL+"/v1/responses", nil)
	require.NoError(t, err)
	clientResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer clientResp.Body.Close()
	scanner := bufio.NewScanner(clientResp.Body)
	for received := 0; received < 15; {
		require.True(t, scanner.Scan(), "expected all 15 events before disconnect")
		if strings.HasPrefix(scanner.Text(), "data:") {
			received++
		}
	}
	require.NoError(t, clientResp.Body.Close())
	select {
	case got := <-finished:
		require.NotNil(t, got.err)
		require.Equal(t, types.ErrorCodeClientDisconnected, got.err.GetErrorCode())
		require.Equal(t, 499, got.err.StatusCode)
		require.Zero(t, got.err.UpstreamStatusCode)
		require.True(t, types.IsSkipRetryError(got.err))
		require.False(t, types.IsRecordErrorLog(got.err))
		require.Zero(t, got.usage.TotalTokens)
	case <-ctx.Done():
		t.Fatal("relay did not stop after downstream disconnect")
	}
	select {
	case <-upstreamClosed:
	case <-ctx.Done():
		t.Fatal("relay did not close the upstream connection")
	}
}

func TestResponsesStreamStopsAtCompleted(t *testing.T) {
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := "data: " + `{"type":"response.completed","response":{"id":"resp_done","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}` + "\n\n" +
		"data: " + `{"type":"response.output_text.delta","delta":"late output"}` + "\n\n"
	resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
	usage, apiErr := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{}, resp)
	require.Nil(t, apiErr)
	require.Equal(t, 12, usage.TotalTokens)
	require.NotContains(t, r.Body.String(), "late output")
}

func TestResponsesStreamClosesUpstreamOnTimeoutOrTerminal(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 1
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, event := range []string{
		`{"type":"response.created","response":{"id":"resp_timeout"}}`,
		`{"type":"response.completed","response":{"id":"resp_done"}}`,
		`{"type":"response.failed","response":{"error":{"code":"server_error","message":"failed"}}}`,
	} {
		reader, writer := io.Pipe()
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		result := make(chan *types.NewAPIError, 1)
		go func() {
			_, err := OaiResponsesStreamHandler(c, &relaycommon.RelayInfo{DisablePing: true}, &http.Response{StatusCode: 200, Header: make(http.Header), Body: reader})
			result <- err
		}()
		_, err := io.WriteString(writer, "data: "+event+"\n\n")
		require.NoError(t, err)
		select {
		case apiErr := <-result:
			if strings.Contains(event, "response.completed") {
				require.Nil(t, apiErr)
			} else {
				require.NotNil(t, apiErr)
			}
		case <-time.After(5 * time.Second):
			_ = writer.Close()
			<-result
			t.Fatal("stream did not stop without upstream EOF")
		}
		_, err = writer.Write([]byte("late event"))
		require.ErrorIs(t, err, io.ErrClosedPipe)
		_ = writer.Close()
	}
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
