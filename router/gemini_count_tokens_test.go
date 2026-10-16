package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiCountTokensRejectedBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetRelayRouter(r)
	for _, version := range []string{"v1", "v1beta"} {
		for _, auth := range []string{"", "Bearer invalid"} {
			req := httptest.NewRequest(http.MethodPost, "/"+version+"/models/gemini-2.5-flash:countTokens", nil)
			req.Header.Set("Authorization", auth)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, req)
			require.Equal(t, http.StatusNotFound, response.Code)
			require.Contains(t, response.Body.String(), "invalid_request_error")
		}
	}
	response := httptest.NewRecorder()
	r.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", nil))
	require.Equal(t, http.StatusUnauthorized, response.Code)
}
