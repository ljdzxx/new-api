package controller

import (
	"fmt"
	"math"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

type userLevelPolicyView struct {
	ID           int      `json:"id"`
	Level        string   `json:"level"`
	Recharge     float64  `json:"recharge"`
	Discount     string   `json:"discount"`
	Icon         string   `json:"icon"`
	Rate         int      `json:"rate"`
	Channel      []string `json:"channel"`
	ChannelText  string   `json:"channel_text"`
	DiscountText string   `json:"discount_text"`
}

func GetSelfUserLevel(c *gin.Context) {
	userId := c.GetInt("id")
	if userId <= 0 {
		common.ApiErrorMsg(c, "invalid user id")
		return
	}

	// Ensure top-up based level upgrade takes effect before building the view.
	if err := model.TryAutoUpgradeUserLevelByRecharge(userId); err != nil {
		common.SysLog(fmt.Sprintf("auto upgrade user level failed: user=%d err=%v", userId, err))
	}

	user, err := model.GetUserById(userId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	totalRecharge, err := model.GetUserTotalRechargeAmount(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	policies := setting.ListUserLevelPolicies()
	views := make([]userLevelPolicyView, 0, len(policies))
	for _, policy := range policies {
		views = append(views, toUserLevelPolicyView(policy))
	}

	current, hasCurrent := resolveCurrentUserLevelPolicy(user.UserLevelId, totalRecharge)
	next, hasNext := findNextUserLevelPolicy(policies, totalRecharge)

	currentRecharge := 0.0
	if hasCurrent {
		currentRecharge = current.Recharge
	}
	nextRecharge := 0.0
	remainingRecharge := 0.0
	progressPercent := 100.0
	if hasNext {
		nextRecharge = next.Recharge
		remainingRecharge = round2(math.Max(next.Recharge-totalRecharge, 0))
		if next.Recharge > currentRecharge {
			raw := (totalRecharge - currentRecharge) / (next.Recharge - currentRecharge)
			progressPercent = round2(math.Max(0, math.Min(1, raw)) * 100)
		}
	}

	data := gin.H{
		"user_id":            user.Id,
		"user_group":         user.Group,
		"user_level_id":      user.UserLevelId,
		"total_recharge":     round2(totalRecharge),
		"current":            nil,
		"next":               nil,
		"progress_percent":   progressPercent,
		"remaining_recharge": remainingRecharge,
		"current_recharge":   round2(currentRecharge),
		"next_recharge":      round2(nextRecharge),
		"levels":             views,
	}
	if hasCurrent {
		data["current"] = toUserLevelPolicyView(current)
	}
	if hasNext {
		data["next"] = toUserLevelPolicyView(next)
	}

	common.ApiSuccess(c, data)
}

func resolveCurrentUserLevelPolicy(userLevelID int, totalRecharge float64) (setting.UserLevelPolicy, bool) {
	if userLevelID > 0 {
		if policy, found := setting.GetUserLevelPolicyByID(userLevelID); found {
			return policy, true
		}
	}
	return setting.GetHighestUserLevelByRecharge(totalRecharge)
}

func findNextUserLevelPolicy(policies []setting.UserLevelPolicy, totalRecharge float64) (setting.UserLevelPolicy, bool) {
	for _, policy := range policies {
		if policy.Recharge > totalRecharge {
			return policy, true
		}
	}
	return setting.UserLevelPolicy{}, false
}

func toUserLevelPolicyView(policy setting.UserLevelPolicy) userLevelPolicyView {
	return userLevelPolicyView{
		ID:           policy.ID,
		Level:        policy.Level,
		Recharge:     round2(policy.Recharge),
		Discount:     policy.Discount,
		Icon:         policy.Icon,
		Rate:         policy.Rate,
		Channel:      policy.Channel,
		ChannelText:  buildChannelText(policy.Channel),
		DiscountText: buildDiscountText(policy.Discount),
	}
}

func buildChannelText(channels []string) string {
	if len(channels) == 0 {
		return "所有渠道"
	}
	return fmt.Sprintf("%d个渠道", len(channels))
}

func buildDiscountText(discount string) string {
	if discount == "" || discount == "0" {
		return "无折扣"
	}
	value, err := strconv.ParseFloat(discount, 64)
	if err != nil || value <= 0 {
		return "无折扣"
	}
	return fmt.Sprintf("%.0f%% OFF", value*100)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
