package controller

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func AdminGetConsumptionRank(c *gin.Context) {
	rangeKey := c.DefaultQuery("range", model.ConsumptionRankRangeToday)
	start, startErr := parseOptionalInt64Query(c, "start_timestamp")
	end, endErr := parseOptionalInt64Query(c, "end_timestamp")
	if startErr != nil || endErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid timestamp"})
		return
	}

	now := time.Unix(model.GetDBTimestamp(), 0)
	window, err := model.ResolveConsumptionRankWindow(rangeKey, start, end, now, time.Local)
	if err != nil {
		if errors.Is(err, model.ErrConsumptionRankInvalidRange) || errors.Is(err, model.ErrConsumptionRankRangeTooLarge) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}

	items, summary, err := model.GetConsumptionRank(window)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"items":   items,
		"summary": summary,
		"window":  window,
		"limit":   model.ConsumptionRankLimit,
	})
}

func parseOptionalInt64Query(c *gin.Context, key string) (int64, error) {
	value := c.Query(key)
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}
