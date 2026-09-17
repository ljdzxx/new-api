package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetUsageCompat serves the sub2api /v1/usage wire contract without an API envelope.
func GetUsageCompat(c *gin.Context) {
	tokenValue, _ := c.Get("usage_token")
	userValue, _ := c.Get("usage_user")
	token, tokenOK := tokenValue.(*model.Token)
	user, userOK := userValue.(*model.User)
	if !tokenOK || !userOK || token == nil || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"type": "error", "error": gin.H{"type": "authentication_error", "message": "Invalid API key"}})
		return
	}
	days := 30
	if raw := c.Query("days"); strings.TrimSpace(raw) != "" {
		var err error
		days, err = strconv.Atoi(raw)
		if err != nil || days < 1 || days > 90 {
			c.JSON(http.StatusBadRequest, gin.H{"type": "error", "error": gin.H{"type": "invalid_request_error", "message": "Invalid days, allowed range is 1-90"}})
			return
		}
	}
	now := time.Now()
	start, end := now.AddDate(0, 0, -30), now
	if parsed, err := time.ParseInLocation("2006-01-02", c.Query("start_date"), time.Local); err == nil {
		start = parsed
	}
	if parsed, err := time.ParseInLocation("2006-01-02", c.Query("end_date"), time.Local); err == nil {
		end = parsed.AddDate(0, 0, 1)
	}
	location := time.Local
	if parsed, err := time.LoadLocation(c.Query("timezone")); c.Query("timezone") != "" && err == nil {
		location = parsed
	}
	response := service.BuildUsageCompatBalance(token, user, now)
	summary, daily, models, err := service.BuildUsageCompatStats(c.Request.Context(), user.Id, token.Id, now, start, end, days, location)
	// Statistics are best-effort, as in sub2api: log-store failure must not hide balance.
	if err == nil {
		response["usage"] = summary
		response["daily_usage"] = daily
		if len(models) > 0 {
			response["model_stats"] = models
		}
	}
	c.JSON(http.StatusOK, response)
}
