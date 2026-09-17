package service

import (
	"context"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// BuildUsageCompatBalance translates native quota units to USD, independently of
// dashboard currency/display settings and DisplayTokenStatEnabled.
func BuildUsageCompatBalance(token *model.Token, user *model.User, now time.Time) map[string]any {
	if !token.UnlimitedQuota {
		status := "active"
		switch token.Status {
		case common.TokenStatusExpired:
			status = "expired"
		case common.TokenStatusExhausted:
			status = "quota_exhausted"
		}
		remaining := max(0, float64(token.RemainQuota)/common.QuotaPerUnit)
		response := map[string]any{
			"mode": "quota_limited", "isValid": true, "status": status,
			"remaining": remaining, "unit": "USD",
			"quota": map[string]any{
				"limit":     (float64(token.RemainQuota) + float64(token.UsedQuota)) / common.QuotaPerUnit,
				"used":      float64(token.UsedQuota) / common.QuotaPerUnit,
				"remaining": remaining, "unit": "USD",
			},
		}
		if token.ExpiredTime > 0 {
			expiry := time.Unix(token.ExpiredTime, 0).UTC()
			response["expires_at"] = expiry
			response["days_until_expiry"] = max(0, int(expiry.Sub(now).Hours()/24))
		}
		return response
	}
	preference := common.NormalizeBillingPreference(user.GetSetting().BillingPreference)
	if preference != "wallet_only" && !(preference == "wallet_first" && user.Quota > 0) {
		if response := usageCompatSubscription(token, user); response != nil {
			return response
		}
		if preference == "subscription_only" {
			group := token.Group
			if group == "" {
				group = user.Group
			}
			// Like a subscription group with no active subscription in sub2api.
			return map[string]any{"mode": "unrestricted", "isValid": true, "planName": group, "unit": "USD"}
		}
	}
	balance := float64(user.Quota) / common.QuotaPerUnit
	return map[string]any{
		"mode": "unrestricted", "isValid": true, "planName": "钱包余额",
		"remaining": balance, "unit": "USD", "balance": balance,
	}
}

// Native subscriptions have one reset period per plan. Select the first eligible
// subscription in settlement order and expose its period using the reference keys.
func usageCompatSubscription(token *model.Token, user *model.User) map[string]any {
	subscriptions, err := model.GetAllActiveUserSubscriptions(user.Id)
	if err != nil || len(subscriptions) == 0 {
		return nil
	}
	group := token.Group
	if group == "" {
		group = user.Group
	}
	groups := []string{group}
	if group == "auto" {
		if autoGroups := GetUserAutoGroup(user.Group); len(autoGroups) > 0 {
			groups = autoGroups
		}
	}
	var fallback map[string]any
	for i := len(subscriptions) - 1; i >= 0; i-- {
		sub := subscriptions[i].Subscription
		plan, err := model.GetSubscriptionPlanById(sub.PlanId)
		if err != nil || !model.SubscriptionPlanSupportsAnyGroup(plan.AllowedGroups, groups) {
			continue
		}
		used := float64(sub.AmountUsed) / common.QuotaPerUnit
		limit := float64(sub.AmountTotal) / common.QuotaPerUnit
		remaining := -1.0
		if sub.AmountTotal > 0 {
			remaining = max(0, limit-used)
		}
		info := map[string]any{
			"daily_usage_usd": 0.0, "weekly_usage_usd": 0.0, "monthly_usage_usd": 0.0,
			"daily_limit_usd": nil, "weekly_limit_usd": nil, "monthly_limit_usd": nil,
			"weekly_window_start": nil, "expires_at": time.Unix(sub.EndTime, 0).UTC(),
		}
		var period string
		switch model.NormalizeResetPeriod(plan.QuotaResetPeriod) {
		case model.SubscriptionResetDaily:
			period = "daily"
		case model.SubscriptionResetWeekly:
			period = "weekly"
			if sub.LastResetTime > 0 {
				info["weekly_window_start"] = time.Unix(sub.LastResetTime, 0).UTC()
			}
		case model.SubscriptionResetMonthly:
			period = "monthly"
		}
		if period != "" {
			info[period+"_usage_usd"] = used
			if sub.AmountTotal > 0 {
				info[period+"_limit_usd"] = limit
			}
		}
		response := map[string]any{
			"mode": "unrestricted", "isValid": true, "planName": plan.Title,
			"remaining": remaining, "unit": "USD", "subscription": info,
		}
		if remaining != 0 {
			return response
		}
		if fallback == nil {
			fallback = response
		}
	}
	return fallback
}

func addUsageCompatTotals(target *dto.UsageCompatTotals, value dto.UsageCompatTotals) {
	target.Requests += value.Requests
	target.InputTokens += value.InputTokens
	target.OutputTokens += value.OutputTokens
	target.CacheCreationTokens += value.CacheCreationTokens
	target.CacheReadTokens += value.CacheReadTokens
	target.TotalTokens += value.TotalTokens
	target.Cost += value.Cost
	target.ActualCost += value.ActualCost
}

func usageCompatLogTotals(entry model.Log) dto.UsageCompatTotals {
	var other struct {
		CacheRead           int64    `json:"cache_tokens"`
		CacheCreation       int64    `json:"cache_creation_tokens"`
		Claude              bool     `json:"claude"`
		GroupRatio          *float64 `json:"group_ratio"`
		EffectiveGroupRatio *float64 `json:"effective_group_ratio"`
	}
	_ = common.UnmarshalJsonStr(entry.Other, &other)
	input := int64(entry.PromptTokens)
	// OpenAI's prompt count includes cached tokens; Claude's excludes them.
	if !other.Claude {
		input = max(0, input-other.CacheRead-other.CacheCreation)
	}
	actual := float64(entry.Quota) / common.QuotaPerUnit
	cost := actual
	ratio := other.EffectiveGroupRatio
	if ratio == nil {
		ratio = other.GroupRatio
	}
	if ratio != nil && *ratio > 0 {
		cost /= *ratio
	}
	return dto.UsageCompatTotals{
		Requests: 1, InputTokens: input, OutputTokens: int64(entry.CompletionTokens),
		CacheCreationTokens: other.CacheCreation, CacheReadTokens: other.CacheRead,
		TotalTokens: input + int64(entry.CompletionTokens) + other.CacheCreation + other.CacheRead,
		Cost:        cost, ActualCost: actual,
	}
}

// BuildUsageCompatStats aggregates in Go to support SQLite, MySQL and PostgreSQL
// without vendor-specific JSON/date expressions. Only consume logs are counted.
func BuildUsageCompatStats(ctx context.Context, userID, tokenID int, now, modelStart, modelEnd time.Time, days int, location *time.Location) (*dto.UsageCompatSummary, []dto.UsageCompatDaily, []dto.UsageCompatModel, error) {
	summary := &dto.UsageCompatSummary{}
	daily := make([]dto.UsageCompatDaily, 0)
	models := make([]dto.UsageCompatModel, 0)
	dailyTotals := make(map[string]*dto.UsageCompatTotals)
	modelTotals := make(map[string]*dto.UsageCompatTotals)
	localNow := now.In(location)
	dailyEnd := time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, 0, 0, 0, 0, location)
	dailyStart := dailyEnd.AddDate(0, 0, -days)
	serverNow := now.In(time.Local)
	today := time.Date(serverNow.Year(), serverNow.Month(), serverNow.Day(), 0, 0, 0, 0, time.Local)
	var durationMS float64
	var recent dto.UsageCompatTotals
	err := model.VisitTokenUsageLogs(ctx, userID, tokenID, func(entry model.Log) {
		value := usageCompatLogTotals(entry)
		at := time.Unix(entry.CreatedAt, 0)
		addUsageCompatTotals(&summary.Total, value)
		durationMS += float64(entry.UseTime) * 1000
		if !at.Before(today) {
			addUsageCompatTotals(&summary.Today, value)
		}
		if !at.Before(now.Add(-5 * time.Minute)) {
			addUsageCompatTotals(&recent, value)
		}
		if !at.Before(dailyStart) && at.Before(dailyEnd) {
			// The reference uses the requested timezone for range boundaries,
			// but its SQL TO_CHAR groups dates in the server/database timezone.
			date := at.In(time.Local).Format("2006-01-02")
			if dailyTotals[date] == nil {
				dailyTotals[date] = &dto.UsageCompatTotals{}
			}
			addUsageCompatTotals(dailyTotals[date], value)
		}
		if !at.Before(modelStart) && at.Before(modelEnd) {
			if modelTotals[entry.ModelName] == nil {
				modelTotals[entry.ModelName] = &dto.UsageCompatTotals{}
			}
			addUsageCompatTotals(modelTotals[entry.ModelName], value)
		}
	})
	if err != nil {
		return nil, nil, nil, err
	}
	if summary.Total.Requests > 0 {
		summary.AverageDurationMS = durationMS / float64(summary.Total.Requests)
	}
	summary.RPM, summary.TPM = recent.Requests/5, recent.TotalTokens/5
	for date, value := range dailyTotals {
		daily = append(daily, dto.UsageCompatDaily{
			Date: date, Requests: value.Requests, InputTokens: value.InputTokens, OutputTokens: value.OutputTokens,
			CacheReadTokens: value.CacheReadTokens, CacheWriteTokens: value.CacheCreationTokens,
			TotalTokens: value.TotalTokens, Cost: value.Cost, ActualCost: value.ActualCost,
		})
	}
	for name, value := range modelTotals {
		// Native logs do not record the upstream account's cost.
		models = append(models, dto.UsageCompatModel{Model: name, UsageCompatTotals: *value})
	}
	sort.Slice(daily, func(i, j int) bool { return daily[i].Date < daily[j].Date })
	sort.Slice(models, func(i, j int) bool {
		if models[i].TotalTokens == models[j].TotalTokens {
			return models[i].Model < models[j].Model
		}
		return models[i].TotalTokens > models[j].TotalTokens
	})
	return summary, daily, models, nil
}
