package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

const (
	InviteRankRangeToday  = "today"
	InviteRankRangeDay    = "day"
	InviteRankRangeWeek   = "week"
	InviteRankRangeMonth  = "month"
	InviteRankRangeCustom = "custom"

	InviteRankLimit           = 100
	InviteRankMaxRangeSeconds = int64(366 * 24 * time.Hour / time.Second)
)

var (
	ErrInviteRankInvalidRange  = errors.New("invalid invite rank time range")
	ErrInviteRankRangeTooLarge = errors.New("invite rank time range cannot exceed 366 days")
)

type InviteRankWindow struct {
	RangeKey string `json:"range_key"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Timezone string `json:"timezone"`
}

type InviteRankItem struct {
	Rank          int    `json:"rank"`
	UserId        int    `json:"user_id"`
	Username      string `json:"username"`
	DisplayName   string `json:"display_name"`
	Group         string `json:"group"`
	Status        int    `json:"status"`
	Deleted       bool   `json:"deleted"`
	InviteCount   int64  `json:"invite_count"`
	TotalAffCount int    `json:"total_aff_count"`
	LastInviteAt  int64  `json:"last_invite_at"`
}

type InviteRankSummary struct {
	InviterCount      int64 `json:"inviter_count"`
	TotalInviteCount  int64 `json:"total_invite_count"`
	Top100InviteCount int64 `json:"top_100_invite_count"`
	TopInviteCount    int64 `json:"top_invite_count"`
}

type inviteRankRow struct {
	InviterId    int   `gorm:"column:inviter_id"`
	InviteCount  int64 `gorm:"column:invite_count"`
	LastInviteAt int64 `gorm:"column:last_invite_at"`
}

type inviteRankSummaryRow struct {
	InviterCount     int64 `gorm:"column:inviter_count"`
	TotalInviteCount int64 `gorm:"column:total_invite_count"`
}

type inviteRankUserRow struct {
	Id          int            `gorm:"column:id"`
	Username    string         `gorm:"column:username"`
	DisplayName string         `gorm:"column:display_name"`
	Group       string         `gorm:"column:group_name"`
	Status      int            `gorm:"column:status"`
	AffCount    int            `gorm:"column:aff_count"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at"`
}

func ResolveInviteRankWindow(rangeKey string, customStart int64, customEnd int64, now time.Time, loc *time.Location) (InviteRankWindow, error) {
	if loc == nil {
		loc = time.Local
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(loc)

	window := InviteRankWindow{RangeKey: rangeKey, Timezone: loc.String()}
	switch rangeKey {
	case "", InviteRankRangeToday:
		window.RangeKey = InviteRankRangeToday
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		window.Start = start.Unix()
		window.End = start.AddDate(0, 0, 1).Unix()
	case InviteRankRangeDay:
		window.Start = now.Add(-24 * time.Hour).Unix()
		window.End = now.Unix()
	case InviteRankRangeWeek:
		window.Start = now.Add(-7 * 24 * time.Hour).Unix()
		window.End = now.Unix()
	case InviteRankRangeMonth:
		window.Start = now.Add(-30 * 24 * time.Hour).Unix()
		window.End = now.Unix()
	case InviteRankRangeCustom:
		if customStart < 0 || customEnd <= customStart {
			return InviteRankWindow{}, ErrInviteRankInvalidRange
		}
		if customEnd-customStart > InviteRankMaxRangeSeconds {
			return InviteRankWindow{}, ErrInviteRankRangeTooLarge
		}
		window.Start = customStart
		window.End = customEnd
	default:
		return InviteRankWindow{}, ErrInviteRankInvalidRange
	}
	return window, nil
}

func GetInviteRank(window InviteRankWindow) ([]*InviteRankItem, *InviteRankSummary, error) {
	if window.Start < 0 || window.End <= window.Start {
		return nil, nil, ErrInviteRankInvalidRange
	}

	baseQuery := DB.Model(&InviteRewardAudit{}).
		Where("reward_status = ? AND created_at >= ? AND created_at < ? AND inviter_id > 0 AND invitee_id > 0",
			InviteRewardAuditStatusGranted, window.Start, window.End)

	summaryRow := inviteRankSummaryRow{}
	if err := baseQuery.Session(&gorm.Session{}).
		Select("COUNT(DISTINCT inviter_id) AS inviter_count, COUNT(DISTINCT invitee_id) AS total_invite_count").
		Scan(&summaryRow).Error; err != nil {
		return nil, nil, err
	}

	rows := make([]inviteRankRow, 0, InviteRankLimit)
	if err := baseQuery.Session(&gorm.Session{}).
		Select("inviter_id, COUNT(DISTINCT invitee_id) AS invite_count, MAX(created_at) AS last_invite_at").
		Group("inviter_id").
		Order("invite_count DESC").
		Order("last_invite_at DESC").
		Order("inviter_id ASC").
		Limit(InviteRankLimit).
		Scan(&rows).Error; err != nil {
		return nil, nil, err
	}

	userIDs := make([]int, 0, len(rows))
	for _, row := range rows {
		userIDs = append(userIDs, row.InviterId)
	}
	users, err := getInviteRankUsers(userIDs)
	if err != nil {
		return nil, nil, err
	}

	items := make([]*InviteRankItem, 0, len(rows))
	summary := &InviteRankSummary{
		InviterCount:     summaryRow.InviterCount,
		TotalInviteCount: summaryRow.TotalInviteCount,
	}
	for _, row := range rows {
		item := &InviteRankItem{
			Rank:         len(items) + 1,
			UserId:       row.InviterId,
			Deleted:      true,
			InviteCount:  row.InviteCount,
			LastInviteAt: row.LastInviteAt,
		}
		if user, ok := users[row.InviterId]; ok {
			item.Username = user.Username
			item.DisplayName = user.DisplayName
			item.Group = user.Group
			item.Status = user.Status
			item.TotalAffCount = user.AffCount
			item.Deleted = user.DeletedAt.Valid
		}
		items = append(items, item)
		summary.Top100InviteCount += item.InviteCount
	}
	if len(items) > 0 {
		summary.TopInviteCount = items[0].InviteCount
	}
	return items, summary, nil
}

func getInviteRankUsers(userIDs []int) (map[int]inviteRankUserRow, error) {
	result := make(map[int]inviteRankUserRow, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}

	rows := make([]inviteRankUserRow, 0, len(userIDs))
	if err := DB.Unscoped().Model(&User{}).
		Select("id, username, display_name, "+commonGroupCol+" AS group_name, status, aff_count, deleted_at").
		Where("id IN ?", userIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.Id] = row
	}
	return result, nil
}
