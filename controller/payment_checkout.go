package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type subscriptionMetaQuery struct {
	PlanID int `form:"plan_id"`
}

func GetPaymentTopupMeta(c *gin.Context) {
	meta, err := service.GetTopupMeta()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"provider_meta":       meta,
		"price":               operation_setting.Price,
		"min_topup":           operation_setting.MinTopUp,
		"stripe_min_topup":    setting.StripeMinTopUp,
		"amount_options":      operation_setting.GetPaymentSetting().AmountOptions,
		"discount":            operation_setting.GetPaymentSetting().AmountDiscount,
		"mall_links":          operation_setting.GetPaymentSetting().MallLinks,
		"pay_methods":         operation_setting.PayMethods,
		"creem_products":      setting.CreemProducts,
		"enable_online_topup": operation_setting.PayAddress != "" && operation_setting.EpayId != "" && operation_setting.EpayKey != "",
		"enable_stripe_topup": setting.StripeApiSecret != "" && setting.StripeWebhookSecret != "" && setting.StripePriceId != "",
		"enable_creem_topup":  setting.CreemApiKey != "" && setting.CreemProducts != "[]",
	})
}

func CreatePaymentTopupCheckout(c *gin.Context) {
	var req types.TopupCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	req.UserID = c.GetInt("id")
	result, err := service.CreateTopupCheckout(&req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func GetPaymentSubscriptionMeta(c *gin.Context) {
	var query subscriptionMetaQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	var plan *model.SubscriptionPlan
	if query.PlanID > 0 {
		loadedPlan, err := model.GetSubscriptionPlanById(query.PlanID)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		plan = loadedPlan
	}

	meta, err := service.GetSubscriptionMeta(plan)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	resp := gin.H{
		"provider_meta": meta,
	}
	if plan != nil {
		resp["plan_id"] = plan.Id
	}
	common.ApiSuccess(c, resp)
}

func CreatePaymentSubscriptionCheckout(c *gin.Context) {
	var req types.SubscriptionCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanID <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	req.UserID = c.GetInt("id")
	result, err := service.CreateSubscriptionCheckout(plan, &req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
