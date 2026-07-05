package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func parseQueryInt(c *gin.Context, key string) int {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return 0
	}
	n, _ := strconv.Atoi(value)
	return n
}

func parseQueryInt64(c *gin.Context, key string) int64 {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return 0
	}
	n, _ := strconv.ParseInt(value, 10, 64)
	return n
}

func AdminGetInviteRewardAudits(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	filter := model.InviteRewardAuditFilter{
		InviterId:    parseQueryInt(c, "inviter_id"),
		InviteeId:    parseQueryInt(c, "invitee_id"),
		RewardStatus: strings.TrimSpace(c.Query("reward_status")),
		MinRiskScore: parseQueryInt(c, "min_risk_score"),
		MaxRiskScore: parseQueryInt(c, "max_risk_score"),
		StartTime:    parseQueryInt64(c, "start_time"),
		EndTime:      parseQueryInt64(c, "end_time"),
	}
	audits, total, err := model.GetInviteRewardAudits(pageInfo, filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(audits)
	common.ApiSuccess(c, pageInfo)
}
