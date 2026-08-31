package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type modelMappedTestRequest struct {
	model string
	meta  *types.TokenCountMeta
}

func (r *modelMappedTestRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return r.meta
}

func (r *modelMappedTestRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *modelMappedTestRequest) SetModelName(modelName string) {
	r.model = modelName
}

func newModelMappedTestContext(modelMapping string) *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("model_mapping", modelMapping)
	return c
}

func newModelMappedTestInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: model,
		},
	}
}

func imageInputMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{
		Files: []*types.FileMeta{
			{FileType: types.FileTypeImage},
		},
	}
}

func TestModelMappedHelperNormalMappingIgnoresImageGuard(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.4":"gpt-5.3-codex-spark"}`)
	info := newModelMappedTestInfo("gpt-5.4")
	request := &modelMappedTestRequest{model: "gpt-5.4", meta: imageInputMeta()}

	err := ModelMappedHelper(c, info, request)

	require.NoError(t, err)
	require.True(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.3-codex-spark", info.UpstreamModelName)
	require.Equal(t, "gpt-5.3-codex-spark", request.model)
}

func TestModelMappedHelperConditionalMappingWithoutImageInput(t *testing.T) {
	c := newModelMappedTestContext(`{"!gpt-5.4":"gpt-5.3-codex-spark"}`)
	info := newModelMappedTestInfo("gpt-5.4")
	request := &modelMappedTestRequest{
		model: "gpt-5.4",
		meta:  &types.TokenCountMeta{},
	}

	err := ModelMappedHelper(c, info, request)

	require.NoError(t, err)
	require.True(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.3-codex-spark", info.UpstreamModelName)
	require.Equal(t, "gpt-5.3-codex-spark", request.model)
}

func TestModelMappedHelperConditionalMappingSkipsImageInput(t *testing.T) {
	c := newModelMappedTestContext(`{"!gpt-5.4":"gpt-5.3-codex-spark"}`)
	info := newModelMappedTestInfo("gpt-5.4")
	request := &modelMappedTestRequest{model: "gpt-5.4", meta: imageInputMeta()}

	err := ModelMappedHelper(c, info, request)

	require.NoError(t, err)
	require.False(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.4", info.UpstreamModelName)
	require.Equal(t, "gpt-5.4", request.model)
}

func TestModelMappedHelperStopsAfterSingleMapping(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.5":"gpt-5.4","!gpt-5.4":"gpt-5.3-codex-spark"}`)
	info := newModelMappedTestInfo("gpt-5.5")
	request := &modelMappedTestRequest{model: "gpt-5.5", meta: imageInputMeta()}

	err := ModelMappedHelper(c, info, request)

	require.NoError(t, err)
	require.True(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.4", info.UpstreamModelName)
	require.Equal(t, "gpt-5.4", request.model)
}

func TestModelMappedHelperConditionalMappingWithPlainFallback(t *testing.T) {
	const mapping = `{"!gpt-5.4":"gpt-5.3-codex-spark,xhigh","gpt-5.4":"gpt-5.6-luna"}`

	t.Run("request without image uses conditional rule", func(t *testing.T) {
		c := newModelMappedTestContext(mapping)
		info := newModelMappedTestInfo("gpt-5.4")
		request := &dto.OpenAIResponsesRequest{Model: "gpt-5.4", Input: []byte(`"hello"`)}

		err := ModelMappedHelper(c, info, request)

		require.NoError(t, err)
		require.True(t, info.IsModelMapped)
		require.Equal(t, "gpt-5.3-codex-spark", info.UpstreamModelName)
		require.Equal(t, "xhigh", info.ReasoningEffort)
		require.Equal(t, "gpt-5.3-codex-spark", request.Model)
		require.NotNil(t, request.Reasoning)
		require.Equal(t, "xhigh", request.Reasoning.Effort)
	})

	t.Run("request with image uses plain fallback", func(t *testing.T) {
		c := newModelMappedTestContext(mapping)
		info := newModelMappedTestInfo("gpt-5.4")
		request := &dto.OpenAIResponsesRequest{
			Model: "gpt-5.4",
			Input: []byte(`[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image.png"}]}]`),
		}

		err := ModelMappedHelper(c, info, request)

		require.NoError(t, err)
		require.True(t, info.IsModelMapped)
		require.Equal(t, "gpt-5.6-luna", info.UpstreamModelName)
		require.Equal(t, "", info.ReasoningEffort)
		require.Equal(t, "gpt-5.6-luna", request.Model)
		require.Nil(t, request.Reasoning)
	})
}

func TestModelMappedHelperChatCompletionImageUsesPlainFallback(t *testing.T) {
	c := newModelMappedTestContext(`{"!gpt-5.4":"gpt-5.3-codex-spark,xhigh","gpt-5.4":"gpt-5.6-luna"}`)
	info := newModelMappedTestInfo("gpt-5.4")
	request := &dto.GeneralOpenAIRequest{
		Model: "gpt-5.4",
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": "https://example.com/image.png"},
				},
			},
		}},
	}

	err := ModelMappedHelper(c, info, request)

	require.NoError(t, err)
	require.True(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.6-luna", info.UpstreamModelName)
	require.Equal(t, "gpt-5.6-luna", request.Model)
}

func TestGetMappedModelConditionalLayerPrecedence(t *testing.T) {
	modelMap := map[string]string{
		"!gpt-5.4,max":  "conditional-exact",
		"!gpt-5.4":      "conditional-fallback",
		"gpt-5.4,xhigh": "plain-exact",
		"gpt-5.4":       "plain-fallback",
	}
	tests := []struct {
		name       string
		effort     string
		needsImage bool
		want       string
	}{
		{name: "conditional exact", effort: "max", want: "conditional-exact"},
		{name: "plain exact before conditional fallback", effort: "xhigh", want: "plain-exact"},
		{name: "plain exact for image", effort: "xhigh", needsImage: true, want: "plain-exact"},
		{name: "plain fallback for image", effort: "high", needsImage: true, want: "plain-fallback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mappedModel, _, exists := getMappedModel(modelMap, "gpt-5.4", tt.effort, tt.needsImage)
			require.True(t, exists)
			require.Equal(t, tt.want, mappedModel)
		})
	}
}

func TestModelMappedHelperReasoningEffortPriorityAndOverride(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.4-mini,xhigh":"gpt-5.6-luna,max","gpt-5.4-mini":"fallback-model"}`)
	info := newModelMappedTestInfo("gpt-5.4-mini")
	request := &dto.GeneralOpenAIRequest{Model: "gpt-5.4-mini", ReasoningEffort: "xhigh"}
	err := ModelMappedHelper(c, info, request)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-luna", info.UpstreamModelName)
	require.Equal(t, "max", request.ReasoningEffort)
}

func TestModelMappedHelperRetryResetsPreviousChannelEffort(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.4-mini,xhigh":"first-target,max"}`)
	info := newModelMappedTestInfo("gpt-5.4-mini")
	firstRequest := &dto.GeneralOpenAIRequest{Model: "gpt-5.4-mini", ReasoningEffort: "xhigh"}
	require.NoError(t, ModelMappedHelper(c, info, firstRequest))
	require.Equal(t, "max", info.ReasoningEffort)

	c.Set("model_mapping", `{"gpt-5.4-mini":"retry-target"}`)
	retryRequest := &dto.GeneralOpenAIRequest{Model: "gpt-5.4-mini", ReasoningEffort: "xhigh"}
	require.NoError(t, ModelMappedHelper(c, info, retryRequest))
	require.Equal(t, "xhigh", info.ReasoningEffort)
	require.Equal(t, "xhigh", retryRequest.ReasoningEffort)
	require.Equal(t, "retry-target", retryRequest.Model)
}

func TestModelMappedHelperReasoningFallbackAndImageCondition(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.4-mini":"fallback-model"}`)
	info := newModelMappedTestInfo("gpt-5.4-mini")
	request := &dto.GeneralOpenAIRequest{Model: "gpt-5.4-mini", ReasoningEffort: "low"}
	err := ModelMappedHelper(c, info, request)
	require.NoError(t, err)
	require.Equal(t, "fallback-model", info.UpstreamModelName)

	c = newModelMappedTestContext(`{"!gpt-5.4-mini":"image-model"}`)
	info = newModelMappedTestInfo("gpt-5.4-mini")
	imageRequest := &modelMappedTestRequest{model: "gpt-5.4-mini", meta: imageInputMeta()}
	require.NoError(t, ModelMappedHelper(c, info, imageRequest))
	require.Equal(t, "gpt-5.4-mini", info.UpstreamModelName)
}

func TestModelMappedHelperInputTokenThreshold(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.4":"mapped-model"}`)
	info := newModelMappedTestInfo("gpt-5.4")
	info.ModelMappingInputTokenThresholdEnabled = true
	info.ModelMappingInputTokenThreshold = 100
	info.SetEstimatePromptTokens(99)
	request := &modelMappedTestRequest{model: "gpt-5.4", meta: &types.TokenCountMeta{}}
	require.NoError(t, ModelMappedHelper(c, info, request))
	require.Equal(t, "gpt-5.4", info.UpstreamModelName)
	info.SetEstimatePromptTokens(100)
	require.NoError(t, ModelMappedHelper(c, info, request))
	require.Equal(t, "mapped-model", info.UpstreamModelName)
}

func TestProductionModelMappingConfigSolXHigh(t *testing.T) {
	c := newModelMappedTestContext(`{
  "gpt-5.4-mini": "gpt-5.6-luna",
  "gpt-5.4,low": "gpt-5.6-luna,medium",
  "gpt-5.4,medium": "gpt-5.6-luna,max",
  "gpt-5.4,high": "gpt-5.6-luna,max",
  "gpt-5.4,xhigh": "gpt-5.6-luna,max",
  "gpt-5.4,max": "gpt-5.6-luna,max",
  "gpt-5.4": "gpt-5.6-luna",
  "gpt-5.5": "gpt-5.6-terra",
  "gpt-5.6-terra,low": "gpt-5.6-luna,medium",
  "gpt-5.6-terra,medium": "gpt-5.6-luna,max",
  "gpt-5.6-terra,high": "gpt-5.6-luna,max",
  "gpt-5.6-terra,xhigh": "gpt-5.6-luna,max",
  "gpt-5.6-terra,max": "gpt-5.6-luna,max",
  "gpt-5.6-terra": "gpt-5.6-luna,max",
  "gpt-5.6-sol,low": "gpt-5.6-terra,medium",
  "gpt-5.6-sol,medium": "gpt-5.6-terra,high",
  "gpt-5.6-sol,high": "gpt-5.6-terra,xhigh",
  "gpt-5.6-sol,xhigh": "gpt-5.6-terra,max",
  "gpt-5.6-sol,max": "gpt-5.6-terra,max",
  "gpt-5.6-sol": "gpt-5.6-terra"
}`)
	info := newModelMappedTestInfo("gpt-5.6-sol")
	info.ModelMappingInputTokenThresholdEnabled = true
	info.ModelMappingInputTokenThreshold = 25000
	info.SetEstimatePromptTokens(156426)
	request := &dto.OpenAIResponsesRequest{
		Model:     "gpt-5.6-sol",
		Reasoning: &dto.Reasoning{Effort: "xhigh"},
	}

	require.NoError(t, ModelMappedHelper(c, info, request))
	require.True(t, info.IsModelMapped)
	require.Equal(t, "gpt-5.6-terra", info.UpstreamModelName)
	require.Equal(t, "xhigh", info.ClientReasoningEffort)
	require.Equal(t, "max", request.Reasoning.Effort)
}

func TestApplyModelMappingToPassthroughBodyPreservesUnknownFields(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.6-sol,xhigh":"gpt-5.6-terra,max"}`)
	info := newModelMappedTestInfo("gpt-5.6-sol")
	request := &dto.GeneralOpenAIRequest{
		Model:           "gpt-5.6-sol",
		ReasoningEffort: "xhigh",
	}
	require.NoError(t, ModelMappedHelper(c, info, request))

	body := []byte(`{"model":"gpt-5.6-sol","reasoning_effort":"xhigh","reasoning":{"effort":"xhigh","vendor_flag":true},"provider_extension":{"keep":"yes"}}`)
	patched, err := ApplyModelMappingToPassthroughBody(body, info, request)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-terra", gjson.GetBytes(patched, "model").String())
	require.Equal(t, "max", gjson.GetBytes(patched, "reasoning_effort").String())
	require.Equal(t, "max", gjson.GetBytes(patched, "reasoning.effort").String())
	require.True(t, gjson.GetBytes(patched, "reasoning.vendor_flag").Bool())
	require.Equal(t, "yes", gjson.GetBytes(patched, "provider_extension.keep").String())
}

func TestApplyModelMappingToPassthroughBodyAddsResponsesEffort(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.6-sol":"gpt-5.6-terra,max"}`)
	info := newModelMappedTestInfo("gpt-5.6-sol")
	request := &dto.OpenAIResponsesRequest{Model: "gpt-5.6-sol"}
	require.NoError(t, ModelMappedHelper(c, info, request))

	body := []byte(`{"model":"gpt-5.6-sol","provider_extension":{"keep":1}}`)
	patched, err := ApplyModelMappingToPassthroughBody(body, info, request)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-terra", gjson.GetBytes(patched, "model").String())
	require.Equal(t, "max", gjson.GetBytes(patched, "reasoning.effort").String())
	require.Equal(t, int64(1), gjson.GetBytes(patched, "provider_extension.keep").Int())
}

func TestApplyModelMappingToPassthroughCompactionStripsReasoning(t *testing.T) {
	c := newModelMappedTestContext(`{"gpt-5.6-sol,xhigh":"gpt-5.6-terra,max"}`)
	info := newModelMappedTestInfo("gpt-5.6-sol-openai-compact")
	info.RelayMode = relayconstant.RelayModeResponsesCompact
	request := &dto.OpenAIResponsesRequest{
		Model:     "gpt-5.6-sol-openai-compact",
		Reasoning: &dto.Reasoning{Effort: "xhigh"},
	}
	require.NoError(t, ModelMappedHelper(c, info, request))

	body := []byte(`{"model":"gpt-5.6-sol-openai-compact","reasoning":{"effort":"xhigh"},"input":"keep"}`)
	patched, err := ApplyModelMappingToPassthroughBody(body, info, request)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-terra", gjson.GetBytes(patched, "model").String())
	require.False(t, gjson.GetBytes(patched, "reasoning").Exists())
	require.Equal(t, "keep", gjson.GetBytes(patched, "input").String())
}

func TestApplyModelMappingToPassthroughBodyGeminiThinkingKeepsFieldStyle(t *testing.T) {
	c := newModelMappedTestContext(`{"gemini-2.5-pro,low":"gemini-2.5-pro,high"}`)
	info := newModelMappedTestInfo("gemini-2.5-pro")
	request := &dto.GeminiChatRequest{}
	request.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{ThinkingLevel: "low"}
	require.NoError(t, ModelMappedHelper(c, info, request))

	body := []byte(`{"generationConfig":{"thinkingConfig":{"thinkingLevel":"low","vendorFlag":true},"providerExtension":{"keep":true}}}`)
	patched, err := ApplyModelMappingToPassthroughBody(body, info, request)
	require.NoError(t, err)
	require.Equal(t, "high", gjson.GetBytes(patched, "generationConfig.thinkingConfig.thinkingLevel").String())
	require.True(t, gjson.GetBytes(patched, "generationConfig.thinkingConfig.vendorFlag").Bool())
	require.True(t, gjson.GetBytes(patched, "generationConfig.providerExtension.keep").Bool())
}
