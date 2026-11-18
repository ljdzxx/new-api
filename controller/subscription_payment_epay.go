package controller

import (
	"fmt"
	"log"
	"net/http"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type SubscriptionEpayPayRequest struct {
	PlanId        int    `json:"plan_id"`
	EpayChannelID int    `json:"epay_channel_id"`
	QuoteToken    string `json:"quote_token"`
	PaymentMethod string `json:"payment_method"`
}

func SubscriptionRequestEpay(context *gin.Context) {
	SubscriptionRequestEpayCompat(context)
}

func readEpayCallbackParams(context *gin.Context) (map[string]string, error) {
	if err := context.Request.ParseForm(); err != nil {
		return nil, err
	}
	params := make(map[string]string)
	for name, values := range context.Request.Form {
		if len(values) != 1 {
			return nil, fmt.Errorf("重复回调参数")
		}
		params[name] = values[0]
	}
	if params["out_trade_no"] == "" {
		return nil, fmt.Errorf("缺少订单号")
	}
	return params, nil
}

func completeEpaySubscriptionCallback(context *gin.Context) error {
	params, err := readEpayCallbackParams(context)
	if err != nil {
		return err
	}
	order := model.GetSubscriptionOrderByTradeNo(params["out_trade_no"])
	if order == nil {
		return model.ErrSubscriptionOrderNotFound
	}
	revision, err := model.GetEpayChannelRevision(order.EpayChannelRevisionId)
	if err != nil || revision.ChannelId != order.EpayChannelId {
		return fmt.Errorf("订单渠道无效")
	}
	client, err := service.NewEpayClient(revision)
	if err != nil {
		return err
	}
	if params["pid"] != revision.MerchantId {
		return fmt.Errorf("支付回调商户不匹配")
	}
	verified, err := client.Verify(params)
	if err != nil || !verified.VerifyStatus || verified.TradeStatus != epay.StatusTradeSuccess {
		return fmt.Errorf("支付回调签名或状态无效")
	}
	if err := validateEpaySubscriptionCallback(context.Request.Context(), verified, order); err != nil {
		return err
	}
	err = model.CompleteSubscriptionOrder(order.TradeNo, common.GetJsonString(verified), model.PaymentProviderEpay, verified.Type)
	if err != nil {
		log.Printf("易支付订阅已核实支付通知，发放失败，请核对 order=%s err=%v", order.TradeNo, err)
	}
	return err
}

func SubscriptionEpayNotify(context *gin.Context) {
	if err := completeEpaySubscriptionCallback(context); err != nil {
		_, _ = context.Writer.Write([]byte("fail"))
		return
	}
	_, _ = context.Writer.Write([]byte("success"))
}

func SubscriptionEpayReturn(context *gin.Context) {
	if err := completeEpaySubscriptionCallback(context); err != nil {
		context.Redirect(http.StatusFound, "/console/topup?pay=fail")
		return
	}
	context.Redirect(http.StatusFound, "/console/topup?pay=success")
}
