package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func AdminGetInviteRank(c *gin.Context) {
	rangeKey := c.DefaultQuery("range", model.InviteRankRangeToday)
	start, startErr := parseOptionalInt64Query(c, "start_timestamp")
	end, endErr := parseOptionalInt64Query(c, "end_timestamp")
	if startErr != nil || endErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid timestamp"})
		return
	}

	now := time.Unix(model.GetDBTimestamp(), 0)
	window, err := model.ResolveInviteRankWindow(rangeKey, start, end, now, time.Local)
	if err != nil {
		if errors.Is(err, model.ErrInviteRankInvalidRange) || errors.Is(err, model.ErrInviteRankRangeTooLarge) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}

	items, summary, err := model.GetInviteRank(window)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"items":   items,
		"summary": summary,
		"window":  window,
		"limit":   model.InviteRankLimit,
	})
}
