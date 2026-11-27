package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeToolsPreserveSchemaOrder(t *testing.T) {
	const schema = `{"type":"object","properties":{"reasoning":{"type":"string"},"answer":{"type":"integer","maximum":9007199254740993}},"required":["reasoning","answer"]}`
	var req ClaudeRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"claude-sonnet-5-5","tools":[{"name":"result","input_schema":`+schema+`,"vendor":true}],"stream":false,"temperature":0}`), &req))
	for _, add := range []bool{false, true} {
		copy, err := common.DeepCopy(&req)
		require.NoError(t, err)
		if add {
			copy.AddTool(Tool{Name: "extra", InputSchema: map[string]any{"type": "object"}})
		}
		encoded, err := common.Marshal(copy)
		require.NoError(t, err)
		require.Equal(t, schema, gjson.GetBytes(encoded, "tools.0.input_schema").Raw)
		require.True(t, gjson.GetBytes(encoded, "tools.0.vendor").Bool())
		require.Equal(t, "false", gjson.GetBytes(encoded, "stream").Raw)
		require.Equal(t, "0", gjson.GetBytes(encoded, "temperature").Raw)
		require.NotEmpty(t, copy.GetTools())
	}
	for _, body := range []string{`{"model":"m"}`, `{"model":"m","tools":null}`} {
		require.NoError(t, common.Unmarshal([]byte(body), &req))
		require.Nil(t, req.Tools)
		encoded, err := common.Marshal(req)
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(encoded, "tools").Exists())
	}
}

func TestClaudeToolInputNumbersSurviveParseContent(t *testing.T) {
	var req ClaudeRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"m","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"a","name":"f","input":{"id":9007199254740993,"nested":[18446744073709551615]}}]}]}`), &req))
	content, err := req.Messages[0].ParseContent()
	require.NoError(t, err)
	encoded, err := common.Marshal(content)
	require.NoError(t, err)
	require.Equal(t, "9007199254740993", gjson.GetBytes(encoded, "0.input.id").Raw)
	require.Equal(t, "18446744073709551615", gjson.GetBytes(encoded, "0.input.nested.0").Raw)
}

func TestOpenAIToolSchemaTokenMetadataUsesJSONText(t *testing.T) {
	const schema = `{"type":"object","properties":{"reasoning":{"type":"string"},"answer":{"type":"integer"}}}`
	var req GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"m","tools":[{"type":"function","function":{"name":"f","parameters":`+schema+`}}]}`), &req))
	require.Contains(t, req.GetTokenCountMeta().CombineText, schema)
	for _, body := range []string{`{"name":"f"}`, `{"name":"f","parameters":null}`} {
		var fn FunctionRequest
		require.NoError(t, common.Unmarshal([]byte(body), &fn))
		require.Nil(t, fn.Parameters)
	}
}

func TestClaudeEstimateIncludesWireAndAppendedTools(t *testing.T) {
	const schema = `{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`
	var req ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"tools":[{"name":"lookup","description":"Lookup a city","input_schema":`+schema+`},{"type":"web_search_20250305","name":"web_search","user_location":{"city":"Berlin"}}]}`, &req))
	meta := req.GetTokenCountMeta()
	require.Equal(t, 2, meta.ToolsCount)
	for _, text := range []string{"lookup", "Lookup a city", schema, "web_search", "Berlin"} {
		require.Contains(t, meta.CombineText, text)
	}
	req.AddTool(Tool{Name: "appended"})
	meta = req.GetTokenCountMeta()
	require.Equal(t, 3, meta.ToolsCount)
	require.Contains(t, meta.CombineText, schema)
	require.Contains(t, meta.CombineText, "appended")
}
