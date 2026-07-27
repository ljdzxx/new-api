package model

import (
	"errors"

	"github.com/QuantumNous/new-api/setting"
	"gorm.io/gorm"
)

func applyGroupRateLimitToUser(user *User) bool {
	if user == nil {
		return false
	}
	rate, found := setting.GetGroupUserRateLimit(user.Group)
	if !found {
		return false
	}

	user.RateLimitEnabled = true
	user.RateLimitDurationMinutes = 1
	if rate.Max != nil {
		user.RateLimitCount = *rate.Max
	}
	if rate.Success != nil {
		user.RateLimitSuccessCount = *rate.Success
	}
	return true
}

func updateUserGroupTx(tx *gorm.DB, userId int, group string) error {
	if tx == nil || userId <= 0 {
		return errors.New("invalid user group update args")
	}

	updates := map[string]interface{}{
		"group": group,
	}
	if rate, found := setting.GetGroupUserRateLimit(group); found {
		updates["rate_limit_enabled"] = true
		updates["rate_limit_duration_minutes"] = 1
		if rate.Max != nil {
			updates["rate_limit_count"] = *rate.Max
		}
		if rate.Success != nil {
			updates["rate_limit_success_count"] = *rate.Success
		}
	}
	return tx.Model(&User{}).Where("id = ?", userId).Updates(updates).Error
}
