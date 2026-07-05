package controller

import (
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const registerRiskCollectMaxBytes = 16 << 10

func CreateRegisterRiskChallenge(c *gin.Context) {
	challenge, err := model.CreateRegisterRiskChallenge(c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, challenge)
}

func CollectRegisterRiskToken(c *gin.Context) {
	challengeId := strings.TrimSpace(c.Param("challenge_id"))
	if challengeId == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, registerRiskCollectMaxBytes+1))
	if err != nil || len(body) == 0 || len(body) > registerRiskCollectMaxBytes {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	token, err := model.CollectRegisterRiskToken(challengeId, string(body), c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgInvalidParams),
		})
		return
	}
	common.ApiSuccess(c, token)
}
