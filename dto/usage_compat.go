package dto

// UsageCompatTotals matches sub2api's /v1/usage statistics. All costs are USD.
type UsageCompatTotals struct {
	Requests            int64   `json:"requests"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	Cost                float64 `json:"cost"`
	ActualCost          float64 `json:"actual_cost"`
}

type UsageCompatSummary struct {
	Today             UsageCompatTotals `json:"today"`
	Total             UsageCompatTotals `json:"total"`
	AverageDurationMS float64           `json:"average_duration_ms"`
	RPM               int64             `json:"rpm"`
	TPM               int64             `json:"tpm"`
}

type UsageCompatModel struct {
	Model string `json:"model"`
	UsageCompatTotals
	AccountCost float64 `json:"account_cost"`
}

type UsageCompatDaily struct {
	Date             string  `json:"date"`
	Requests         int64   `json:"requests"`
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	Cost             float64 `json:"cost"`
	ActualCost       float64 `json:"actual_cost"`
}
