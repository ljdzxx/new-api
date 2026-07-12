package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestEffectiveBillingUsagePrefersClaudeNativeUsage(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens: 999, CompletionTokens: 999,
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{
			InputTokens: 70, CacheReadInputTokens: 30, CacheCreationInputTokens: 20, OutputTokens: 7,
		}),
	}
	effective := effectiveBillingUsage(usage)
	require.Equal(t, 70, effective.PromptTokens)
	require.Equal(t, 7, effective.CompletionTokens)
	require.Equal(t, 30, effective.PromptTokensDetails.CachedTokens)
	require.Equal(t, 20, effective.PromptTokensDetails.CacheWriteTokens)
	require.Equal(t, dto.BillingUsageSemanticAnthropic, effective.UsageSemantic)
}

func TestEffectiveBillingUsagePrefersGeminiNativeUsage(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens: 999, CompletionTokens: 999,
		BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{
			PromptTokenCount: 100, ToolUsePromptTokenCount: 5, CandidatesTokenCount: 20,
			ThoughtsTokenCount: 3, CachedContentTokenCount: 7, TotalTokenCount: 128,
		}),
	}
	effective := effectiveBillingUsage(usage)
	require.Equal(t, 105, effective.PromptTokens)
	require.Equal(t, 23, effective.CompletionTokens)
	require.Equal(t, 7, effective.PromptTokensDetails.CachedTokens)
	require.Equal(t, dto.BillingUsageSemanticGemini, effective.UsageSemantic)
}
