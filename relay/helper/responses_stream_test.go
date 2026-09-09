package helper

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesFailurePreservesIdentityAndTerminatesOnce(t *testing.T) {
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetEventStreamHeaders(c)
	require.NoError(t, ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.created"}, `{"type":"response.created","sequence_number":7,"response":{"id":"resp_original","model":"test-model","created_at":123}}`))
	err := types.NewOpenAIError(errors.New("upstream disconnected (request id: test)"), types.ErrorCodeBadResponse, 502)
	require.NoError(t, WriteResponsesStreamFailure(c, err))
	require.NoError(t, WriteResponsesStreamFailure(c, err))
	require.Equal(t, 200, r.Code)
	require.Equal(t, 1, strings.Count(r.Body.String(), "event: response.failed\n"))
	parts := strings.Split(r.Body.String(), "event: response.failed\ndata: ")
	var event struct {
		Type     string `json:"type"`
		Sequence int    `json:"sequence_number"`
		Response struct {
			ID     string            `json:"id"`
			Status string            `json:"status"`
			Model  string            `json:"model"`
			Error  types.OpenAIError `json:"error"`
		} `json:"response"`
	}
	require.NoError(t, common.UnmarshalJsonStr(strings.TrimSpace(parts[1]), &event))
	require.Equal(t, 8, event.Sequence)
	require.Equal(t, "resp_original", event.Response.ID)
	require.Equal(t, "test-model", event.Response.Model)
	require.Equal(t, "failed", event.Response.Status)
	// Codex's Responses SSE parser maps this code to non-retryable InvalidRequest.
	require.Equal(t, "invalid_prompt", event.Response.Error.Code)
	require.Contains(t, event.Response.Error.Message, "upstream disconnected")
	require.Contains(t, event.Response.Error.Message, "must not be replayed automatically")
	require.True(t, strings.HasSuffix(r.Body.String(), "\n\n"))
	require.NotContains(t, r.Body.String(), "[DONE]")
}

type partialResponsesWriter struct{ *httptest.ResponseRecorder }

func (w partialResponsesWriter) Write(data []byte) (int, error) {
	_, _ = w.ResponseRecorder.Write(data[:1])
	return 1, io.ErrClosedPipe
}

func TestResponsesPartialWriteStillLocksRetry(t *testing.T) {
	c, _ := gin.CreateTestContext(partialResponsesWriter{httptest.NewRecorder()})
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	err := ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.created"}, `{"type":"response.created","response":{"id":"resp_partial"}}`)
	require.ErrorIs(t, err, io.ErrClosedPipe)
	require.True(t, ResponsesStreamStarted(c))
}

func TestResponsesFailureDoesNotWriteAfterCancellationOrCompletion(t *testing.T) {
	for _, cancelRequest := range []bool{true, false} {
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		ctx, cancel := context.WithCancel(context.Background())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
		if cancelRequest {
			cancel()
		} else {
			require.NoError(t, ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.completed"}, `{"type":"response.completed","response":{"id":"resp_done"}}`))
		}
		before := r.Body.String()
		err := WriteResponsesStreamFailure(c, types.NewError(errors.New("late failure"), types.ErrorCodeBadResponse))
		if cancelRequest {
			require.ErrorIs(t, err, context.Canceled)
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, before, r.Body.String())
		cancel()
	}
}
