package controller

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/gin-gonic/gin"
)

func GetTopUpInfo(context *gin.Context) {
	GetPaymentTopupMeta(context)
}

type EpayRequest struct {
	EpayChannelID int    `json:"epay_channel_id"`
	AmountCNY     string `json:"amount_cny"`
	QuoteToken    string `json:"quote_token"`
	PaymentMethod string `json:"payment_method"`
}

type AmountRequest struct {
	Amount int64 `json:"amount"`
}

func RequestEpay(context *gin.Context) {
	RequestEpayCompat(context)
}

var orderLocks sync.Map
var orderLocksMutex sync.Mutex

func LockOrder(tradeNo string) {
	lock, exists := orderLocks.Load(tradeNo)
	if !exists {
		orderLocksMutex.Lock()
		lock, exists = orderLocks.Load(tradeNo)
		if !exists {
			lock = new(sync.Mutex)
			orderLocks.Store(tradeNo, lock)
		}
		orderLocksMutex.Unlock()
	}
	lock.(*sync.Mutex).Lock()
}

func UnlockOrder(tradeNo string) {
	if lock, exists := orderLocks.Load(tradeNo); exists {
		lock.(*sync.Mutex).Unlock()
	}
}

func EpayNotify(context *gin.Context) {
	params, err := readEpayCallbackParams(context)
	if err != nil {
		_, _ = context.Writer.Write([]byte("fail"))
		return
	}
	topup := model.GetTopUpByTradeNo(params["out_trade_no"])
	if topup == nil {
		_, _ = context.Writer.Write([]byte("fail"))
		return
	}
	revision, err := model.GetEpayChannelRevision(topup.EpayChannelRevisionId)
	if err != nil || revision.ChannelId != topup.EpayChannelId {
		_, _ = context.Writer.Write([]byte("fail"))
		return
	}
	client, err := service.NewEpayClient(revision)
	if err != nil {
		_, _ = context.Writer.Write([]byte("fail"))
		return
	}
	if params["pid"] != revision.MerchantId {
		_, _ = context.Writer.Write([]byte("fail"))
		return
	}
	verified, err := client.Verify(params)
	if err != nil || !verified.VerifyStatus || verified.TradeStatus != epay.StatusTradeSuccess {
		_, _ = context.Writer.Write([]byte("fail"))
		return
	}
	if err := validateEpayTopUpCallback(context.Request.Context(), verified, topup); err != nil {
		log.Printf("易支付回调校验失败 order=%s err=%v", topup.TradeNo, err)
		_, _ = context.Writer.Write([]byte("fail"))
		return
	}
	if err := model.CompleteEpayTopup(topup.TradeNo); err != nil {
		log.Printf("易支付入账失败 order=%s err=%v", topup.TradeNo, err)
		_, _ = context.Writer.Write([]byte("fail"))
		return
	}
	_, _ = context.Writer.Write([]byte("success"))
}

func RequestAmount(context *gin.Context) {
	GetEpayQuote(context)
}

func GetUserTopUps(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	var (
		topups []*model.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = model.SearchUserTopUps(userId, keyword, pageInfo)
	} else {
		topups, total, err = model.GetUserTopUps(userId, pageInfo)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	common.ApiSuccess(c, pageInfo)
}

// GetAllTopUps 管理员获取全平台充值记录
func GetAllTopUps(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId, _ := strconv.Atoi(c.Query("user_id"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	filter := model.TopUpFilter{
		EpayChannelId:   common.String2Int(c.Query("epay_channel_id")),
		Keyword:         c.Query("keyword"),
		UserId:          userId,
		Username:        c.Query("username"),
		Status:          c.Query("status"),
		PaymentProvider: c.Query("payment_provider"),
		PaymentMethod:   c.Query("payment_method"),
		ReconcileStatus: c.Query("reconcile_status"),
		StartTimestamp:  startTimestamp,
		EndTimestamp:    endTimestamp,
	}

	var (
		topups []*model.TopUp
		total  int64
		err    error
	)
	topups, total, err = model.SearchAllTopUpsWithFilter(filter, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	common.ApiSuccess(c, pageInfo)
}

type AdminCompleteTopupRequest struct {
	TradeNo string `json:"trade_no"`
}

type adminRefundTopUpRequest struct {
	TradeNo string `json:"trade_no"`
	Reason  string `json:"reason"`
}

// AdminCompleteTopUp 管理员补单接口
func AdminCompleteTopUp(c *gin.Context) {
	var req AdminCompleteTopupRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TradeNo == "" {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	// 订单级互斥，防止并发补单
	LockOrder(req.TradeNo)
	defer UnlockOrder(req.TradeNo)

	if err := model.ManualCompleteTopUp(req.TradeNo); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// AdminRefundTopUp records an externally completed wallet or subscription refund.
func AdminRefundTopUp(c *gin.Context) {
	var req adminRefundTopUpRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	req.TradeNo = strings.TrimSpace(req.TradeNo)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.TradeNo == "" || req.Reason == "" {
		common.ApiErrorMsg(c, "订单号和退款原因不能为空")
		return
	}

	LockOrder(req.TradeNo)
	defer UnlockOrder(req.TradeNo)

	result, err := model.MarkTopUpRefunded(req.TradeNo, c.GetInt("id"), req.Reason)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !result.AlreadyRefunded {
		if result.OrderType == model.TopUpOrderTypeSubscription {
			if result.RedemptionId > 0 {
				model.RecordLog(result.UserId, model.LogTypeManage, fmt.Sprintf("管理员将订阅兑换码订单 %s 标记为退款并终止订阅权益 #%d，兑换码 #%d 保持已使用，用户分组保持不变", req.TradeNo, result.SubscriptionId, result.RedemptionId))
			} else {
				model.RecordLog(result.UserId, model.LogTypeManage, fmt.Sprintf("管理员将订阅订单 %s 标记为作废并终止订阅权益 #%d，用户分组保持不变", req.TradeNo, result.SubscriptionId))
			}
		} else {
			model.RecordLog(result.UserId, model.LogTypeManage, fmt.Sprintf("管理员将充值订单 %s 标记为退款，退款后累计充值 %.2f", req.TradeNo, result.TotalRecharge))
			if result.LevelChanged {
				model.RecordLog(result.UserId, model.LogTypeManage, fmt.Sprintf("退款后重新计算等级：#%d -> %s (#%d)", result.PreviousLevelId, result.LevelName, result.UserLevelId))
			}
		}
	}
	common.ApiSuccess(c, gin.H{
		"already_refunded": result.AlreadyRefunded,
		"order_type":       result.OrderType,
		"subscription_id":  result.SubscriptionId,
		"redemption_id":    result.RedemptionId,
		"total_recharge":   result.TotalRecharge,
		"user_level_id":    result.UserLevelId,
	})
}
