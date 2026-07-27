package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

const (
	ConsumptionRankRangeToday  = "today"
	ConsumptionRankRangeDay    = "day"
	ConsumptionRankRangeWeek   = "week"
	ConsumptionRankRangeCustom = "custom"

	ConsumptionRankLimit           = 100
	ConsumptionRankMaxRangeSeconds = int64(30 * 24 * time.Hour / time.Second)
)

var (
	ErrConsumptionRankInvalidRange  = errors.New("invalid consumption rank time range")
	ErrConsumptionRankRangeTooLarge = errors.New("consumption rank time range cannot exceed 30 days")
)

type ConsumptionRankWindow struct {
	RangeKey string `json:"range_key"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
}

type ConsumptionRankItem struct {
	Rank             int    `json:"rank"`
	UserId           int    `json:"user_id"`
	Username         string `json:"username"`
	DisplayName      string `json:"display_name"`
	Group            string `json:"group"`
	Status           int    `json:"status"`
	Deleted          bool   `json:"deleted"`
	TotalTokens      int64  `json:"total_tokens"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	ConsumedQuota    int64  `json:"consumed_quota"`
	RequestCount     int64  `json:"request_count"`
	LastRequestAt    int64  `json:"last_request_at"`
}

type ConsumptionRankSummary struct {
	RankedUserCount    int64 `json:"ranked_user_count"`
	TotalTokens        int64 `json:"total_tokens"`
	PromptTokens       int64 `json:"prompt_tokens"`
	CompletionTokens   int64 `json:"completion_tokens"`
	TotalConsumedQuota int64 `json:"total_consumed_quota"`
}

type consumptionRankLogRow struct {
	UserId           int    `gorm:"column:user_id"`
	Username         string `gorm:"column:username"`
	PromptTokens     int64  `gorm:"column:prompt_tokens"`
	CompletionTokens int64  `gorm:"column:completion_tokens"`
	ConsumedQuota    int64  `gorm:"column:consumed_quota"`
	RequestCount     int64  `gorm:"column:request_count"`
	LastRequestAt    int64  `gorm:"column:last_request_at"`
}

type consumptionRankUserRow struct {
	Id          int            `gorm:"column:id"`
	Username    string         `gorm:"column:username"`
	DisplayName string         `gorm:"column:display_name"`
	Group       string         `gorm:"column:group_name"`
	Status      int            `gorm:"column:status"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at"`
}

func ResolveConsumptionRankWindow(rangeKey string, customStart int64, customEnd int64, now time.Time, loc *time.Location) (ConsumptionRankWindow, error) {
	if loc == nil {
		loc = time.Local
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(loc)

	window := ConsumptionRankWindow{RangeKey: rangeKey}
	switch rangeKey {
	case "", ConsumptionRankRangeToday:
		window.RangeKey = ConsumptionRankRangeToday
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		window.Start = start.Unix()
		window.End = start.AddDate(0, 0, 1).Unix()
	case ConsumptionRankRangeDay:
		window.Start = now.Add(-24 * time.Hour).Unix()
		window.End = now.Unix()
	case ConsumptionRankRangeWeek:
		window.Start = now.Add(-7 * 24 * time.Hour).Unix()
		window.End = now.Unix()
	case ConsumptionRankRangeCustom:
		if customStart < 0 || customEnd <= customStart {
			return ConsumptionRankWindow{}, ErrConsumptionRankInvalidRange
		}
		if customEnd-customStart > ConsumptionRankMaxRangeSeconds {
			return ConsumptionRankWindow{}, ErrConsumptionRankRangeTooLarge
		}
		window.Start = customStart
		window.End = customEnd
	default:
		return ConsumptionRankWindow{}, ErrConsumptionRankInvalidRange
	}
	return window, nil
}

func GetConsumptionRank(window ConsumptionRankWindow) ([]*ConsumptionRankItem, *ConsumptionRankSummary, error) {
	if window.Start < 0 || window.End <= window.Start {
		return nil, nil, ErrConsumptionRankInvalidRange
	}

	rows := make([]consumptionRankLogRow, 0, ConsumptionRankLimit)
	positivePrompt := "CASE WHEN prompt_tokens > 0 THEN prompt_tokens ELSE 0 END"
	positiveCompletion := "CASE WHEN completion_tokens > 0 THEN completion_tokens ELSE 0 END"
	positiveQuota := "CASE WHEN quota > 0 THEN quota ELSE 0 END"
	tokenRequest := "CASE WHEN prompt_tokens > 0 OR completion_tokens > 0 THEN 1 ELSE 0 END"
	totalTokens := "SUM(" + positivePrompt + ") + SUM(" + positiveCompletion + ")"

	err := excludeChannelForwardPrecheckLogs(LOG_DB.Table("logs")).
		Select("user_id AS user_id, MAX(username) AS username, "+
			"COALESCE(SUM("+positivePrompt+"), 0) AS prompt_tokens, "+
			"COALESCE(SUM("+positiveCompletion+"), 0) AS completion_tokens, "+
			"COALESCE(SUM("+positiveQuota+"), 0) AS consumed_quota, "+
			"COALESCE(SUM("+tokenRequest+"), 0) AS request_count, "+
			"MAX(created_at) AS last_request_at").
		Where("type = ? AND created_at >= ? AND created_at < ? AND user_id > 0", LogTypeConsume, window.Start, window.End).
		Group("user_id").
		Having(totalTokens + " > 0").
		Order(totalTokens + " DESC").
		Order("SUM(" + positiveCompletion + ") DESC").
		Order("user_id ASC").
		Limit(ConsumptionRankLimit).
		Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}

	userIDs := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.UserId > 0 {
			userIDs = append(userIDs, row.UserId)
		}
	}
	users, err := getConsumptionRankUsers(userIDs)
	if err != nil {
		return nil, nil, err
	}

	items := make([]*ConsumptionRankItem, 0, len(rows))
	summary := &ConsumptionRankSummary{}
	for _, row := range rows {
		if row.UserId <= 0 {
			continue
		}
		item := &ConsumptionRankItem{
			Rank:             len(items) + 1,
			UserId:           row.UserId,
			Username:         row.Username,
			Deleted:          true,
			TotalTokens:      row.PromptTokens + row.CompletionTokens,
			PromptTokens:     row.PromptTokens,
			CompletionTokens: row.CompletionTokens,
			ConsumedQuota:    row.ConsumedQuota,
			RequestCount:     row.RequestCount,
			LastRequestAt:    row.LastRequestAt,
		}
		if user, ok := users[row.UserId]; ok {
			item.Username = user.Username
			item.DisplayName = user.DisplayName
			item.Group = user.Group
			item.Status = user.Status
			item.Deleted = user.DeletedAt.Valid
		}
		items = append(items, item)
		summary.TotalTokens += item.TotalTokens
		summary.PromptTokens += item.PromptTokens
		summary.CompletionTokens += item.CompletionTokens
		summary.TotalConsumedQuota += item.ConsumedQuota
	}
	summary.RankedUserCount = int64(len(items))
	return items, summary, nil
}

func getConsumptionRankUsers(userIDs []int) (map[int]consumptionRankUserRow, error) {
	result := make(map[int]consumptionRankUserRow, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}

	rows := make([]consumptionRankUserRow, 0, len(userIDs))
	err := DB.Unscoped().Model(&User{}).
		Select("id, username, display_name, "+commonGroupCol+" AS group_name, status, deleted_at").
		Where("id IN ?", userIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.Id] = row
	}
	return result, nil
}
