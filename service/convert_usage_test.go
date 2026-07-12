package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
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
