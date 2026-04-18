package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

type UserSubscriptionSourceRecord struct {
	Id                 int    `json:"id"`
	UserSubscriptionId int    `json:"user_subscription_id"`
	PlanId             int    `json:"plan_id"`
	PlanTitle          string `json:"plan_title"`
	Source             string `json:"source"`
	SourceLabel        string `json:"source_label"`
	SourceDetail       string `json:"source_detail"`
	StartTime          int64  `json:"start_time"`
	EndTime            int64  `json:"end_time"`
	CreatedAt          int64  `json:"created_at"`
	Status             string `json:"status"`
}

func normalizeSubscriptionSourceLabel(source string) string {
	switch strings.TrimSpace(source) {
	case "admin":
		return "admin"
	case "redemption":
		return "redemption"
	case "order":
		return "order"
	default:
		return "unknown"
	}
}

func buildSubscriptionSourceDetail(source string, sub *UserSubscription) string {
	if sub == nil {
		return ""
	}
	switch normalizeSubscriptionSourceLabel(source) {
	case "admin":
		return ""
	case "redemption":
		if sub.RedemptionId > 0 {
			var redemption Redemption
			if err := DB.Select("id", commonKeyCol, "used_user_id", "plan_id", "reward_type", "redeemed_time").
				Where("id = ?", sub.RedemptionId).
				First(&redemption).Error; err == nil {
				return strings.TrimSpace(redemption.Key)
			}
		}

		var redemption Redemption
		if err := DB.Select("id", commonKeyCol, "used_user_id", "plan_id", "reward_type", "redeemed_time").
			Where(
				"used_user_id = ? AND plan_id = ? AND reward_type = ? AND status = ?",
				sub.UserId,
				sub.PlanId,
				common.RedemptionRewardTypeSubscription,
				common.RedemptionCodeStatusUsed,
			).
			Order(fmt.Sprintf("ABS(redeemed_time - %d) asc", sub.CreatedAt)).
			Order("id desc").
			First(&redemption).Error; err == nil {
			return strings.TrimSpace(redemption.Key)
		}
		return ""
	case "order":
		var order SubscriptionOrder
		if err := DB.Select("id", "trade_no", "payment_method", "status", "create_time", "complete_time").
			Where(
				"user_id = ? AND plan_id = ? AND status = ?",
				sub.UserId,
				sub.PlanId,
				common.TopUpStatusSuccess,
			).
			Order(fmt.Sprintf("ABS(COALESCE(complete_time, create_time, 0) - %d) asc", sub.CreatedAt)).
			Order("id desc").
			First(&order).Error; err == nil {
			method := strings.TrimSpace(order.PaymentMethod)
			if method != "" {
				return method
			}
			return strings.TrimSpace(order.TradeNo)
		}
		return ""
	default:
		return strings.TrimSpace(sub.Source)
	}
}

func normalizeSubscriptionStatus(status string, endTime int64) string {
	trimmed := strings.TrimSpace(status)
	if trimmed == "" {
		return "unknown"
	}
	if trimmed == "active" && endTime > 0 && endTime <= GetDBTimestamp() {
		return "expired"
	}
	return trimmed
}

func GetUserSubscriptionSourceRecords(userId int, pageInfo *common.PageInfo) ([]UserSubscriptionSourceRecord, int64, error) {
	if userId <= 0 {
		return nil, 0, errors.New("invalid user id")
	}
	if pageInfo == nil {
		pageInfo = &common.PageInfo{Page: 1, PageSize: common.ItemsPerPage}
	}

	base := DB.Model(&UserSubscription{}).Where("user_id = ?", userId)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	type subscriptionSourceRow struct {
		Id           int    `gorm:"column:id"`
		UserId       int    `gorm:"column:user_id"`
		PlanId       int    `gorm:"column:plan_id"`
		PlanTitle    string `gorm:"column:plan_title"`
		Source       string `gorm:"column:source"`
		StartTime    int64  `gorm:"column:start_time"`
		EndTime      int64  `gorm:"column:end_time"`
		CreatedAt    int64  `gorm:"column:created_at"`
		Status       string `gorm:"column:status"`
		RedemptionId int    `gorm:"column:redemption_id"`
	}

	rows := make([]subscriptionSourceRow, 0)
	err := DB.Table("user_subscriptions AS us").
		Select(`us.id,
us.user_id,
us.plan_id,
COALESCE(sp.title, '') AS plan_title,
COALESCE(us.source, '') AS source,
us.start_time,
us.end_time,
us.created_at,
COALESCE(us.status, '') AS status,
COALESCE(us.redemption_id, 0) AS redemption_id`).
		Joins("LEFT JOIN subscription_plans AS sp ON sp.id = us.plan_id").
		Where("us.user_id = ?", userId).
		Order("us.created_at desc").
		Order("us.id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	items := make([]UserSubscriptionSourceRecord, 0, len(rows))
	for _, row := range rows {
		sub := &UserSubscription{
			Id:           row.Id,
			UserId:       row.UserId,
			PlanId:       row.PlanId,
			RedemptionId: row.RedemptionId,
			Source:       row.Source,
			StartTime:    row.StartTime,
			EndTime:      row.EndTime,
			CreatedAt:    row.CreatedAt,
			Status:       row.Status,
		}
		source := normalizeSubscriptionSourceLabel(row.Source)
		items = append(items, UserSubscriptionSourceRecord{
			Id:                 row.Id,
			UserSubscriptionId: row.Id,
			PlanId:             row.PlanId,
			PlanTitle:          row.PlanTitle,
			Source:             source,
			SourceLabel:        source,
			SourceDetail:       buildSubscriptionSourceDetail(source, sub),
			StartTime:          row.StartTime,
			EndTime:            row.EndTime,
			CreatedAt:          row.CreatedAt,
			Status:             normalizeSubscriptionStatus(row.Status, row.EndTime),
		})
	}
	return items, total, nil
}
