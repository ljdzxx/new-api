package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func legacyTopupError(c *gin.Context, err error) {
	c.JSON(http.StatusOK, gin.H{
		"message": "error",
		"data":    err.Error(),
	})
}

func legacyTopupSuccess(c *gin.Context, result *types.CheckoutResult) {
	switch result.Provider {
	case types.PaymentProviderEpay:
		form := result.Form
		if form == nil {
			legacyTopupError(c, http.ErrNoLocation)
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": form.Fields, "url": form.URL})
	case types.PaymentProviderStripe:
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"pay_link": result.URL}})
	case types.PaymentProviderCreem:
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"checkout_url": result.URL}})
	default:
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"redirect_url": result.URL}})
	}
}

func legacySubscriptionError(c *gin.Context, err error) {
	common.ApiErrorMsg(c, err.Error())
}

func legacySubscriptionSuccess(c *gin.Context, result *types.CheckoutResult) {
	switch result.Provider {
	case types.PaymentProviderEpay:
		form := result.Form
		if form == nil {
			legacySubscriptionError(c, http.ErrNoLocation)
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": form.Fields, "url": form.URL})
	case types.PaymentProviderStripe:
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"pay_link": result.URL}})
	case types.PaymentProviderCreem:
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"checkout_url": result.URL}})
	default:
		c.JSON(http.StatusOK, gin.H{"message": "success", "data": gin.H{"redirect_url": result.URL}})
	}
}
