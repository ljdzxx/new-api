package dto

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// MergeGeminiUsageMetadataNonZero overlays cumulative snapshots, preserving
// earlier fields when a later frame omits them. Returned slices are detached.
func MergeGeminiUsageMetadataNonZero(current, incoming *GeminiUsageMetadata) *GeminiUsageMetadata {
	if current == nil && incoming == nil {
		return nil
	}
	if current == nil {
		metadata := cloneGeminiUsageMetadata(*incoming)
		return &metadata
	}
	merged := cloneGeminiUsageMetadata(*current)
	if incoming == nil {
		return &merged
	}
	if incoming.PromptTokenCount > 0 {
		merged.PromptTokenCount = incoming.PromptTokenCount
	}
	if incoming.ToolUsePromptTokenCount > 0 {
		merged.ToolUsePromptTokenCount = incoming.ToolUsePromptTokenCount
	}
	if incoming.CandidatesTokenCount > 0 {
		merged.CandidatesTokenCount = incoming.CandidatesTokenCount
		merged.ThoughtsTokenCount = incoming.ThoughtsTokenCount
	} else if incoming.ThoughtsTokenCount > 0 {
		merged.ThoughtsTokenCount = incoming.ThoughtsTokenCount
	}
	if incoming.TotalTokenCount > 0 {
		merged.TotalTokenCount = incoming.TotalTokenCount
	}
	if incoming.CachedContentTokenCount > 0 {
		merged.CachedContentTokenCount = incoming.CachedContentTokenCount
	}
	merged.PromptTokensDetails = mergeGeminiTokenDetails(merged.PromptTokensDetails, incoming.PromptTokensDetails)
	merged.ToolUsePromptTokensDetails = mergeGeminiTokenDetails(merged.ToolUsePromptTokensDetails, incoming.ToolUsePromptTokensDetails)
	merged.CandidatesTokensDetails = mergeGeminiTokenDetails(merged.CandidatesTokensDetails, incoming.CandidatesTokensDetails)
	total := common.QuotaFromFloat(float64(merged.PromptTokenCount) + float64(merged.ToolUsePromptTokenCount) + float64(merged.CandidatesTokenCount) + float64(merged.ThoughtsTokenCount))
	merged.TotalTokenCount = max(merged.TotalTokenCount, total)
	return &merged
}

// Each modality is cumulative across frames; keep duplicate entries within
// one snapshot for settlement to sum, but never sum the snapshots themselves.
func mergeGeminiTokenDetails(current, incoming []GeminiPromptTokensDetails) []GeminiPromptTokensDetails {
	reported := make(map[string]bool, len(incoming))
	for _, detail := range incoming {
		if detail.TokenCount > 0 {
			reported[normalizeGeminiModality(detail.Modality)] = true
		}
	}
	merged := make([]GeminiPromptTokensDetails, 0, len(current)+len(incoming))
	for _, detail := range current {
		if !reported[normalizeGeminiModality(detail.Modality)] {
			merged = append(merged, detail)
		}
	}
	for _, detail := range incoming {
		if detail.TokenCount > 0 {
			merged = append(merged, detail)
		}
	}
	return merged
}

func normalizeGeminiModality(modality string) string {
	return strings.ToUpper(strings.TrimSpace(modality))
}
