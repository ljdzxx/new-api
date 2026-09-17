package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/gin-gonic/gin"
)

// GetUserAPIInfo shares the configured endpoints with authenticated token users,
// independently of whether the dashboard API information panel is displayed.
func GetUserAPIInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    console_setting.GetApiInfo(),
	})
}
