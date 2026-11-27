package xiaomi

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestMimoRawToolsPreserveSchemaAndNumbers(t *testing.T) {
	const schema = `{"properties":{"z":{"const":9007199254740993},"a":{"enum":[18446744073709551615,1e-25]}}}`
	for _, appendTool := range []bool{false, true} {
		var req dto.ClaudeRequest
		require.NoError(t, common.Unmarshal([]byte(`{"model":"m","tools":[{"name":"f","input_schema":`+schema+`,"vendor":false},{"type":"web_search_20250305","name":"web_search"},{"type":"  ","name":"g","input_schema":`+schema+`}]}`), &req))
		if appendTool {
			req.AddTool(dto.Tool{Name: "extra", InputSchema: map[string]any{"type": "object"}})
		}
		for i := 0; i < 2; i++ {
			require.NoError(t, normalizeClaudeToolsForMimo(&req))
			encoded, err := common.Marshal(req)
			require.NoError(t, err)
			require.Equal(t, schema, gjson.GetBytes(encoded, "tools.0.input_schema").Raw)
			require.Equal(t, schema, gjson.GetBytes(encoded, "tools.2.input_schema").Raw)
			require.Equal(t, "false", gjson.GetBytes(encoded, "tools.0.vendor").Raw)
			require.Equal(t, "custom", gjson.GetBytes(encoded, "tools.0.type").String())
			require.Equal(t, "web_search_20250305", gjson.GetBytes(encoded, "tools.1.type").String())
			require.Equal(t, "custom", gjson.GetBytes(encoded, "tools.2.type").String())
			if appendTool {
				require.Equal(t, "custom", gjson.GetBytes(encoded, "tools.3.type").String())
			}
		}
	}
}
