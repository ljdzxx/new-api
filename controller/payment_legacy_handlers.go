package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func RequestEpayCompat(c *gin.Context) {
	var req EpayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		legacyTopupError(c, err)
		return
	}
	result, err := service.CreateTopupCheckoutWithProvider(types.PaymentProviderEpay, &types.TopupCheckoutRequest{
		UserID:        c.GetInt("id"),
		Amount:        req.Amount,
		PaymentMethod: req.PaymentMethod,
	})
	if err != nil {
		legacyTopupError(c, err)
		return
	}
	legacyTopupSuccess(c, result)
}

func RequestStripePayCompat(c *gin.Context) {
	var req StripePayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		legacyTopupError(c, err)
		return
	}
	result, err := service.CreateTopupCheckoutWithProvider(types.PaymentProviderStripe, &types.TopupCheckoutRequest{
		UserID:        c.GetInt("id"),
		Amount:        req.Amount,
		PaymentMethod: req.PaymentMethod,
		SuccessURL:    req.SuccessURL,
		CancelURL:     req.CancelURL,
	})
	if err != nil {
		legacyTopupError(c, err)
		return
	}
	legacyTopupSuccess(c, result)
}

func RequestCreemPayCompat(c *gin.Context) {
	var req CreemPayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		legacyTopupError(c, err)
		return
	}
	result, err := service.CreateTopupCheckoutWithProvider(types.PaymentProviderCreem, &types.TopupCheckoutRequest{
		UserID:        c.GetInt("id"),
		ProductID:     req.ProductId,
		PaymentMethod: req.PaymentMethod,
	})
	if err != nil {
		legacyTopupError(c, err)
		return
	}
	legacyTopupSuccess(c, result)
}

func SubscriptionRequestEpayCompat(c *gin.Context) {
	var req SubscriptionEpayPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.CreateSubscriptionCheckoutWithProvider(types.PaymentProviderEpay, plan, &types.SubscriptionCheckoutRequest{
		UserID:        c.GetInt("id"),
		PlanID:        req.PlanId,
		PaymentMethod: req.PaymentMethod,
	})
	if err != nil {
		legacySubscriptionError(c, err)
		return
	}
	legacySubscriptionSuccess(c, result)
}

func SubscriptionRequestStripePayCompat(c *gin.Context) {
	var req SubscriptionStripePayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.CreateSubscriptionCheckoutWithProvider(types.PaymentProviderStripe, plan, &types.SubscriptionCheckoutRequest{
		UserID: c.GetInt("id"),
		PlanID: req.PlanId,
	})
	if err != nil {
		legacySubscriptionError(c, err)
		return
	}
	legacySubscriptionSuccess(c, result)
}

func SubscriptionRequestCreemPayCompat(c *gin.Context) {
	var req SubscriptionCreemPayRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.CreateSubscriptionCheckoutWithProvider(types.PaymentProviderCreem, plan, &types.SubscriptionCheckoutRequest{
		UserID: c.GetInt("id"),
		PlanID: req.PlanId,
	})
	if err != nil {
		legacySubscriptionError(c, err)
		return
	}
	legacySubscriptionSuccess(c, result)
}
