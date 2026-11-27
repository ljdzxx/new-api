package aws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAwsClaudeWirePreservation(t *testing.T) {
	const schema = `{"properties":{"z":{"const":9007199254740993},"a":{"type":"string"}}}`
	const body = `{"model":"claude-test","stream":true,"max_tokens":10,"tools":[{"name":"f","input_schema":` + schema + `,"vendor":false}],"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"x","name":"f","input":{"id":9007199254740993,"nested":[18446744073709551615,1e-25]}}]}]}`
	for _, passthrough := range []bool{false, true} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		defer common.CleanupBodyStorage(ctx)
		headers := make(http.Header)
		headers.Add("anthropic-beta", "first,second")
		headers.Add("anthropic-beta", "third")
		req, err := formatRequest(strings.NewReader(body), headers)
		require.NoError(t, err)
		require.JSONEq(t, `["first","second","third"]`, string(req.AnthropicBeta))
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: passthrough},
		}}
		encoded, err := buildAwsRequestBody(ctx, info, req)
		require.NoError(t, err)
		require.Equal(t, schema, gjson.GetBytes(encoded, "tools.0.input_schema").Raw)
		require.Equal(t, "false", gjson.GetBytes(encoded, "tools.0.vendor").Raw)
		require.Equal(t, "9007199254740993", gjson.GetBytes(encoded, "messages.0.content.0.input.id").Raw)
		require.Equal(t, "18446744073709551615", gjson.GetBytes(encoded, "messages.0.content.0.input.nested.0").Raw)
		require.Equal(t, "1e-25", gjson.GetBytes(encoded, "messages.0.content.0.input.nested.1").Raw)
		require.False(t, gjson.GetBytes(encoded, "model").Exists())
		require.False(t, gjson.GetBytes(encoded, "stream").Exists())
		if !passthrough {
			require.Equal(t, "bedrock-2023-05-31", gjson.GetBytes(encoded, "anthropic_version").String())
		}
	}
}
