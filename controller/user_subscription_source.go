package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func AdminGetUserSubscriptionSourceRecords(c *gin.Context) {
	userID, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || userID <= 0 {
		common.ApiErrorMsg(c, "用户 ID 无效")
		return
	}

	pageInfo := common.GetPageQuery(c)
	items, total, err := model.GetUserSubscriptionSourceRecords(userID, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}
