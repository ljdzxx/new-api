package billing_policy

import "sync"

type ShadowPhaseStats struct {
	Observations int64            `json:"observations"`
	Matches      int64            `json:"matches"`
	Mismatches   int64            `json:"mismatches"`
	Errors       int64            `json:"errors"`
	ByModel      map[string]int64 `json:"by_model"`
}

type ShadowStats struct {
	Observations int64            `json:"observations"`
	Matches      int64            `json:"matches"`
	Mismatches   int64            `json:"mismatches"`
	Errors       int64            `json:"errors"`
	ByModel      map[string]int64 `json:"by_model"`
	PreConsume   ShadowPhaseStats `json:"pre_consume"`
	Settlement   ShadowPhaseStats `json:"settlement"`
}

var shadowStore = struct {
	sync.RWMutex
	stats ShadowStats
}{stats: newShadowStats()}

func newShadowPhaseStats() ShadowPhaseStats {
	return ShadowPhaseStats{ByModel: map[string]int64{}}
}

func newShadowStats() ShadowStats {
	return ShadowStats{
		ByModel:    map[string]int64{},
		PreConsume: newShadowPhaseStats(),
		Settlement: newShadowPhaseStats(),
	}
}

func ResetShadowStats() {
	shadowStore.Lock()
	shadowStore.stats = newShadowStats()
	shadowStore.Unlock()
}

func ObserveShadowPreConsume(model string, legacyQuota, policyQuota int) {
	recordShadowObservation(model, legacyQuota == policyQuota, false, false)
}

func ObserveShadowPreConsumeError(model string) {
	recordShadowObservation(model, false, true, false)
}

func ObserveShadowSettlement(model string, legacyQuota, policyQuota int) {
	recordShadowObservation(model, legacyQuota == policyQuota, false, true)
}

func ObserveShadowSettlementError(model string) {
	recordShadowObservation(model, false, true, true)
}

func recordShadowObservation(model string, matched, failed, settlement bool) {
	shadowStore.Lock()
	defer shadowStore.Unlock()
	phase := &shadowStore.stats.PreConsume
	if settlement {
		phase = &shadowStore.stats.Settlement
	}
	shadowStore.stats.Observations++
	shadowStore.stats.ByModel[model]++
	phase.Observations++
	phase.ByModel[model]++
	if failed {
		shadowStore.stats.Errors++
		phase.Errors++
		return
	}
	if matched {
		shadowStore.stats.Matches++
		phase.Matches++
		return
	}
	shadowStore.stats.Mismatches++
	phase.Mismatches++
}

func GetShadowStats() ShadowStats {
	shadowStore.RLock()
	defer shadowStore.RUnlock()
	result := shadowStore.stats
	result.ByModel = cloneShadowByModel(shadowStore.stats.ByModel)
	result.PreConsume.ByModel = cloneShadowByModel(shadowStore.stats.PreConsume.ByModel)
	result.Settlement.ByModel = cloneShadowByModel(shadowStore.stats.Settlement.ByModel)
	return result
}

func cloneShadowByModel(source map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(source))
	for model, count := range source {
		result[model] = count
	}
	return result
}

func ShadowReadyForSwitch(stats ShadowStats) bool {
	return stats.Settlement.Observations > 0 &&
		stats.Settlement.Mismatches == 0 &&
		stats.Errors == 0
}
