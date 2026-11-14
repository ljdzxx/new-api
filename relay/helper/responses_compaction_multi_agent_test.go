package helper

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesCompactionRejectsHostedState(t *testing.T) {
	for _, raw := range []string{
		`{"model":"m","multi_agent":{"enabled":true},"input":[]}`,
		`{"model":"m","multi_agent":{"enabled":false},"input":[]}`,
		`{"model":"m","input":[{"type":"agent_message","content":"opaque"}]}`,
		`{"model":"m","input":[{"type":"multi_agent_call","id":"opaque"}]}`,
		`{"model":"m","input":[{"type":"multi_agent_call_output","id":"opaque"}]}`,
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses/compact", strings.NewReader(raw))
		c.Request.Header.Set("Content-Type", "application/json")
		_, err := GetAndValidateResponsesCompactionRequest(c)
		require.ErrorContains(t, err, "/v1/responses/compact")
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses/compact", strings.NewReader(`{"model":"m","input":[{"type":"function_call_output","call_id":"x","output":"ok"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	_, err := GetAndValidateResponsesCompactionRequest(c)
	require.NoError(t, err)
}
