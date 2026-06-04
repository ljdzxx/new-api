package relay

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesHelperRejectsXiaomiChannel(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 3)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeXiaomi)

	err := ResponsesHelper(ctx, &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request: &dto.OpenAIResponsesRequest{
			Model: "mimo-vl",
		},
	})

	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Equal(t, types.ErrorCodeInvalidRequest, err.GetErrorCode())
	require.Equal(t, "渠道 #3 暂不支持走OpenAI /v1/responses 协议", err.Error())
}

func TestIsEncryptedContentRelayError(t *testing.T) {
	t.Parallel()

	require.False(t, isEncryptedContentRelayError(nil))
	require.False(t, isEncryptedContentRelayError(types.NewOpenAIError(
		errors.New("bad request"),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)))
	require.True(t, isEncryptedContentRelayError(types.NewOpenAIError(
		errors.New("The encrypted content could not be verified. Reason: Encrypted content could not be decrypted or parsed."),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)))
	require.True(t, isEncryptedContentRelayError(types.NewOpenAIError(
		errors.New("Missing required parameter: 'input[0].encrypted_content'."),
		types.ErrorCodeBadResponseStatusCode,
		http.StatusBadRequest,
	)))
}
