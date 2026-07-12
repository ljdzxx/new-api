package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexCliPassThroughHeaders(t *testing.T) {
	required := []string{
		"Thread_id",
		"Session-Id",
		"Thread-Id",
		"X-Client-Request-Id",
		"X-Codex-Turn-State",
		"X-Codex-Window-Id",
		"X-Codex-Parent-Thread-Id",
		"X-OpenAI-Subagent",
		"X-OpenAI-Memgen-Request",
		"X-ResponsesAPI-Include-Timing-Metrics",
		"X-OpenAI-Internal-Codex-Responses-Lite",
	}

	for _, header := range required {
		require.Contains(t, codexCliPassThroughHeaders, header)
	}

	template := buildPassHeaderTemplate(codexCliPassThroughHeaders)
	operations := template["operations"].([]map[string]interface{})
	require.True(t, operations[0]["keep_origin"].(bool))
}
