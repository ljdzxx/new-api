package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiUsageSnapshotsPreserveMissingDetailsWithoutAccumulating(t *testing.T) {
	first := &GeminiUsageMetadata{
		PromptTokenCount: 100, ToolUsePromptTokenCount: 5, CachedContentTokenCount: 30,
		PromptTokensDetails:        []GeminiPromptTokensDetails{{Modality: "IMAGE", TokenCount: 40}, {Modality: "TEXT", TokenCount: 60}},
		ToolUsePromptTokensDetails: []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 5}},
	}
	incoming := &GeminiUsageMetadata{
		CandidatesTokenCount: 10, ThoughtsTokenCount: 2, TotalTokenCount: 117,
		PromptTokensDetails:     []GeminiPromptTokensDetails{{Modality: " image ", TokenCount: 25}, {Modality: "IMAGE", TokenCount: 15}},
		CandidatesTokensDetails: []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 10}},
	}
	merged := MergeGeminiUsageMetadataNonZero(first, incoming)
	repeated := MergeGeminiUsageMetadataNonZero(merged, incoming)
	require.Equal(t, merged, repeated)
	require.Equal(t, 100, merged.PromptTokenCount)
	require.Equal(t, 30, merged.CachedContentTokenCount)
	require.Equal(t, 117, merged.TotalTokenCount)
	require.Len(t, merged.PromptTokensDetails, 3)
	require.Equal(t, first.ToolUsePromptTokensDetails, merged.ToolUsePromptTokensDetails)
	repeated.PromptTokensDetails[0].TokenCount = 999
	require.Equal(t, 60, merged.PromptTokensDetails[0].TokenCount)
	require.Equal(t, 40, first.PromptTokensDetails[0].TokenCount)
	require.Equal(t, 25, incoming.PromptTokensDetails[0].TokenCount)
	require.Nil(t, MergeGeminiUsageMetadataNonZero(nil, nil))
	require.Equal(t, first, MergeGeminiUsageMetadataNonZero(first, nil))
}

func TestGeminiFinalCompletionSnapshotClearsEarlierThoughts(t *testing.T) {
	merged := MergeGeminiUsageMetadataNonZero(
		&GeminiUsageMetadata{PromptTokenCount: 10, ThoughtsTokenCount: 5},
		&GeminiUsageMetadata{CandidatesTokenCount: 20, TotalTokenCount: 30},
	)
	require.Zero(t, merged.ThoughtsTokenCount)
	require.Equal(t, 30, merged.TotalTokenCount)
}

func TestGeminiEstimatedCompletionPreservesNativeBillingDetails(t *testing.T) {
	original := NewGeminiChatBillingUsage(&GeminiUsageMetadata{
		PromptTokenCount: 100, ToolUsePromptTokenCount: 5, CachedContentTokenCount: 30,
		ThoughtsTokenCount:         2,
		PromptTokensDetails:        []GeminiPromptTokensDetails{{Modality: "IMAGE", TokenCount: 40}},
		ToolUsePromptTokensDetails: []GeminiPromptTokensDetails{{Modality: "TEXT", TokenCount: 5}},
	})
	estimated := CloneBillingUsageWithEstimatedCompletion(original, 12)
	require.True(t, estimated.Estimated)
	require.Equal(t, 10, estimated.GeminiUsageMetadata.CandidatesTokenCount)
	require.Equal(t, 117, estimated.GeminiUsageMetadata.TotalTokenCount)
	require.Equal(t, 30, estimated.GeminiUsageMetadata.CachedContentTokenCount)
	require.Equal(t, original.GeminiUsageMetadata.ToolUsePromptTokensDetails, estimated.GeminiUsageMetadata.ToolUsePromptTokensDetails)
	require.Zero(t, original.GeminiUsageMetadata.CandidatesTokenCount)
	require.False(t, original.Estimated)
	estimated.GeminiUsageMetadata.PromptTokensDetails[0].TokenCount = 999
	require.Equal(t, 40, original.GeminiUsageMetadata.PromptTokensDetails[0].TokenCount)
}
