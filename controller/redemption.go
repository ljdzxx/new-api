package controller

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func normalizeAndValidateRedemptionReward(c *gin.Context, redemption *model.Redemption) (bool, string) {
	if redemption == nil {
		return false, i18n.T(c, i18n.MsgInvalidParams)
	}
	if redemption.CodeType == 0 {
		redemption.CodeType = common.RedemptionCodeTypeNormal
	}
	if redemption.CodeType != common.RedemptionCodeTypeNormal && redemption.CodeType != common.RedemptionCodeTypeWelfare {
		return false, "不支持的兑换码类型"
	}
	if redemption.RewardType == 0 {
		redemption.RewardType = common.RedemptionRewardTypeQuota
	}
	if redemption.PayMoney < 0 {
		return false, "实付金额不能小于 0"
	}

	switch redemption.RewardType {
	case common.RedemptionRewardTypeQuota:
		if redemption.Quota <= 0 {
			return false, "额度必须大于 0"
		}
		redemption.PlanId = 0
		return true, ""
	case common.RedemptionRewardTypeSubscription:
		if redemption.PlanId <= 0 {
			return false, "请选择订阅套餐"
		}
		plan, err := model.GetSubscriptionPlanById(redemption.PlanId)
		if err != nil {
			return false, "订阅套餐不存在"
		}
		if plan == nil || !plan.Enabled {
			return false, "订阅套餐未启用"
		}
		redemption.Quota = 0
		return true, ""
	default:
		return false, "不支持的兑换码奖励类型"
	}
}

func GetAllRedemptions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.GetAllRedemptions(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
	return
}

func SearchRedemptions(c *gin.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.SearchRedemptions(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetRedemption(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	redemption, err := model.GetRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    redemption,
	})
	return
}

func AddRedemption(c *gin.Context) {
	redemption := model.Redemption{}
	err := c.ShouldBindJSON(&redemption)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if utf8.RuneCountInString(redemption.Name) == 0 || utf8.RuneCountInString(redemption.Name) > 20 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionNameLength)
		return
	}
	if redemption.CodeType == 0 {
		redemption.CodeType = common.RedemptionCodeTypeNormal
	}
	if redemption.CodeType == common.RedemptionCodeTypeWelfare {
		redemption.Count = 1
		redemption.Key = strings.TrimSpace(redemption.Key)
		if redemption.Key == "" {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "请输入福利兑换码"})
			return
		}
		if utf8.RuneCountInString(redemption.Key) > 32 {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "福利兑换码长度不能超过 32 个字符"})
			return
		}
	}
	if redemption.Count <= 0 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionCountPositive)
		return
	}
	if redemption.Count > 100 {
		common.ApiErrorI18n(c, i18n.MsgRedemptionCountMax)
		return
	}
	if valid, msg := normalizeAndValidateRedemptionReward(c, &redemption); !valid {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	if valid, msg := validateExpiredTime(c, redemption.ExpiredTime); !valid {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
		return
	}
	var keys []string
	for i := 0; i < redemption.Count; i++ {
		key := common.GetUUID()
		if redemption.CodeType == common.RedemptionCodeTypeWelfare {
			key = redemption.Key
		}
		cleanRedemption := model.Redemption{
			UserId:      c.GetInt("id"),
			Name:        redemption.Name,
			Key:         key,
			CreatedTime: common.GetTimestamp(),
			CodeType:    redemption.CodeType,
			RewardType:  redemption.RewardType,
			Quota:       redemption.Quota,
			PlanId:      redemption.PlanId,
			PayMoney:    redemption.PayMoney,
			ExpiredTime: redemption.ExpiredTime,
		}
		err = cleanRedemption.Insert()
		if err != nil {
			common.SysError("failed to insert redemption: " + err.Error())
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": i18n.T(c, i18n.MsgRedemptionCreateFailed),
				"data":    keys,
			})
			return
		}
		keys = append(keys, key)
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    keys,
	})
	return
}

func DeleteRedemption(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	err := model.DeleteRedemptionById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func UpdateRedemption(c *gin.Context) {
	statusOnly := c.Query("status_only")
	redemption := model.Redemption{}
	err := c.ShouldBindJSON(&redemption)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	cleanRedemption, err := model.GetRedemptionById(redemption.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if statusOnly == "" {
		if redemption.CodeType == 0 {
			redemption.CodeType = cleanRedemption.CodeType
		}
		if valid, msg := normalizeAndValidateRedemptionReward(c, &redemption); !valid {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
			return
		}
		if valid, msg := validateExpiredTime(c, redemption.ExpiredTime); !valid {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": msg})
			return
		}
		// If you add more fields, please also update redemption.Update()
		cleanRedemption.Name = redemption.Name
		cleanRedemption.CodeType = redemption.CodeType
		cleanRedemption.RewardType = redemption.RewardType
		cleanRedemption.Quota = redemption.Quota
		cleanRedemption.PlanId = redemption.PlanId
		cleanRedemption.PayMoney = redemption.PayMoney
		cleanRedemption.ExpiredTime = redemption.ExpiredTime
	}
	if statusOnly != "" {
		if redemption.Status == common.RedemptionCodeStatusEnabled && cleanRedemption.Status == common.RedemptionCodeStatusUsed {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "已使用的兑换码不可重新启用"})
			return
		}
		cleanRedemption.Status = redemption.Status
	}
	err = cleanRedemption.Update()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    cleanRedemption,
	})
	return
}

func DeleteInvalidRedemption(c *gin.Context) {
	rows, err := model.DeleteInvalidRedemptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
	return
}

func validateExpiredTime(c *gin.Context, expired int64) (bool, string) {
	if expired != 0 && expired < common.GetTimestamp() {
		return false, i18n.T(c, i18n.MsgRedemptionExpireTimeInvalid)
	}
	return true, ""
}
