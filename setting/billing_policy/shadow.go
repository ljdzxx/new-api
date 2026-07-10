package billing_policy

import "sync"

type ShadowStats struct {
	Observations int64            `json:"observations"`
	Matches      int64            `json:"matches"`
	Mismatches   int64            `json:"mismatches"`
	ByModel      map[string]int64 `json:"by_model"`
}

var shadowStore = struct {
	sync.RWMutex
	stats ShadowStats
}{stats: ShadowStats{ByModel: map[string]int64{}}}

func ResetShadowStats() {
	shadowStore.Lock()
	shadowStore.stats = ShadowStats{ByModel: map[string]int64{}}
	shadowStore.Unlock()
}

func ObserveShadow(model string, legacyQuota, policyQuota int) {
	shadowStore.Lock()
	defer shadowStore.Unlock()
	shadowStore.stats.Observations++
	shadowStore.stats.ByModel[model]++
	if legacyQuota == policyQuota {
		shadowStore.stats.Matches++
	} else {
		shadowStore.stats.Mismatches++
	}
}

func GetShadowStats() ShadowStats {
	shadowStore.RLock()
	defer shadowStore.RUnlock()
	result := shadowStore.stats
	result.ByModel = make(map[string]int64, len(shadowStore.stats.ByModel))
	for model, count := range shadowStore.stats.ByModel {
		result.ByModel[model] = count
	}
	return result
}
