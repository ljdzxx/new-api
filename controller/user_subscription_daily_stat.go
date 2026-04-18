package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func AdminGetUserSubscriptionDailyStats(c *gin.Context) {
	userID, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || userID <= 0 {
		common.ApiErrorMsg(c, "用户 ID 无效")
		return
	}

	view := strings.TrimSpace(c.DefaultQuery("view", "daily"))
	if view != "daily" && view != "detail" {
		common.ApiErrorMsg(c, "视图参数无效")
		return
	}

	pageInfo := common.GetPageQuery(c)
	startDay := c.Query("start_day")
	endDay := c.Query("end_day")
	keyword := c.Query("keyword")
	status := c.Query("status")
	onlyUsed := strings.EqualFold(strings.TrimSpace(c.Query("only_used")), "true")

	summary, err := model.GetUserSubscriptionDailyStatsSummary(userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if view == "daily" {
		items, total, err := model.GetUserSubscriptionDailyAggregatePage(userID, pageInfo, startDay, endDay, keyword, status, onlyUsed)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		pageInfo.SetTotal(int(total))
		pageInfo.SetItems(items)
		common.ApiSuccess(c, gin.H{
			"summary": summary,
			"view":    view,
			"page":    pageInfo,
		})
		return
	}

	items, total, err := model.GetUserSubscriptionDailyDetailPage(userID, pageInfo, startDay, endDay, keyword, status, onlyUsed)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, gin.H{
		"summary": summary,
		"view":    view,
		"page":    pageInfo,
	})
}
