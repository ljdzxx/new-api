package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func AdminListEpayChannels(context *gin.Context) {
	channels, err := service.ListAdminEpayChannels()
	if err != nil {
		common.ApiError(context, err)
		return
	}
	common.ApiSuccess(context, channels)
}

func AdminSaveEpayChannel(context *gin.Context) {
	id := 0
	if context.Param("id") != "" {
		parsedID, err := strconv.Atoi(context.Param("id"))
		if err != nil || parsedID <= 0 {
			common.ApiErrorMsg(context, "渠道 ID 无效")
			return
		}
		id = parsedID
	}
	var input service.EpayChannelInput
	if err := common.DecodeJson(context.Request.Body, &input); err != nil {
		common.ApiErrorMsg(context, "渠道配置格式无效")
		return
	}
	if err := service.SaveEpayChannel(id, &input); err != nil {
		common.ApiError(context, err)
		return
	}
	common.ApiSuccess(context, nil)
}

func GetEpayQuote(context *gin.Context) {
	var input struct {
		ChannelID int                `json:"epay_channel_id"`
		Method    string             `json:"payment_method"`
		Scene     types.PaymentScene `json:"scene"`
		AmountCNY string             `json:"amount_cny"`
		PlanID    int                `json:"plan_id"`
	}
	if err := common.DecodeJson(context.Request.Body, &input); err != nil {
		common.ApiErrorMsg(context, "报价参数无效")
		return
	}
	var plan *model.SubscriptionPlan
	if input.Scene == types.PaymentSceneSubscription {
		loaded, err := model.GetSubscriptionPlanById(input.PlanID)
		if err != nil || !loaded.Enabled {
			common.ApiErrorMsg(context, "套餐不存在或未启用")
			return
		}
		plan = loaded
	}
	quote, err := service.QuoteEpay(input.ChannelID, input.Method, input.Scene, input.AmountCNY, plan)
	if err != nil {
		common.ApiError(context, err)
		return
	}
	common.ApiSuccess(context, quote)
}

func AdminReorderEpayChannels(context *gin.Context) {
	var input struct {
		ChannelIDs  []int `json:"channel_ids"`
		ExpectedIDs []int `json:"expected_ids"`
	}
	if err := common.DecodeJson(context.Request.Body, &input); err != nil {
		common.ApiErrorMsg(context, "渠道排序参数无效")
		return
	}
	if err := model.ReorderEpayChannels(input.ChannelIDs, input.ExpectedIDs); err != nil {
		common.ApiError(context, err)
		return
	}
	common.ApiSuccess(context, nil)
}

func AdminSetEpayChannelEnabled(context *gin.Context) {
	id, err := strconv.Atoi(context.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(context, "渠道 ID 无效")
		return
	}
	var input struct {
		Enabled    *bool `json:"enabled"`
		RevisionID int   `json:"revision_id"`
	}
	if err := common.DecodeJson(context.Request.Body, &input); err != nil || input.Enabled == nil {
		common.ApiErrorMsg(context, "渠道状态参数无效")
		return
	}
	if err := model.SetEpayChannelEnabled(id, *input.Enabled, input.RevisionID); err != nil {
		common.ApiError(context, err)
		return
	}
	common.ApiSuccess(context, nil)
}
