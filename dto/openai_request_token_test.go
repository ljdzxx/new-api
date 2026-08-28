package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesTokenMetaKeepsUnknownInputItems(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: json.RawMessage(`[{"type":"message","content":[{"type":"output_text","text":"historical assistant content"}]},{"type":"compaction","encrypted_content":"opaque-history-payload"}]`),
	}

	meta := req.GetTokenCountMeta()
	require.NotNil(t, meta)
	require.True(t, strings.Contains(meta.CombineText, "historical assistant content"))
	require.True(t, strings.Contains(meta.CombineText, "opaque-history-payload"))
}
