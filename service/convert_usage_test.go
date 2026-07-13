package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestBuildClaudeUsageFromOpenAICacheWriteUsage(t *testing.T) {
	usage := buildClaudeUsageFromOpenAIUsage(&dto.Usage{
		PromptTokens:     3619,
		CompletionTokens: 36,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:     2921,
			CacheWriteTokens: 3616,
		},
	})

	require.NotNil(t, usage)
	require.Zero(t, usage.InputTokens)
	require.Equal(t, 2921, usage.CacheReadInputTokens)
	require.Equal(t, 3616, usage.CacheCreationInputTokens)
	require.Equal(t, 36, usage.OutputTokens)
	require.NotNil(t, usage.BillingUsage)
	require.NotNil(t, usage.BillingUsage.OpenAIUsage)
}

func TestBuildClaudeUsageClampsNegativeOpenAICacheWriteUsage(t *testing.T) {
	usage := buildClaudeUsageFromOpenAIUsage(&dto.Usage{
		PromptTokens: 100,
		PromptTokensDetails: dto.InputTokenDetails{
			CacheWriteTokens: -25,
		},
	})

	require.NotNil(t, usage)
	require.Equal(t, 100, usage.InputTokens)
	require.Zero(t, usage.CacheCreationInputTokens)
}

func TestBuildClaudeUsageRestoresNativeAnthropicUsage(t *testing.T) {
	native := &dto.ClaudeUsage{
		InputTokens:              17,
		OutputTokens:             9,
		CacheReadInputTokens:     11,
		CacheCreationInputTokens: 13,
		CacheCreation: &dto.ClaudeCacheCreationUsage{
			Ephemeral5mInputTokens: 5,
			Ephemeral1hInputTokens: 8,
		},
		ServerToolUse: &dto.ClaudeServerToolUse{WebSearchRequests: 2},
	}
	usage := buildClaudeUsageFromOpenAIUsage(&dto.Usage{
		PromptTokens:     999,
		CompletionTokens: 888,
		BillingUsage:     dto.NewClaudeMessagesBillingUsage(native),
	})

	require.Equal(t, native, usage)
	require.NotSame(t, native, usage)
	require.NotSame(t, native.CacheCreation, usage.CacheCreation)
	require.NotSame(t, native.ServerToolUse, usage.ServerToolUse)
}

func TestResponseOpenAI2GeminiRestoresNativeUsageMetadata(t *testing.T) {
	native := &dto.GeminiUsageMetadata{
		PromptTokenCount:        17,
		ToolUsePromptTokenCount: 3,
		CandidatesTokenCount:    9,
		TotalTokenCount:         31,
		ThoughtsTokenCount:      2,
		CachedContentTokenCount: 11,
		PromptTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "TEXT", TokenCount: 17},
		},
		CandidatesTokensDetails: []dto.GeminiPromptTokensDetails{
			{Modality: "IMAGE", TokenCount: 9},
		},
	}
	openAIResponse := &dto.OpenAITextResponse{}
	openAIResponse.PromptTokens = 999
	openAIResponse.CompletionTokens = 888
	openAIResponse.BillingUsage = dto.NewGeminiChatBillingUsage(native)

	response := ResponseOpenAI2Gemini(openAIResponse, nil)
	require.Equal(t, *native, response.UsageMetadata)
	require.NotSame(t, &native.PromptTokensDetails[0], &response.UsageMetadata.PromptTokensDetails[0])
	require.NotSame(t, &native.CandidatesTokensDetails[0], &response.UsageMetadata.CandidatesTokensDetails[0])
}

func TestStreamResponseOpenAI2GeminiRestoresNativeUsageMetadata(t *testing.T) {
	native := &dto.GeminiUsageMetadata{
		PromptTokenCount:        17,
		CandidatesTokenCount:    9,
		TotalTokenCount:         26,
		CachedContentTokenCount: 11,
	}
	response := StreamResponseOpenAI2Gemini(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: common.GetPointer("stop")}},
		Usage: &dto.Usage{
			PromptTokens:     999,
			CompletionTokens: 888,
			BillingUsage:     dto.NewGeminiChatBillingUsage(native),
		},
	}, &relaycommon.RelayInfo{})

	require.NotNil(t, response)
	require.Equal(t, *native, response.UsageMetadata)
}
