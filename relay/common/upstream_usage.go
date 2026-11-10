package common

import (
	"github.com/QuantumNous/new-api/dto"
	"github.com/tidwall/gjson"
)

// RecordUpstreamUsage must only be called with counters decoded from an upstream
// response, before any local estimates or response scaling are applied. Streaming
// adaptors pass their accumulated upstream counters, not an individual delta.
func (info *RelayInfo) RecordUpstreamUsage(usage *dto.Usage) {
	if info == nil || usage == nil || (usage.BillingUsage != nil && usage.BillingUsage.Estimated) {
		return
	}
	info.UpstreamUsageReported = true
	info.upstreamUsage = dto.CloneUsage(usage)
}

func (info *RelayInfo) GetUpstreamUsage() *dto.Usage {
	if info == nil {
		return nil
	}
	return dto.CloneUsage(info.upstreamUsage)
}

// Value-typed provider DTOs cannot distinguish missing usage from explicit zero.
// Check the decoded response's original JSON before recording their counters.
func (info *RelayInfo) RecordUpstreamUsageFromJSON(usage *dto.Usage, data []byte, path string) {
	if gjson.GetBytes(data, path).IsObject() {
		info.RecordUpstreamUsage(usage)
	}
}

func (info *RelayInfo) ResetUpstreamUsage() {
	info.UpstreamUsageReported = false
	info.upstreamUsage = nil
}
