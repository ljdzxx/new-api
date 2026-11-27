package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIResponseToClaudePreservesToolNumbers(t *testing.T) {
	var response dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal([]byte(`{"id":"test","choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"f","arguments":"{\"id\":9007199254740993,\"nested\":[18446744073709551615]}"}}]}}]}`), &response))
	converted := ResponseOpenAI2Claude(&response, &relaycommon.RelayInfo{})
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	require.Equal(t, "9007199254740993", gjson.GetBytes(encoded, "content.0.input.id").Raw)
	require.Equal(t, "18446744073709551615", gjson.GetBytes(encoded, "content.0.input.nested.0").Raw)
}

func TestClaudeToOpenAIRequestPreservesToolSchemaOrder(t *testing.T) {
	const schema = `{"type":"object","properties":{"reasoning":{"type":"string"},"answer":{"type":"integer"}}}`
	var request dto.ClaudeRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"m","tools":[{"name":"f","input_schema":`+schema+`}]}`), &request))
	converted, err := ClaudeToOpenAIRequest(request, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	encoded, err := common.Marshal(converted)
	require.NoError(t, err)
	require.Equal(t, schema, gjson.GetBytes(encoded, "tools.0.function.parameters").Raw)
}

func TestClaudeCacheOnlyUsageRemainsBillable(t *testing.T) {
	for _, summary := range []textQuotaSummary{
		{IsClaudeUsageSemantic: true, CacheTokens: 123},
		{IsClaudeUsageSemantic: true, CacheCreationTokens: 123},
		{IsClaudeUsageSemantic: true, CacheCreationTokens5m: 123},
		{IsClaudeUsageSemantic: true, CacheCreationTokens1h: 123},
	} {
		require.True(t, hasBillableTokenUsage(summary))
	}
	require.False(t, hasBillableTokenUsage(textQuotaSummary{}))
}
