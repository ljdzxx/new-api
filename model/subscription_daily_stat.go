package model

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UserSubscriptionDailyStat struct {
	Id                 int    `json:"id"`
	StatDay            string `json:"stat_day" gorm:"type:varchar(10);not null;index:idx_user_subscription_daily_stats_user_day,priority:2;index:idx_user_subscription_daily_stats_day,priority:1;uniqueIndex:uk_user_subscription_daily_stats_sub_day,priority:2"`
	UserId             int    `json:"user_id" gorm:"not null;index;index:idx_user_subscription_daily_stats_user_day,priority:1"`
	UserSubscriptionId int    `json:"user_subscription_id" gorm:"not null;index;uniqueIndex:uk_user_subscription_daily_stats_sub_day,priority:1"`
	PlanId             int    `json:"plan_id" gorm:"not null;default:0;index"`
	PlanTitle          string `json:"plan_title" gorm:"type:varchar(128);default:''"`
	AmountTotal        int64  `json:"amount_total" gorm:"type:bigint;not null;default:0"`
	AmountUsed         int64  `json:"amount_used" gorm:"type:bigint;not null;default:0"`
	AmountRemain       int64  `json:"amount_remain" gorm:"type:bigint;not null;default:0"`
	Unlimited          bool   `json:"unlimited" gorm:"default:false"`
	SnapshotStatus     string `json:"snapshot_status" gorm:"type:varchar(32);default:'active';index"`
	StartTime          int64  `json:"start_time" gorm:"type:bigint;default:0"`
	EndTime            int64  `json:"end_time" gorm:"type:bigint;default:0;index"`
	ResetPeriod        string `json:"reset_period" gorm:"type:varchar(16);default:'never'"`
	CreatedAt          int64  `json:"created_at" gorm:"type:bigint"`
	UpdatedAt          int64  `json:"updated_at" gorm:"type:bigint;index"`
}

func (s *UserSubscriptionDailyStat) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	s.CreatedAt = now
	s.UpdatedAt = now
	return nil
}

func (s *UserSubscriptionDailyStat) BeforeUpdate(tx *gorm.DB) error {
	s.UpdatedAt = common.GetTimestamp()
	return nil
}

type UserSubscriptionDailyStatsSummary struct {
	StatDay                 string `json:"stat_day"`
	Total                   int64  `json:"total"`
	Used                    int64  `json:"used"`
	Remain                  int64  `json:"remain"`
	UnlimitedCount          int64  `json:"unlimited_count"`
	ActiveSubscriptionCount int64  `json:"active_subscription_count"`
}

type UserSubscriptionDailyAggregateItem struct {
	StatDay           string `json:"stat_day"`
	Total             int64  `json:"total"`
	Used              int64  `json:"used"`
	Remain            int64  `json:"remain"`
	UnlimitedCount    int64  `json:"unlimited_count"`
	SubscriptionCount int64  `json:"subscription_count"`
	RecordCount       int64  `json:"record_count"`
}

func formatSubscriptionStatDayFromUnix(ts int64) string {
	if ts <= 0 {
		ts = common.GetTimestamp()
	}
	return time.Unix(ts, 0).In(time.Local).Format("2006-01-02")
}

func SubscriptionStatDayKeyFromUnix(ts int64) int64 {
	day := formatSubscriptionStatDayFromUnix(ts)
	value, _ := strconv.ParseInt(strings.ReplaceAll(day, "-", ""), 10, 64)
	return value
}

func normalizeSubscriptionStatDay(day string) string {
	day = strings.TrimSpace(day)
	if day == "" {
		return ""
	}
	t, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func snapshotSubscriptionUsage(sub *UserSubscription) (total int64, used int64, remain int64, unlimited bool) {
	if sub == nil {
		return 0, 0, 0, false
	}
	total = sub.AmountTotal
	if total < 0 {
		total = 0
	}
	used = sub.AmountUsed
	if used < 0 {
		used = 0
	}
	unlimited = total <= 0
	if unlimited {
		return total, used, 0, true
	}
	if used > total {
		used = total
	}
	remain = total - used
	if remain < 0 {
		remain = 0
	}
	return total, used, remain, false
}

func SyncUserSubscriptionDailyStatTx(tx *gorm.DB, sub *UserSubscription, plan *SubscriptionPlan, statUnix int64) error {
	if sub == nil || sub.Id <= 0 {
		return errors.New("invalid user subscription")
	}
	if tx == nil {
		tx = DB
	}
	if plan == nil {
		var err error
		plan, err = getSubscriptionPlanByIdTx(tx, sub.PlanId)
		if err != nil {
			return err
		}
	}

	statDay := formatSubscriptionStatDayFromUnix(statUnix)
	total, used, remain, unlimited := snapshotSubscriptionUsage(sub)
	now := common.GetTimestamp()
	row := UserSubscriptionDailyStat{
		StatDay:            statDay,
		UserId:             sub.UserId,
		UserSubscriptionId: sub.Id,
		PlanId:             sub.PlanId,
		PlanTitle:          strings.TrimSpace(plan.Title),
		AmountTotal:        total,
		AmountUsed:         used,
		AmountRemain:       remain,
		Unlimited:          unlimited,
		SnapshotStatus:     strings.TrimSpace(sub.Status),
		StartTime:          sub.StartTime,
		EndTime:            sub.EndTime,
		ResetPeriod:        NormalizeResetPeriod(plan.QuotaResetPeriod),
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_subscription_id"},
			{Name: "stat_day"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"user_id":         row.UserId,
			"plan_id":         row.PlanId,
			"plan_title":      row.PlanTitle,
			"amount_total":    row.AmountTotal,
			"amount_used":     row.AmountUsed,
			"amount_remain":   row.AmountRemain,
			"unlimited":       row.Unlimited,
			"snapshot_status": row.SnapshotStatus,
			"start_time":      row.StartTime,
			"end_time":        row.EndTime,
			"reset_period":    row.ResetPeriod,
			"updated_at":      row.UpdatedAt,
		}),
	}).Create(&row).Error
}

func EnsureTodaySubscriptionDailyStats(limit int) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	now := GetDBTimestamp()
	statDay := formatSubscriptionStatDayFromUnix(now)

	var subs []UserSubscription
	err := DB.Table("user_subscriptions AS us").
		Select("us.*").
		Joins("LEFT JOIN user_subscription_daily_stats AS ds ON ds.user_subscription_id = us.id AND ds.stat_day = ?", statDay).
		Where("us.status = ? AND us.end_time > ? AND ds.id IS NULL", "active", now).
		Order("us.id asc").
		Limit(limit).
		Find(&subs).Error
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}

	for _, sub := range subs {
		subCopy := sub
		if err := DB.Transaction(func(tx *gorm.DB) error {
			plan, err := getSubscriptionPlanByIdTx(tx, subCopy.PlanId)
			if err != nil {
				return err
			}
			return SyncUserSubscriptionDailyStatTx(tx, &subCopy, plan, now)
		}); err != nil {
			return 0, err
		}
	}
	return len(subs), nil
}

func GetUserSubscriptionDailyStatsSummary(userId int) (*UserSubscriptionDailyStatsSummary, error) {
	if userId <= 0 {
		return nil, errors.New("invalid user id")
	}
	now := GetDBTimestamp()
	type summaryRow struct {
		Total          int64 `gorm:"column:total"`
		Used           int64 `gorm:"column:used"`
		Remain         int64 `gorm:"column:remain"`
		UnlimitedCount int64 `gorm:"column:unlimited_count"`
		ActiveCount    int64 `gorm:"column:active_count"`
	}
	row := &summaryRow{}
	err := DB.Table("user_subscriptions").
		Select(`COALESCE(SUM(CASE WHEN amount_total > 0 THEN amount_total ELSE 0 END), 0) AS total,
COALESCE(SUM(CASE WHEN amount_total > 0 THEN CASE WHEN amount_used < 0 THEN 0 WHEN amount_used > amount_total THEN amount_total ELSE amount_used END ELSE 0 END), 0) AS used,
COALESCE(SUM(CASE WHEN amount_total > 0 THEN CASE WHEN amount_total - amount_used > 0 THEN amount_total - amount_used ELSE 0 END ELSE 0 END), 0) AS remain,
COALESCE(SUM(CASE WHEN amount_total <= 0 THEN 1 ELSE 0 END), 0) AS unlimited_count,
COUNT(*) AS active_count`).
		Where("user_id = ? AND status = ? AND end_time > ?", userId, "active", now).
		Scan(row).Error
	if err != nil {
		return nil, err
	}
	return &UserSubscriptionDailyStatsSummary{
		StatDay:                 formatSubscriptionStatDayFromUnix(now),
		Total:                   row.Total,
		Used:                    row.Used,
		Remain:                  row.Remain,
		UnlimitedCount:          row.UnlimitedCount,
		ActiveSubscriptionCount: row.ActiveCount,
	}, nil
}

func applyUserSubscriptionDailyStatFilters(query *gorm.DB, userId int, startDay string, endDay string, keyword string, status string, onlyUsed bool) *gorm.DB {
	query = query.Where("user_id = ?", userId)
	if startDay = normalizeSubscriptionStatDay(startDay); startDay != "" {
		query = query.Where("stat_day >= ?", startDay)
	}
	if endDay = normalizeSubscriptionStatDay(endDay); endDay != "" {
		query = query.Where("stat_day <= ?", endDay)
	}

	keyword = strings.TrimSpace(keyword)
	if keyword != "" {
		if numericID, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("(user_subscription_id = ? OR plan_id = ? OR plan_title LIKE ?)", numericID, numericID, "%"+keyword+"%")
		} else {
			query = query.Where("plan_title LIKE ?", "%"+keyword+"%")
		}
	}

	switch strings.TrimSpace(status) {
	case "active", "expired", "cancelled", "deleted":
		query = query.Where("snapshot_status = ?", status)
	}

	if onlyUsed {
		query = query.Where("amount_used > ?", 0)
	}
	return query
}

func GetUserSubscriptionDailyAggregatePage(userId int, pageInfo *common.PageInfo, startDay string, endDay string, keyword string, status string, onlyUsed bool) ([]UserSubscriptionDailyAggregateItem, int64, error) {
	if userId <= 0 {
		return nil, 0, errors.New("invalid user id")
	}
	if pageInfo == nil {
		pageInfo = &common.PageInfo{Page: 1, PageSize: common.ItemsPerPage}
	}
	base := applyUserSubscriptionDailyStatFilters(DB.Model(&UserSubscriptionDailyStat{}), userId, startDay, endDay, keyword, status, onlyUsed)

	var total int64
	if err := base.Distinct("stat_day").Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]UserSubscriptionDailyAggregateItem, 0)
	err := base.Select(`stat_day,
COALESCE(SUM(CASE WHEN amount_total > 0 THEN amount_total ELSE 0 END), 0) AS total,
COALESCE(SUM(CASE WHEN amount_used > 0 THEN amount_used ELSE 0 END), 0) AS used,
COALESCE(SUM(CASE WHEN amount_remain > 0 THEN amount_remain ELSE 0 END), 0) AS remain,
COALESCE(SUM(CASE WHEN amount_total <= 0 THEN 1 ELSE 0 END), 0) AS unlimited_count,
COUNT(*) AS subscription_count,
COUNT(*) AS record_count`).
		Group("stat_day").
		Order("stat_day desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Scan(&items).Error
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func GetUserSubscriptionDailyDetailPage(userId int, pageInfo *common.PageInfo, startDay string, endDay string, keyword string, status string, onlyUsed bool) ([]UserSubscriptionDailyStat, int64, error) {
	if userId <= 0 {
		return nil, 0, errors.New("invalid user id")
	}
	if pageInfo == nil {
		pageInfo = &common.PageInfo{Page: 1, PageSize: common.ItemsPerPage}
	}
	base := applyUserSubscriptionDailyStatFilters(DB.Model(&UserSubscriptionDailyStat{}), userId, startDay, endDay, keyword, status, onlyUsed)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]UserSubscriptionDailyStat, 0)
	err := base.Order("stat_day desc").
		Order("amount_used desc").
		Order("user_subscription_id desc").
		Order("id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&items).Error
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
