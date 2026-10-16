package vertex

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVertexGeminiFunctionIdentitySetting(t *testing.T) {
	settings := model_setting.GetGeminiSettings()
	original := settings.RemoveFunctionResponseIdEnabled
	t.Cleanup(func() { settings.RemoveFunctionResponseIdEnabled = original })
	for _, enabled := range []bool{true, false} {
		settings.RemoveFunctionResponseIdEnabled = enabled
		var request dto.GeminiChatRequest
		require.NoError(t, common.UnmarshalJsonStr("{\"contents\":[{\"parts\":[{\"functionCall\":{\"id\":\"call_1\",\"name\":\"lookup\",\"args\":{}}},{\"functionResponse\":{\"id\":\"call_1\",\"name\":\"lookup\",\"response\":{}}}]}],\"requests\":[{\"contents\":[{\"parts\":[{\"functionCall\":{\"id\":\"call_2\",\"name\":\"lookup\",\"args\":{}}}]}]}]}", &request))
		_, err := (&Adaptor{}).ConvertGeminiRequest(nil, &relaycommon.RelayInfo{}, &request)
		require.NoError(t, err)
		parts := request.Contents[0].Parts
		require.Equal(t, enabled, parts[0].FunctionCall.ID == nil)
		require.Equal(t, enabled, len(parts[1].FunctionResponse.ID) == 0)
		require.Equal(t, enabled, request.Requests[0].Contents[0].Parts[0].FunctionCall.ID == nil)
		require.Equal(t, "lookup", parts[0].FunctionCall.FunctionName)
	}
}

func TestVertexResponsesFunctionIdentitySetting(t *testing.T) {
	settings := model_setting.GetGeminiSettings()
	original := settings.RemoveFunctionResponseIdEnabled
	t.Cleanup(func() { settings.RemoveFunctionResponseIdEnabled = original })
	for _, enabled := range []bool{true, false} {
		settings.RemoveFunctionResponseIdEnabled = enabled
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVertexAi, UpstreamModelName: "gemini-3-flash-preview"}}
		var req dto.OpenAIResponsesRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-3-flash-preview","input":[{"type":"function_call","call_id":"a","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"a","output":"ok"}]}`, &req))
		a := Adaptor{}
		a.Init(info)
		converted, err := a.ConvertOpenAIResponsesRequest(c, info, req)
		require.NoError(t, err)
		request := converted.(*dto.GeminiChatRequest)
		require.Equal(t, enabled, request.Contents[0].Parts[0].FunctionCall.ID == nil)
		require.Equal(t, enabled, len(request.Contents[1].Parts[0].FunctionResponse.ID) == 0)
		require.Equal(t, req.Model, c.GetString("request_model"))
	}
}
