package dto

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAdvancedCustomConfigValidation(t *testing.T) {
	valid := AdvancedCustomRoute{IncomingPath: "/v1beta/models/{model}:generateContent", UpstreamPath: "/v1/responses", Converter: AdvancedCustomGeminiToResponses}
	config := &AdvancedCustomConfig{Routes: []AdvancedCustomRoute{valid}}
	require.NoError(t, config.Validate())
	_, ok := config.MatchPath("/v1beta/models/gemini-3:streamGenerateContent?alt=sse")
	require.True(t, ok)
	_, ok = config.MatchPath("/v1/chat/completions")
	require.False(t, ok)
	for _, path := range []string{"//evil.example/path", "ftp://example.com/path", "https://user:password@example.com/path", "/v1/responses#fragment", "/v1/responses\r\nX: bad"} {
		route := valid
		route.UpstreamPath = path
		require.Error(t, (&AdvancedCustomConfig{Routes: []AdvancedCustomRoute{route}}).Validate())
	}
	require.Error(t, (&AdvancedCustomConfig{Routes: []AdvancedCustomRoute{valid, valid}}).Validate())
	require.Error(t, (*AdvancedCustomConfig)(nil).Validate())
}
