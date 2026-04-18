package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func AdminGetSubscriptionUsageRank(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	rangeKey := c.DefaultQuery("range", model.SubscriptionUsageRankRange1Day)
	keyword := c.Query("keyword")
	sortBy := strings.TrimSpace(c.DefaultQuery("sort_by", "usage_amount"))
	sortOrder := strings.TrimSpace(c.DefaultQuery("sort_order", "desc"))

	items, total, summary, err := model.GetSubscriptionUsageRankPage(pageInfo, rangeKey, keyword, sortBy, sortOrder)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)

	common.ApiSuccess(c, gin.H{
		"summary": summary,
		"page":    pageInfo,
	})
}
