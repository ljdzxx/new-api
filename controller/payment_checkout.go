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
		"epay_channels":       meta.EpayChannels,
		"usd_exchange_rate":   operation_setting.USDExchangeRate,
		"epay_amount_options": operation_setting.GetEpaySetting().AmountOptions,
		"stripe_min_topup":    setting.StripeMinTopUp,
		"stripe_unit_price":   setting.StripeUnitPrice,
		"amount_options":      operation_setting.GetPaymentSetting().AmountOptions,
		"discount":            operation_setting.GetPaymentSetting().AmountDiscount,
		"mall_links":          operation_setting.GetPaymentSetting().MallLinks,
		"pay_methods":         paymentOtherMethods(),
		"creem_products":      setting.CreemProducts,
		"enable_online_topup": len(meta.EpayChannels) > 0,
		"enable_stripe_topup": setting.StripeApiSecret != "" && setting.StripeWebhookSecret != "" && setting.StripePriceId != "",
		"enable_creem_topup":  setting.CreemApiKey != "" && setting.CreemProducts != "[]",
	})
}

func paymentOtherMethods() []map[string]string {
	methods := make([]map[string]string, 0)
	if setting.StripeApiSecret != "" && setting.StripeWebhookSecret != "" && setting.StripePriceId != "" {
		methods = append(methods, map[string]string{"type": "stripe", "name": "Stripe"})
	}
	if len(operation_setting.GetPaymentSetting().MallLinks) > 0 {
		methods = append(methods, map[string]string{"type": "mall", "name": "商城"})
	}
	return methods
}

func CreatePaymentTopupCheckout(c *gin.Context) {
	var req types.TopupCheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	req.UserID = c.GetInt("id")
	if returnURL, err := service.BuildPaymentReturnURL(c.Request, "/console/log"); err == nil {
		req.ReturnURL = returnURL
	}
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
	if returnURL, err := service.BuildPaymentReturnURL(c.Request, "/api/subscription/epay/return"); err == nil {
		req.ReturnURL = returnURL
	}
	result, err := service.CreateSubscriptionCheckout(plan, &req)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
