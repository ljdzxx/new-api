package relay

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesRequestFromCompactionUsesUpstreamWhitelist(t *testing.T) {
	t.Parallel()

	req := &dto.OpenAIResponsesCompactionRequest{
		Model:                "gpt-5.6-sol",
		Input:                json.RawMessage(`"input"`),
		Instructions:         json.RawMessage(`"instructions"`),
		PreviousResponseID:   "resp-1",
		Tools:                json.RawMessage(`[]`),
		ParallelToolCalls:    json.RawMessage(`false`),
		Reasoning:            &dto.Reasoning{Effort: "high"},
		ServiceTier:          "default",
		PromptCacheKey:       json.RawMessage(`"cache-1"`),
		PromptCacheOptions:   json.RawMessage(`{"type":"ephemeral"}`),
		PromptCacheRetention: json.RawMessage(`"24h"`),
		Text:                 json.RawMessage(`{"format":{"type":"text"}}`),
	}

	converted := openAIResponsesRequestFromCompaction(req)

	require.Equal(t, req.Model, converted.Model)
	require.Equal(t, req.Input, converted.Input)
	require.Equal(t, req.Instructions, converted.Instructions)
	require.Equal(t, req.PreviousResponseID, converted.PreviousResponseID)
	require.Equal(t, req.ParallelToolCalls, converted.ParallelToolCalls)
	require.Equal(t, req.ServiceTier, converted.ServiceTier)
	require.Equal(t, req.PromptCacheKey, converted.PromptCacheKey)
	require.Equal(t, req.PromptCacheOptions, converted.PromptCacheOptions)
	require.Equal(t, req.PromptCacheRetention, converted.PromptCacheRetention)
	require.Nil(t, converted.Tools)
	require.NotNil(t, converted.Reasoning)
	require.Equal(t, "high", converted.Reasoning.Effort)
	// The internal mapping copy must not mutate the client request.
	require.Equal(t, "high", req.Reasoning.Effort)
	require.Nil(t, converted.Text)
}

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

func TestCompactionModelMappingUsesClientReasoningBeforeUpstreamStrip(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	ctx.Set("model_mapping", `{"gpt-5.6-sol,xhigh":"gpt-5.6-terra,max"}`)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.6-sol-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ModelMappingInputTokenThresholdEnabled: true,
			ModelMappingInputTokenThreshold:        25000,
		},
	}
	info.SetEstimatePromptTokens(156426)
	request := openAIResponsesRequestFromCompaction(&dto.OpenAIResponsesCompactionRequest{
		Model:     "gpt-5.6-sol-openai-compact",
		Reasoning: &dto.Reasoning{Effort: "xhigh"},
	})

	require.NoError(t, helper.ModelMappedHelper(ctx, info, request))
	require.True(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.6-terra", info.UpstreamModelName)
	require.Equal(t, "xhigh", info.ClientReasoningEffort)
	require.Equal(t, "max", info.ReasoningEffort)
	require.Equal(t, "max", request.Reasoning.Effort)

	// This field is used only for matching and must not be sent to the
	// compaction upstream endpoint.
	request.Reasoning = nil
	require.Nil(t, request.Reasoning)
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
