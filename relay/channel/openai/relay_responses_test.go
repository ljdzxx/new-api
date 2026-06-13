package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
