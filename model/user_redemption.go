package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type UserRedemptionRecord struct {
	Id                    int    `json:"id"`
	Key                   string `json:"key"`
	RewardType            int    `json:"reward_type"`
	PlanId                int    `json:"plan_id"`
	PlanTitle             string `json:"plan_title"`
	RedeemedTime          int64  `json:"redeemed_time"`
	SubscriptionStartTime int64  `json:"subscription_start_time"`
	SubscriptionEndTime   int64  `json:"subscription_end_time"`
	SubscriptionStatus    string `json:"subscription_status"`
}

func GetRedemptionCountsByUserIDs(userIDs []int) (map[int]int64, error) {
	result := make(map[int]int64)
	if len(userIDs) == 0 {
		return result, nil
	}

	type countRow struct {
		UsedUserId int   `gorm:"column:used_user_id"`
		Count      int64 `gorm:"column:redemption_count"`
	}
	rows := make([]countRow, 0)
	err := DB.Model(&Redemption{}).
		Select("used_user_id, COUNT(*) AS redemption_count").
		Where("used_user_id IN ? AND status = ?", userIDs, common.RedemptionCodeStatusUsed).
		Group("used_user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.UsedUserId] = row.Count
	}
	return result, nil
}

func normalizeUserRedemptionStatus(rewardType int, rawStatus string, endTime int64) string {
	if rewardType != common.RedemptionRewardTypeSubscription {
		return "quota"
	}
	status := strings.TrimSpace(rawStatus)
	if status == "" {
		return "unknown"
	}
	if status == "active" && endTime > 0 && endTime <= GetDBTimestamp() {
		return "expired"
	}
	return status
}

func applyUserRedemptionStatusFilter(query *gorm.DB, status string) *gorm.DB {
	now := GetDBTimestamp()
	switch strings.TrimSpace(status) {
	case "quota":
		return query.Where("r.reward_type <> ?", common.RedemptionRewardTypeSubscription)
	case "active":
		return query.Where("r.reward_type = ? AND us.status = ? AND us.end_time > ?", common.RedemptionRewardTypeSubscription, "active", now)
	case "expired":
		return query.Where(
			`r.reward_type = ? AND (
us.status = ? OR
(us.status = ? AND us.end_time > 0 AND us.end_time <= ?)
)`,
			common.RedemptionRewardTypeSubscription,
			"expired",
			"active",
			now,
		)
	case "cancelled":
		return query.Where("r.reward_type = ? AND us.status = ?", common.RedemptionRewardTypeSubscription, "cancelled")
	case "unknown":
		return query.Where("r.reward_type = ? AND COALESCE(us.status, '') = ''", common.RedemptionRewardTypeSubscription)
	default:
		return query
	}
}

func buildUserRedemptionRecordsQuery(userId int) *gorm.DB {
	return DB.Table("redemptions AS r").
		Joins("LEFT JOIN subscription_plans AS sp ON sp.id = r.plan_id").
		Joins(`LEFT JOIN user_subscriptions AS us ON
(us.redemption_id = r.id OR (
us.redemption_id = 0 AND
r.reward_type = ? AND
us.user_id = r.used_user_id AND
us.plan_id = r.plan_id AND
us.source = ? AND
ABS(us.created_at - r.redeemed_time) <= ?
))`, common.RedemptionRewardTypeSubscription, "redemption", 5).
		Where("r.used_user_id = ? AND r.status = ?", userId, common.RedemptionCodeStatusUsed)
}

func GetUserRedemptionRecords(userId int, pageInfo *common.PageInfo, status string) ([]UserRedemptionRecord, int64, error) {
	if userId <= 0 {
		return nil, 0, errors.New("invalid user id")
	}
	if pageInfo == nil {
		pageInfo = &common.PageInfo{Page: 1, PageSize: common.ItemsPerPage}
	}

	base := applyUserRedemptionStatusFilter(buildUserRedemptionRecordsQuery(userId), status)

	var total int64
	if err := base.Distinct("r.id").Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]UserRedemptionRecord, 0)
	err := base.
		Select(`r.id,
r.key,
r.reward_type,
r.plan_id,
COALESCE(sp.title, '') AS plan_title,
r.redeemed_time,
COALESCE(us.start_time, 0) AS subscription_start_time,
COALESCE(us.end_time, 0) AS subscription_end_time,
COALESCE(us.status, '') AS subscription_status`).
		Order("r.redeemed_time desc").
		Order("r.id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Scan(&items).Error
	if err != nil {
		return nil, 0, err
	}

	for i := range items {
		items[i].SubscriptionStatus = normalizeUserRedemptionStatus(
			items[i].RewardType,
			items[i].SubscriptionStatus,
			items[i].SubscriptionEndTime,
		)
	}
	return items, total, nil
}

func FillUsersRedemptionCount(users []*User) error {
	if len(users) == 0 {
		return nil
	}
	userIDs := make([]int, 0, len(users))
	for _, user := range users {
		if user == nil || user.Id <= 0 {
			continue
		}
		userIDs = append(userIDs, user.Id)
	}
	if len(userIDs) == 0 {
		return nil
	}
	counts, err := GetRedemptionCountsByUserIDs(userIDs)
	if err != nil {
		return err
	}
	for _, user := range users {
		if user == nil {
			continue
		}
		user.RedemptionCount = counts[user.Id]
	}
	return nil
}

func FillUsersDailySubscriptionQuota(users []*User) error {
	if len(users) == 0 {
		return nil
	}
	userIDs := make([]int, 0, len(users))
	for _, user := range users {
		if user == nil || user.Id <= 0 {
			continue
		}
		userIDs = append(userIDs, user.Id)
	}
	if len(userIDs) == 0 {
		return nil
	}
	statsByUserID, err := GetDailySubscriptionQuotaStatsByUserIDs(userIDs)
	if err != nil {
		return err
	}
	for _, user := range users {
		if user == nil {
			continue
		}
		stat, ok := statsByUserID[user.Id]
		if !ok {
			user.DailySubscriptionTotal = 0
			user.DailySubscriptionRemain = 0
			user.DailySubscriptionUnlimited = false
			continue
		}
		user.DailySubscriptionTotal = stat.Total
		user.DailySubscriptionRemain = stat.Remain
		user.DailySubscriptionUnlimited = stat.Unlimited
	}
	return nil
}

func FillUsersListingExtras(users []*User) error {
	if err := FillUsersDailySubscriptionQuota(users); err != nil {
		return err
	}
	if err := FillUsersRedemptionCount(users); err != nil {
		return err
	}
	return nil
}
