package common

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestUpstreamUsageSnapshotIsIndependentAndResets(t *testing.T) {
	info := &RelayInfo{}
	usage := &dto.Usage{PromptTokens: 100, InputTokensDetails: &dto.InputTokenDetails{CachedTokens: 20}}
	usage.BillingUsage = dto.NewOpenAIChatBillingUsage(usage)
	info.RecordUpstreamUsage(usage)
	usage.PromptTokens = 999
	usage.InputTokensDetails.CachedTokens = 999
	usage.BillingUsage.OpenAIUsage.PromptTokens = 999
	got := info.GetUpstreamUsage()
	require.Equal(t, 100, got.PromptTokens)
	require.Equal(t, 20, got.InputTokensDetails.CachedTokens)
	require.Equal(t, 100, got.BillingUsage.OpenAIUsage.PromptTokens)
	got.PromptTokens = 999
	require.Equal(t, 100, info.GetUpstreamUsage().PromptTokens)
	info.ResetUpstreamUsage()
	require.False(t, info.UpstreamUsageReported)
	require.Nil(t, info.GetUpstreamUsage())
}

func TestUpstreamUsagePresenceAndEstimates(t *testing.T) {
	for _, raw := range []string{`{}`, `{"usage":null}`, `{"usage":{}}`} {
		info := &RelayInfo{}
		info.RecordUpstreamUsageFromJSON(&dto.Usage{}, []byte(raw), "usage")
		require.Equal(t, raw == `{"usage":{}}`, info.UpstreamUsageReported)
	}
	info := &RelayInfo{}
	info.RecordUpstreamUsage(&dto.Usage{PromptTokens: 100, BillingUsage: &dto.BillingUsage{Estimated: true}})
	require.Nil(t, info.GetUpstreamUsage())
}
