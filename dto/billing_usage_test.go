package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingUsageConstructorsRejectAllZeroUsage(t *testing.T) {
	require.Nil(t, NewClaudeMessagesBillingUsage(nil))
	require.Nil(t, NewClaudeMessagesBillingUsage(&ClaudeUsage{}))
	require.Nil(t, NewClaudeMessagesBillingUsage(&ClaudeUsage{CacheCreation: &ClaudeCacheCreationUsage{}}))
	require.Nil(t, NewOpenAIChatBillingUsage(nil))
	require.Nil(t, NewOpenAIChatBillingUsage(&Usage{}))
	require.Nil(t, NewGeminiChatBillingUsage(nil))
	require.Nil(t, NewGeminiChatBillingUsage(&GeminiUsageMetadata{}))
}

func TestBillingUsageConstructorsPreserveNativeUsage(t *testing.T) {
	claudeUsage := NewClaudeMessagesBillingUsage(&ClaudeUsage{InputTokens: 1})
	require.NotNil(t, claudeUsage)
	assert.Equal(t, BillingUsageSemanticAnthropic, claudeUsage.Semantic)

	openAIUsage := NewOpenAIChatBillingUsage(&Usage{PromptTokens: 2})
	require.NotNil(t, openAIUsage)
	assert.Equal(t, BillingUsageSemanticOpenAI, openAIUsage.Semantic)

	geminiUsage := NewGeminiChatBillingUsage(&GeminiUsageMetadata{PromptTokenCount: 3})
	require.NotNil(t, geminiUsage)
	assert.Equal(t, BillingUsageSemanticGemini, geminiUsage.Semantic)
}

func TestCloneBillingUsageRemovesNestedSidecars(t *testing.T) {
	original := &BillingUsage{
		OpenAIUsage: &Usage{PromptTokens: 1, BillingUsage: NewClaudeMessagesBillingUsage(&ClaudeUsage{InputTokens: 9})},
		ClaudeUsage: &ClaudeUsage{InputTokens: 2, BillingUsage: NewOpenAIChatBillingUsage(&Usage{PromptTokens: 8})},
		GeminiUsageMetadata: &GeminiUsageMetadata{PromptTokenCount: 3,
			BillingUsage: NewOpenAIChatBillingUsage(&Usage{PromptTokens: 7})},
	}
	clone := CloneBillingUsage(original)
	require.NotNil(t, clone)
	assert.Nil(t, clone.OpenAIUsage.BillingUsage)
	assert.Nil(t, clone.ClaudeUsage.BillingUsage)
	assert.Nil(t, clone.GeminiUsageMetadata.BillingUsage)
}
