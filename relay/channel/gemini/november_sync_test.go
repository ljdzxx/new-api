package gemini

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNovemberGeminiThinkingLevelsAndChannelPriority(t *testing.T) {
	for _, model := range []string{"gemini-4-argon", "gemini-4-argon-cyber-permissive", "gemini-4-hydrogen-preview", "gemini-4-neon", "gemini-4-future"} {
		for _, effort := range []string{"minimal", "low", "medium", "high", "xhigh", "max"} {
			for _, override := range []bool{false, true} {
				for _, format := range []string{"native", "chat", "responses", "claude"} {
					t.Run(fmt.Sprintf("%s/%s/%t/%s", model, effort, override, format), func(t *testing.T) {
						c, _, info := geminiSyncContext()
						info.OriginModelName, info.UpstreamModelName = model, model
						var req dto.Request
						switch format {
						case "native":
							req = &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: effort}}}
						case "chat":
							req = &dto.GeneralOpenAIRequest{Model: model, ReasoningEffort: effort}
						case "responses":
							req = &dto.OpenAIResponsesRequest{Model: model, Reasoning: &dto.Reasoning{Effort: effort}, Input: []byte(`"hi"`)}
						case "claude":
							req = &dto.ClaudeRequest{Model: model, OutputConfig: []byte(`{"effort":"` + effort + `"}`), Messages: []dto.ClaudeMessage{{Role: "user", Content: "hi"}}}
						}
						if override {
							c.Set("model_mapping", fmt.Sprintf(`{"%s":"%s,%s"}`, model, model, effort))
						}
						require.NoError(t, helper.ModelMappedHelper(c, info, req))
						var converted *dto.GeminiChatRequest
						switch r := req.(type) {
						case *dto.GeminiChatRequest:
							converted = r
							require.NoError(t, NormalizeNativeThinkingRequest(r, info))
						case *dto.GeneralOpenAIRequest:
							var err error
							converted, err = CovertOpenAI2Gemini(c, *r, info)
							require.NoError(t, err)
						case *dto.OpenAIResponsesRequest:
							var err error
							converted, err = ConvertResponsesToGemini(c, info, *r)
							require.NoError(t, err)
						case *dto.ClaudeRequest:
							value, err := (&Adaptor{}).ConvertClaudeRequest(c, info, r)
							require.NoError(t, err)
							converted = value.(*dto.GeminiChatRequest)
						}
						want := effort
						if !override {
							if effort == "minimal" {
								want = "low"
							}
							if effort == "xhigh" || effort == "max" {
								want = "high"
								if strings.Contains(model, "hydrogen") || strings.Contains(model, "neon") {
									want = "xhigh"
								}
							}
						}
						require.NotNil(t, converted.GenerationConfig.ThinkingConfig)
						require.Equal(t, want, converted.GenerationConfig.ThinkingConfig.ThinkingLevel)
						require.Nil(t, converted.GenerationConfig.ThinkingConfig.ThinkingBudget)
						require.Equal(t, model, info.OriginModelName)
					})
				}
			}
		}
	}
}

func TestNovemberGeminiChannelOverridePassthroughAndRetry(t *testing.T) {
	c, _, info := geminiSyncContext()
	info.OriginModelName = "gemini-4-argon"
	c.Set("model_mapping", `{"gemini-4-argon":"gemini-4-argon,max"}`)
	request := &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: "max", ThinkingBudget: common.GetPointer(100)}}}
	require.NoError(t, helper.ModelMappedHelper(c, info, request))
	require.NoError(t, NormalizeNativeThinkingRequest(request, info))
	for _, body := range []string{
		`{"generationConfig":{"thinkingConfig":{"thinkingLevel":"max","thinkingBudget":100,"vendorFlag":true}},"keep":true}`,
		`{"generation_config":{"thinking_config":{"thinking_level":"max","thinking_budget":100,"vendorFlag":true}},"keep":true}`,
	} {
		got, err := helper.ApplyModelMappingToPassthroughBody([]byte(body), info, request)
		require.NoError(t, err)
		require.Contains(t, string(got), `"max"`)
		require.NotContains(t, string(got), "100")
		require.Contains(t, string(got), `"vendorFlag":true`)
		require.Contains(t, string(got), `"keep":true`)
	}
	// A retry without a matching rule must use provider defaults again.
	c.Set("model_mapping", `{}`)
	require.NoError(t, helper.ModelMappedHelper(c, info, request))
	require.NoError(t, NormalizeNativeThinkingRequest(request, info))
	require.Empty(t, info.ChannelMappedReasoningEffort)
	require.Equal(t, "high", request.GenerationConfig.ThinkingConfig.ThinkingLevel)
	// Thresholded channel rules must remain inactive below the threshold.
	c.Set("model_mapping", `{"gemini-4-argon":"gemini-4-argon,max"}`)
	info.ChannelMeta.ModelMappingInputTokenThresholdEnabled = true
	info.ChannelMeta.ModelMappingInputTokenThreshold = 100
	info.SetEstimatePromptTokens(99)
	require.NoError(t, helper.ModelMappedHelper(c, info, request))
	require.Empty(t, info.ChannelMappedReasoningEffort)
}

func TestNovemberGeminiDefaultsPrecedeChannelRuleMatching(t *testing.T) {
	c, _, info := geminiSyncContext()
	info.OriginModelName = "gemini-4-argon"
	// max becomes high under the provider default, then the channel matches
	// high and explicitly selects max on a different model.
	c.Set("model_mapping", `{"gemini-4-argon,high":"gemini-4-neon,max","gemini-4-argon,max":"wrong-target"}`)
	request := &dto.GeneralOpenAIRequest{Model: info.OriginModelName, ReasoningEffort: "max"}
	require.NoError(t, helper.ModelMappedHelper(c, info, request))
	converted, err := CovertOpenAI2Gemini(c, *request, info)
	require.NoError(t, err)
	require.Equal(t, "gemini-4-neon", info.UpstreamModelName)
	require.Equal(t, "gemini-4-argon", info.OriginModelName)
	require.Equal(t, "max", info.ClientReasoningEffort)
	require.Equal(t, "high", info.ModelMappingCandidateEffort)
	require.Equal(t, "max", converted.GenerationConfig.ThinkingConfig.ThinkingLevel)
}

func TestNovemberEmbeddingMissingUsageFallsBack(t *testing.T) {
	c, _, info := geminiSyncContext()
	info.SetEstimatePromptTokens(17)
	for _, metadata := range []*dto.GeminiEmbeddingUsageMetadata{nil, {}, {PromptTokenCount: -1}} {
		require.Equal(t, 17, embeddingUsage(c, info, metadata).PromptTokens)
	}
}

func TestNovemberResponsesVerbatimAndContinuation(t *testing.T) {
	for _, stream := range []bool{false, true} {
		c, recorder, info := responsesTestContext()
		info.IsStream = stream
		payload := `{"candidates":[{"content":{"parts":[{"text":"{\"a"},{"text":"\":1}"},{"text":"\n"}]},"finishReason":"CONTINUATION"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`
		resp := geminiSSE(payload)
		if !stream {
			resp = &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload))}
		}
		_, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
		require.Nil(t, apiErr)
		result := gjson.Parse(recorder.Body.String())
		if stream {
			events := responseEvents(t, recorder.Body.String())
			result = events[len(events)-1].Get("response")
		}
		require.Equal(t, "{\"a\":1}\n", result.Get("output.0.content.0.text").String())
		require.Equal(t, "max_output_tokens", result.Get("incomplete_details.reason").String())
	}
}

func TestNovemberEmbeddingRawUsageAndThreeLevelScaling(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	original := settings.ResponseUsageScaleEnabled
	t.Cleanup(func() { settings.ResponseUsageScaleEnabled = original })
	for _, enabled := range []bool{false, true} {
		settings.ResponseUsageScaleEnabled = enabled
		for _, format := range []string{"single", "batch", "openai"} {
			for _, tokens := range []int{99, 100, 200, 300} {
				t.Run(fmt.Sprintf("%t/%s/%d", enabled, format, tokens), func(t *testing.T) {
					c, recorder, info := geminiSyncContext()
					info.UpstreamModelName = "gemini-embedding-001"
					info.IsGeminiBatchEmbedding = format != "single"
					info.PriceData = types.PriceData{GlobalRatioConfigSnapshot: true, GlobalModelRatio: 1,
						ConfiguredSystemGlobalModelRatio: 2, ConfiguredChannelModelRatio: 3, ConfiguredUserGlobalModelRatio: 0.5,
						SystemGlobalRatioThreshold: 100, ChannelGlobalRatioThreshold: 200, UserGlobalRatioThreshold: 300}
					info.SetEstimatePromptTokens(5)
					key, value := "embeddings", `[{"values":[0.5]}]`
					if format == "single" {
						key, value = "embedding", `{"values":[0.5]}`
					}
					body := fmt.Sprintf(`{"%s":%s,"usageMetadata":{"promptTokenCount":%d,"promptTokenDetails":[{"modality":"TEXT","tokenCount":%d}]},"extension":true}`, key, value, tokens, tokens)
					resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}
					var usage *dto.Usage
					var apiErr *types.NewAPIError
					if format == "openai" {
						usage, apiErr = GeminiEmbeddingHandler(c, info, resp)
					} else {
						usage, apiErr = NativeGeminiEmbeddingHandler(c, resp, info)
					}
					require.Nil(t, apiErr)
					require.Equal(t, tokens, usage.PromptTokens)
					require.Equal(t, tokens, usage.TotalTokens)
					require.Equal(t, tokens, usage.BillingUsage.GeminiUsageMetadata.PromptTokenCount)
					ratio := 1.0
					if enabled {
						if tokens >= 100 {
							ratio *= 2
						}
						if tokens >= 200 {
							ratio *= 3
						}
						if tokens >= 300 {
							ratio *= 0.5
						}
					}
					path := "usageMetadata.promptTokenCount"
					if format == "openai" {
						path = "usage.prompt_tokens"
					} else {
						require.EqualValues(t, int(float64(tokens)*ratio), gjson.Get(recorder.Body.String(), "usageMetadata.promptTokenDetails.0.tokenCount").Int())
						require.True(t, gjson.Get(recorder.Body.String(), "extension").Bool())
					}
					require.EqualValues(t, int(float64(tokens)*ratio), gjson.Get(recorder.Body.String(), path).Int())
				})
			}
		}
	}
}

func TestNovemberGeminiVerbatimTextAndGrounding(t *testing.T) {
	c, _, _ := geminiSyncContext()
	for _, parts := range [][]dto.GeminiPart{
		{{Text: `{"ke`}, {Text: `y":"va`}, {Text: `lue","n":1`}, {Text: `2}`}},
		{{Text: "你"}, {Text: "\n"}, {Text: "好"}, {}},
		{{Text: `{"escape":"\`}, {Text: `n"}`}},
	} {
		want := ""
		for _, part := range parts {
			want += part.Text
		}
		response := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Parts: parts}, FinishReason: common.GetPointer("CONTINUATION")}}}
		require.Equal(t, want, responseGeminiChat2OpenAI(c, response).Choices[0].Message.StringContent())
		stream, _ := streamResponseGeminiChat2OpenAI(response)
		require.Equal(t, want, stream.Choices[0].Delta.GetContentString())
		require.Equal(t, "length", *stream.Choices[0].FinishReason)
	}
	// A source text also occurs inside the code block. Cite its actual rendered position.
	metadata := &dto.GeminiGroundingMetadata{GroundingChunks: []byte(`[{"web":{"uri":"https://example.com"}}]`), GroundingSupports: []byte(`[{"segment":{"partIndex":1,"startIndex":0,"endIndex":1,"text":"1"},"groundingChunkIndices":[0]}]`)}
	candidate := dto.GeminiChatCandidate{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{ExecutableCode: &dto.GeminiPartExecutableCode{Language: "python", Code: "print(1)"}}, {Text: "1"}}}, GroundingMetadata: metadata}
	message := responseGeminiChat2OpenAI(c, &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{candidate}}).Choices[0].Message
	encoded, err := common.Marshal(message.Annotations)
	require.NoError(t, err)
	require.EqualValues(t, len([]rune(message.StringContent()))-1, gjson.GetBytes(encoded, "0.url_citation.start_index").Int())
	// Delayed stream metadata must locate a newline and text across several chunks.
	state := &geminiGroundingStreamState{}
	for _, text := range []string{"你", "\n", "好"} {
		response := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: text}}}}}}
		converted, _, parts := streamResponseGeminiChatWithParts(response)
		state.appendContent(response.Candidates[0].Content, parts[0], converted.Choices[0].Delta.GetContentString())
	}
	metadata.GroundingSupports = []byte(`[{"segment":{"partIndex":0,"startIndex":0,"endIndex":7,"text":"你\n好"},"groundingChunkIndices":[0]}]`)
	encoded, err = common.Marshal(state.annotations(metadata))
	require.NoError(t, err)
	require.EqualValues(t, 3, gjson.GetBytes(encoded, "0.url_citation.end_index").Int())
}

func TestNovemberGroundingRepeatedTextAndMediaNewline(t *testing.T) {
	c, _, _ := geminiSyncContext()
	for _, parts := range [][]dto.GeminiPart{
		{{Text: "ab"}, {Text: "ab"}},
		{{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "AAAA"}}, {Text: "\n"}, {Text: "你"}},
	} {
		index := len(parts) - 1
		text := parts[index].Text
		metadata := &dto.GeminiGroundingMetadata{GroundingChunks: []byte(`[{"web":{"uri":"https://example.com"}}]`), GroundingSupports: []byte(fmt.Sprintf(`[{"segment":{"partIndex":%d,"startIndex":0,"endIndex":%d},"groundingChunkIndices":[0]}]`, index, len(text)))}
		response := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Parts: parts}, GroundingMetadata: metadata}}}
		message := responseGeminiChat2OpenAI(c, response).Choices[0].Message
		encoded, err := common.Marshal(message.Annotations)
		require.NoError(t, err)
		require.EqualValues(t, len([]rune(message.StringContent()))-len([]rune(text)), gjson.GetBytes(encoded, "0.url_citation.start_index").Int())
		stream, _, locations := streamResponseGeminiChatWithParts(response)
		state := &geminiGroundingStreamState{}
		state.appendContent(response.Candidates[0].Content, locations[0], stream.Choices[0].Delta.GetContentString())
		encoded, err = common.Marshal(state.annotations(metadata))
		require.NoError(t, err)
		require.EqualValues(t, len([]rune(stream.Choices[0].Delta.GetContentString()))-len([]rune(text)), gjson.GetBytes(encoded, "0.url_citation.start_index").Int())
	}
	response := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{
		{Index: 0, Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{FunctionName: "lookup"}}}}},
		{Index: 1, Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: "plain"}}}},
	}}
	got := responseGeminiChat2OpenAI(c, response)
	require.Equal(t, "tool_calls", got.Choices[0].FinishReason)
	require.Equal(t, "stop", got.Choices[1].FinishReason)
}
