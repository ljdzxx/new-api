package model

import (
	"cmp"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	SubscriptionUsageRankRange1Day = "1d"
	SubscriptionUsageRankRange3Day = "3d"
	SubscriptionUsageRankRange7Day = "7d"
)

type UserSubscriptionUsageSnapshot struct {
	UserId      int   `json:"user_id"`
	TodayTotal  int64 `json:"today_total"`
	TodayUsed   int64 `json:"today_used"`
	Unlimited   bool  `json:"unlimited"`
	ActiveCount int64 `json:"active_count"`
}

type SubscriptionUsageRankUser struct {
	UserId      int    `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Group       string `json:"group"`
}

type SubscriptionUsageRankItem struct {
	Rank                       int     `json:"rank"`
	UserId                     int     `json:"user_id"`
	Username                   string  `json:"username"`
	DisplayName                string  `json:"display_name"`
	Group                      string  `json:"group"`
	UsageAmount                int64   `json:"usage_amount"`
	RequestCount               int64   `json:"request_count"`
	ActiveMinutes              int64   `json:"active_minutes"`
	ARPM                       float64 `json:"arpm"`
	FirstRequestAt             int64   `json:"first_request_at"`
	LastRequestAt              int64   `json:"last_request_at"`
	TodaySubscriptionUsed      int64   `json:"today_subscription_used"`
	TodaySubscriptionTotal     int64   `json:"today_subscription_total"`
	TodaySubscriptionUnlimited bool    `json:"today_subscription_unlimited"`
	TodayUsageRatio            float64 `json:"today_usage_ratio"`
	ActiveSubscriptionCount    int64   `json:"active_subscription_count"`
}

type SubscriptionUsageRankSummary struct {
	RangeKey          string  `json:"range_key"`
	WindowStart       int64   `json:"window_start"`
	WindowEnd         int64   `json:"window_end"`
	RankedUserCount   int64   `json:"ranked_user_count"`
	TotalUsageAmount  int64   `json:"total_usage_amount"`
	TotalRequestCount int64   `json:"total_request_count"`
	AverageARPM       float64 `json:"average_arpm"`
}

type subscriptionUsageRankLogRow struct {
	UserId         int   `gorm:"column:user_id"`
	UsageAmount    int64 `gorm:"column:usage_amount"`
	RequestCount   int64 `gorm:"column:request_count"`
	FirstRequestAt int64 `gorm:"column:first_request_at"`
	LastRequestAt  int64 `gorm:"column:last_request_at"`
}

type subscriptionUsageRankSnapshotRow struct {
	UserId         int   `gorm:"column:user_id"`
	TodayTotal     int64 `gorm:"column:today_total"`
	TodayUsed      int64 `gorm:"column:today_used"`
	UnlimitedCount int64 `gorm:"column:unlimited_count"`
	ActiveCount    int64 `gorm:"column:active_count"`
}

type subscriptionUsageRankUserRow struct {
	Id          int    `gorm:"column:id"`
	Username    string `gorm:"column:username"`
	DisplayName string `gorm:"column:display_name"`
	Group       string `gorm:"column:group_name"`
}

func NormalizeSubscriptionUsageRankRange(rangeKey string) string {
	switch strings.TrimSpace(rangeKey) {
	case SubscriptionUsageRankRange3Day:
		return SubscriptionUsageRankRange3Day
	case SubscriptionUsageRankRange7Day:
		return SubscriptionUsageRankRange7Day
	default:
		return SubscriptionUsageRankRange1Day
	}
}

func SubscriptionUsageRankRangeSeconds(rangeKey string) int64 {
	switch NormalizeSubscriptionUsageRankRange(rangeKey) {
	case SubscriptionUsageRankRange3Day:
		return 3 * 24 * 60 * 60
	case SubscriptionUsageRankRange7Day:
		return 7 * 24 * 60 * 60
	default:
		return 24 * 60 * 60
	}
}

func GetSubscriptionUsageRankPage(pageInfo *common.PageInfo, rangeKey string, keyword string, sortBy string, sortOrder string) ([]*SubscriptionUsageRankItem, int64, *SubscriptionUsageRankSummary, error) {
	if pageInfo == nil {
		return nil, 0, nil, errors.New("page info is required")
	}

	windowEnd := GetDBTimestamp()
	rangeKey = NormalizeSubscriptionUsageRankRange(rangeKey)
	windowStart := windowEnd - SubscriptionUsageRankRangeSeconds(rangeKey)
	if windowStart < 0 {
		windowStart = 0
	}

	summary := &SubscriptionUsageRankSummary{
		RangeKey:    rangeKey,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
	}

	snapshots, err := getActiveSubscriptionUsageSnapshots(windowEnd)
	if err != nil {
		return nil, 0, nil, err
	}
	if len(snapshots) == 0 {
		return []*SubscriptionUsageRankItem{}, 0, summary, nil
	}

	userIDs := make([]int, 0, len(snapshots))
	for userId := range snapshots {
		userIDs = append(userIDs, userId)
	}

	users, err := getSubscriptionUsageRankUsers(userIDs, keyword)
	if err != nil {
		return nil, 0, nil, err
	}
	if len(users) == 0 {
		return []*SubscriptionUsageRankItem{}, 0, summary, nil
	}

	filteredUserIDs := make([]int, 0, len(users))
	for userId := range users {
		filteredUserIDs = append(filteredUserIDs, userId)
	}

	logAggMap, err := getSubscriptionUsageRankLogs(filteredUserIDs, windowStart, windowEnd)
	if err != nil {
		return nil, 0, nil, err
	}

	items := make([]*SubscriptionUsageRankItem, 0, len(logAggMap))
	for userId, logAgg := range logAggMap {
		if logAgg == nil || logAgg.RequestCount <= 0 {
			continue
		}
		userInfo, ok := users[userId]
		if !ok || userInfo == nil {
			continue
		}
		snapshot, ok := snapshots[userId]
		if !ok || snapshot == nil {
			continue
		}

		activeMinutes := calculateSubscriptionUsageActiveMinutes(logAgg.FirstRequestAt, logAgg.LastRequestAt)
		arpm := float64(logAgg.RequestCount) / float64(activeMinutes)
		todayUsageRatio := 0.0
		if !snapshot.Unlimited && snapshot.TodayTotal > 0 {
			todayUsageRatio = float64(snapshot.TodayUsed) / float64(snapshot.TodayTotal)
			if todayUsageRatio < 0 {
				todayUsageRatio = 0
			}
			if todayUsageRatio > 1 {
				todayUsageRatio = 1
			}
		}

		items = append(items, &SubscriptionUsageRankItem{
			UserId:                     userInfo.UserId,
			Username:                   userInfo.Username,
			DisplayName:                userInfo.DisplayName,
			Group:                      userInfo.Group,
			UsageAmount:                logAgg.UsageAmount,
			RequestCount:               logAgg.RequestCount,
			ActiveMinutes:              activeMinutes,
			ARPM:                       arpm,
			FirstRequestAt:             logAgg.FirstRequestAt,
			LastRequestAt:              logAgg.LastRequestAt,
			TodaySubscriptionUsed:      snapshot.TodayUsed,
			TodaySubscriptionTotal:     snapshot.TodayTotal,
			TodaySubscriptionUnlimited: snapshot.Unlimited,
			TodayUsageRatio:            todayUsageRatio,
			ActiveSubscriptionCount:    snapshot.ActiveCount,
		})
	}

	sortSubscriptionUsageRankItems(items, sortBy, sortOrder)

	var arpmSum float64
	for index, item := range items {
		item.Rank = index + 1
		summary.TotalUsageAmount += item.UsageAmount
		summary.TotalRequestCount += item.RequestCount
		arpmSum += item.ARPM
	}

	summary.RankedUserCount = int64(len(items))
	if len(items) > 0 {
		summary.AverageARPM = arpmSum / float64(len(items))
	}

	total := int64(len(items))
	startIdx := pageInfo.GetStartIdx()
	if startIdx > len(items) {
		startIdx = len(items)
	}
	endIdx := pageInfo.GetEndIdx()
	if endIdx > len(items) {
		endIdx = len(items)
	}
	if endIdx < startIdx {
		endIdx = startIdx
	}

	return items[startIdx:endIdx], total, summary, nil
}

func getActiveSubscriptionUsageSnapshots(now int64) (map[int]*UserSubscriptionUsageSnapshot, error) {
	rows := make([]subscriptionUsageRankSnapshotRow, 0)
	err := DB.Table("user_subscriptions").
		Select(`user_id AS user_id,
COALESCE(SUM(CASE WHEN amount_total > 0 THEN amount_total ELSE 0 END), 0) AS today_total,
COALESCE(SUM(CASE WHEN amount_total > 0 THEN CASE WHEN amount_used < 0 THEN 0 WHEN amount_used > amount_total THEN amount_total ELSE amount_used END ELSE 0 END), 0) AS today_used,
COALESCE(SUM(CASE WHEN amount_total <= 0 THEN 1 ELSE 0 END), 0) AS unlimited_count,
COUNT(*) AS active_count`).
		Where("status = ? AND end_time > ?", "active", now).
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[int]*UserSubscriptionUsageSnapshot, len(rows))
	for _, row := range rows {
		if row.UserId <= 0 {
			continue
		}
		total := row.TodayTotal
		used := row.TodayUsed
		if total < 0 {
			total = 0
		}
		if used < 0 {
			used = 0
		}
		if total > 0 && used > total {
			used = total
		}
		result[row.UserId] = &UserSubscriptionUsageSnapshot{
			UserId:      row.UserId,
			TodayTotal:  total,
			TodayUsed:   used,
			Unlimited:   row.UnlimitedCount > 0,
			ActiveCount: row.ActiveCount,
		}
	}
	return result, nil
}

func getSubscriptionUsageRankUsers(userIDs []int, keyword string) (map[int]*SubscriptionUsageRankUser, error) {
	if len(userIDs) == 0 {
		return map[int]*SubscriptionUsageRankUser{}, nil
	}

	rows := make([]subscriptionUsageRankUserRow, 0)
	query := DB.Model(&User{}).
		Select("id, username, display_name, "+commonGroupCol+" AS group_name").
		Where("id IN ?", userIDs)

	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		likeKeyword := "%" + keyword + "%"
		if numericID, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("(id = ? OR username LIKE ? OR display_name LIKE ?)", numericID, likeKeyword, likeKeyword)
		} else {
			query = query.Where("(username LIKE ? OR display_name LIKE ?)", likeKeyword, likeKeyword)
		}
	}

	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}

	result := make(map[int]*SubscriptionUsageRankUser, len(rows))
	for _, row := range rows {
		if row.Id <= 0 {
			continue
		}
		result[row.Id] = &SubscriptionUsageRankUser{
			UserId:      row.Id,
			Username:    row.Username,
			DisplayName: row.DisplayName,
			Group:       row.Group,
		}
	}
	return result, nil
}

func getSubscriptionUsageRankLogs(userIDs []int, windowStart int64, windowEnd int64) (map[int]*subscriptionUsageRankLogRow, error) {
	if len(userIDs) == 0 {
		return map[int]*subscriptionUsageRankLogRow{}, nil
	}

	rows := make([]subscriptionUsageRankLogRow, 0)
	err := LOG_DB.Table("logs").
		Select(`user_id AS user_id,
COALESCE(SUM(quota), 0) AS usage_amount,
COUNT(*) AS request_count,
MIN(created_at) AS first_request_at,
MAX(created_at) AS last_request_at`).
		Where("type = ? AND created_at >= ? AND created_at <= ? AND user_id IN ?", LogTypeConsume, windowStart, windowEnd, userIDs).
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make(map[int]*subscriptionUsageRankLogRow, len(rows))
	for _, row := range rows {
		if row.UserId <= 0 || row.RequestCount <= 0 {
			continue
		}
		result[row.UserId] = &subscriptionUsageRankLogRow{
			UserId:         row.UserId,
			UsageAmount:    row.UsageAmount,
			RequestCount:   row.RequestCount,
			FirstRequestAt: row.FirstRequestAt,
			LastRequestAt:  row.LastRequestAt,
		}
	}
	return result, nil
}

func calculateSubscriptionUsageActiveMinutes(firstRequestAt int64, lastRequestAt int64) int64 {
	if firstRequestAt <= 0 || lastRequestAt <= 0 || lastRequestAt <= firstRequestAt {
		return 1
	}
	minutes := int64(math.Ceil(float64(lastRequestAt-firstRequestAt) / 60.0))
	if minutes < 1 {
		return 1
	}
	return minutes
}

func sortSubscriptionUsageRankItems(items []*SubscriptionUsageRankItem, sortBy string, sortOrder string) {
	normalizedSortBy := strings.TrimSpace(sortBy)
	if normalizedSortBy == "" {
		normalizedSortBy = "usage_amount"
	}
	desc := !strings.EqualFold(strings.TrimSpace(sortOrder), "asc")

	compareInt64 := func(left int64, right int64) int {
		if desc {
			return cmp.Compare(right, left)
		}
		return cmp.Compare(left, right)
	}
	compareFloat := func(left float64, right float64) int {
		if desc {
			return cmp.Compare(right, left)
		}
		return cmp.Compare(left, right)
	}
	compareString := func(left string, right string) int {
		if desc {
			return cmp.Compare(right, left)
		}
		return cmp.Compare(left, right)
	}

	slices.SortStableFunc(items, func(left *SubscriptionUsageRankItem, right *SubscriptionUsageRankItem) int {
		if left == nil || right == nil {
			return 0
		}

		var result int
		switch normalizedSortBy {
		case "request_count":
			result = compareInt64(left.RequestCount, right.RequestCount)
		case "arpm":
			result = compareFloat(left.ARPM, right.ARPM)
		case "today_usage_ratio":
			result = compareFloat(left.TodayUsageRatio, right.TodayUsageRatio)
		case "last_request_at":
			result = compareInt64(left.LastRequestAt, right.LastRequestAt)
		case "username":
			result = compareString(left.Username, right.Username)
		default:
			result = compareInt64(left.UsageAmount, right.UsageAmount)
		}
		if result != 0 {
			return result
		}

		if tie := cmp.Compare(right.UsageAmount, left.UsageAmount); tie != 0 {
			return tie
		}
		if tie := cmp.Compare(right.RequestCount, left.RequestCount); tie != 0 {
			return tie
		}
		return cmp.Compare(left.UserId, right.UserId)
	})
}
