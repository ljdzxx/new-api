package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUp struct {
	Id                 int     `json:"id"`
	UserId             int     `json:"user_id" gorm:"index"`
	Username           string  `json:"username" gorm:"-"`
	Amount             int64   `json:"amount"`
	Money              float64 `json:"money"`
	TradeNo            string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod      string  `json:"payment_method" gorm:"type:varchar(50)"`
	PaymentProvider    string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	ReconcileStatus    string  `json:"reconcile_status" gorm:"type:varchar(32);default:'unchecked';index"`
	ReconcileTime      int64   `json:"reconcile_time"`
	ReconcileMessage   string  `json:"reconcile_message" gorm:"type:text"`
	CreateTime         int64   `json:"create_time"`
	CompleteTime       int64   `json:"complete_time"`
	Status             string  `json:"status"`
	RefundTime         int64   `json:"refund_time" gorm:"column:refund_time"`
	RefundOperatorId   int     `json:"refund_operator_id" gorm:"column:refund_operator_id;index"`
	RefundReason       string  `json:"refund_reason" gorm:"type:text;column:refund_reason"`
	OrderType          string  `json:"order_type" gorm:"-"`
	Refundable         bool    `json:"refundable" gorm:"-"`
	SubscriptionStatus string  `json:"subscription_status,omitempty" gorm:"-"`
	SubscriptionSource string  `json:"subscription_source,omitempty" gorm:"-"`
}

const (
	TopUpOrderTypeWallet       = "topup"
	TopUpOrderTypeSubscription = "subscription"
)

const (
	PaymentProviderEpay      = "epay"
	PaymentProviderStripe    = "stripe"
	PaymentProviderCreem     = "creem"
	PaymentProviderMall      = "mall"
	PaymentProviderBalance   = "balance"
	PaymentProviderPromotion = "promotion"
)

const (
	PaymentMethodAffLegacy  = "aff"
	PaymentMethodAffInviter = "aff_inviter"
	PaymentMethodAffInvitee = "aff_invitee"
	PaymentMethodBalance    = "balance"
	PaymentMethodRedemption = "redemption"
)

const (
	PaymentReconcileStatusUnchecked = "unchecked"
	PaymentReconcileStatusNormal    = "normal"
	PaymentReconcileStatusAbnormal  = "abnormal"
)

var (
	ErrPaymentMethodMismatch = errors.New("payment method mismatch")
	ErrTopUpNotFound         = errors.New("topup not found")
	ErrTopUpStatusInvalid    = errors.New("topup status invalid")
)

type TopUpFilter struct {
	Keyword         string
	UserId          int
	Username        string
	Status          string
	PaymentProvider string
	PaymentMethod   string
	ReconcileStatus string
	StartTimestamp  int64
	EndTimestamp    int64
	MaxId           int
}

func (topUp *TopUp) Insert() error {
	var err error
	err = DB.Create(topUp).Error
	return err
}

func (topUp *TopUp) Update() error {
	var err error
	err = DB.Save(topUp).Error
	return err
}

func quotaToTopUpAmount(quota int) int64 {
	if quota <= 0 {
		return 0
	}
	if common.QuotaPerUnit <= 0 {
		return int64(quota)
	}
	dQuota := decimal.NewFromInt(int64(quota))
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	return dQuota.Div(dQuotaPerUnit).IntPart()
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if tradeNo == "" {
		return errors.New("未提供支付单号")
	}
	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		topUp.Status = targetStatus
		topUp.CompleteTime = common.GetTimestamp()
		return tx.Save(topUp).Error
	})
}

func GetUserTotalRechargeAmount(userId int) (float64, error) {
	var total float64
	// Subscription audit records use amount=0. Wallet top-ups, including
	// quota redemptions, keep a positive amount and contribute their paid money.
	err := DB.Model(&TopUp{}).
		Where("user_id = ? AND status = ? AND amount > ?", userId, common.TopUpStatusSuccess, 0).
		Select("COALESCE(SUM(money), 0)").
		Scan(&total).Error
	return total, err
}

func getUserTotalRechargeAmountTx(tx *gorm.DB, userId int) (float64, error) {
	var total float64
	err := tx.Model(&TopUp{}).
		Where("user_id = ? AND status = ? AND amount > ?", userId, common.TopUpStatusSuccess, 0).
		Select("COALESCE(SUM(money), 0)").
		Scan(&total).Error
	return total, err
}

func applyUserLevelByRechargeTx(tx *gorm.DB, userId int, totalRecharge float64) (bool, string, int, error) {
	return recalculateUserLevelByRechargeTx(tx, userId, totalRecharge, false)
}

func recalculateUserLevelByRechargeTx(tx *gorm.DB, userId int, totalRecharge float64, allowAutoDowngrade bool) (bool, string, int, error) {
	target, found := setting.GetHighestUserLevelByRecharge(totalRecharge)
	if !found {
		return false, "", 0, nil
	}

	user := User{}
	if err := lockForUpdate(tx).Where("id = ?", userId).First(&user).Error; err != nil {
		return false, "", 0, err
	}

	targetSource := UserLevelSourceAuto
	if manualLevel, hasManualLevel := setting.GetUserLevelPolicyByID(user.UserLevelManualId); hasManualLevel &&
		(manualLevel.Recharge > target.Recharge || (manualLevel.Recharge == target.Recharge && manualLevel.ID >= target.ID)) {
		target = manualLevel
		targetSource = UserLevelSourceManual
	}

	if user.UserLevelId == target.ID {
		if user.UserLevelSource != targetSource {
			if err := tx.Model(&User{}).Where("id = ?", userId).Update("user_level_source", targetSource).Error; err != nil {
				return false, "", 0, err
			}
		}
		return false, target.Level, target.ID, nil
	}
	current, hasCurrent := setting.GetUserLevelPolicyByID(user.UserLevelId)
	if !allowAutoDowngrade && (!hasCurrent || current.Recharge >= target.Recharge) {
		return false, current.Level, current.ID, nil
	}

	err := tx.Model(&User{}).Where("id = ?", userId).Updates(map[string]interface{}{
		"user_level_id":     target.ID,
		"user_level_source": targetSource,
	}).Error
	if err != nil {
		return false, "", 0, err
	}

	return true, target.Level, target.ID, nil
}

type TopUpRefundResult struct {
	UserId          int
	OrderType       string
	SubscriptionId  int
	RedemptionId    int
	PreviousLevelId int
	LevelChanged    bool
	LevelName       string
	UserLevelId     int
	TotalRecharge   float64
	AlreadyRefunded bool
}

func parseRedemptionTradeNo(tradeNo string) (redemptionId int, welfareUserId int, ok bool) {
	const prefix = "redeem-"
	if !strings.HasPrefix(tradeNo, prefix) {
		return 0, 0, false
	}
	remainder := strings.TrimPrefix(tradeNo, prefix)
	idPart := remainder
	if strings.Contains(remainder, "-u") {
		var userPart string
		idPart, userPart, ok = strings.Cut(remainder, "-u")
		if !ok || userPart == "" || strings.Contains(userPart, "-u") {
			return 0, 0, false
		}
		welfareUserId, _ = strconv.Atoi(userPart)
		if welfareUserId <= 0 {
			return 0, 0, false
		}
	}
	redemptionId, _ = strconv.Atoi(idPart)
	if redemptionId <= 0 {
		return 0, 0, false
	}
	return redemptionId, welfareUserId, true
}

func resolveRedemptionSubscriptionEntitlementTx(tx *gorm.DB, topUp *TopUp) (*UserSubscription, int, error) {
	if tx == nil || topUp == nil || topUp.UserId <= 0 || topUp.PaymentMethod != PaymentMethodRedemption {
		return nil, 0, errors.New("无效的订阅兑换码订单")
	}
	redemptionId, welfareUserId, ok := parseRedemptionTradeNo(topUp.TradeNo)
	if !ok || (welfareUserId > 0 && welfareUserId != topUp.UserId) {
		return nil, 0, errors.New("订阅兑换码订单号无效")
	}

	var redemption Redemption
	if err := lockForUpdate(tx.Unscoped()).Where("id = ?", redemptionId).First(&redemption).Error; err != nil {
		return nil, 0, err
	}
	if redemption.RewardType != common.RedemptionRewardTypeSubscription || redemption.PlanId <= 0 || redemption.PayMoney <= 0 || topUp.Money <= 0 {
		return nil, 0, errors.New("仅实付金额大于0的订阅兑换码订单可以标记退款")
	}

	subscriptionId := 0
	if welfareUserId > 0 {
		var usage RedemptionUsage
		if err := lockForUpdate(tx).Where("redemption_id = ? AND user_id = ?", redemptionId, topUp.UserId).First(&usage).Error; err != nil {
			return nil, 0, err
		}
		if usage.RewardType != common.RedemptionRewardTypeSubscription || usage.PlanId != redemption.PlanId {
			return nil, 0, errors.New("兑换码使用记录与订阅权益不匹配")
		}
		subscriptionId = usage.SubscriptionId
	} else if redemption.UsedUserId != topUp.UserId {
		return nil, 0, errors.New("兑换码使用用户与订单用户不匹配")
	}

	var sub UserSubscription
	query := lockForUpdate(tx).Where("user_id = ? AND plan_id = ? AND source = ?", topUp.UserId, redemption.PlanId, PaymentMethodRedemption)
	if subscriptionId > 0 {
		query = query.Where("id = ?", subscriptionId)
	} else {
		query = query.Where("redemption_id = ?", redemptionId)
	}
	err := query.Order("id desc").First(&sub).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && subscriptionId == 0 {
		redeemedAt := topUp.CompleteTime
		if redeemedAt <= 0 {
			redeemedAt = redemption.RedeemedTime
		}
		var candidates []UserSubscription
		err = lockForUpdate(tx).
			Where("user_id = ? AND plan_id = ? AND source = ? AND redemption_id = ? AND created_at >= ? AND created_at <= ?",
				topUp.UserId, redemption.PlanId, PaymentMethodRedemption, 0, redeemedAt-5, redeemedAt+5).
			Order("id asc").Find(&candidates).Error
		if err == nil {
			if len(candidates) != 1 {
				return nil, 0, errors.New("无法唯一确定兑换码对应的订阅权益")
			}
			sub = candidates[0]
		}
	}
	if err != nil {
		return nil, 0, err
	}
	if sub.RedemptionId != 0 && sub.RedemptionId != redemptionId {
		return nil, 0, errors.New("兑换码关联的订阅权益不匹配")
	}
	if sub.RedemptionId == 0 {
		if err := tx.Model(&UserSubscription{}).Where("id = ?", sub.Id).Update("redemption_id", redemptionId).Error; err != nil {
			return nil, 0, err
		}
		sub.RedemptionId = redemptionId
	}
	return &sub, redemptionId, nil
}

func MarkTopUpRefunded(tradeNo string, operatorId int, reason string) (*TopUpRefundResult, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	reason = strings.TrimSpace(reason)
	if tradeNo == "" {
		return nil, errors.New("未提供订单号")
	}
	if operatorId <= 0 {
		return nil, errors.New("无效的操作人")
	}
	if reason == "" {
		return nil, errors.New("请填写退款原因")
	}
	if len([]rune(reason)) > 500 {
		return nil, errors.New("退款原因不能超过500个字符")
	}

	result := &TopUpRefundResult{OrderType: TopUpOrderTypeWallet}
	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTopUpNotFound
			}
			return err
		}
		result.UserId = topUp.UserId

		var subscriptionOrder SubscriptionOrder
		subscriptionOrderErr := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(&subscriptionOrder).Error
		if subscriptionOrderErr != nil && !errors.Is(subscriptionOrderErr, gorm.ErrRecordNotFound) {
			return subscriptionOrderErr
		}
		if subscriptionOrderErr == nil {
			result.OrderType = TopUpOrderTypeSubscription
			result.SubscriptionId = subscriptionOrder.UserSubscriptionId
			if topUp.Status == common.TopUpStatusRefunded && subscriptionOrder.Status == SubscriptionOrderStatusInvalidated {
				result.AlreadyRefunded = true
				totalRecharge, totalErr := getUserTotalRechargeAmountTx(tx, topUp.UserId)
				if totalErr != nil {
					return totalErr
				}
				result.TotalRecharge = totalRecharge
				var user User
				if userErr := tx.Select("user_level_id").Where("id = ?", topUp.UserId).First(&user).Error; userErr != nil {
					return userErr
				}
				result.PreviousLevelId = user.UserLevelId
				result.UserLevelId = user.UserLevelId
				return nil
			}
			if topUp.Status != common.TopUpStatusSuccess && topUp.Status != common.TopUpStatusRefunded {
				return errors.New("仅支付成功的订阅订单可以标记退款")
			}
			if subscriptionOrder.Status != common.TopUpStatusSuccess && subscriptionOrder.Status != SubscriptionOrderStatusInvalidated {
				return errors.New("仅支付成功的订阅订单可以标记退款")
			}
			if topUp.Money <= 0 || subscriptionOrder.Money <= 0 {
				return errors.New("仅实付金额大于0的订阅订单可以标记退款")
			}

			sub, err := resolveSubscriptionOrderEntitlementTx(tx, &subscriptionOrder)
			if err != nil {
				return err
			}
			result.SubscriptionId = sub.Id
			now := common.GetTimestamp()
			if err := cancelUserSubscriptionTx(tx, sub, now); err != nil {
				return err
			}
			if err := tx.Model(&SubscriptionOrder{}).Where("id = ?", subscriptionOrder.Id).Updates(map[string]interface{}{
				"status":             SubscriptionOrderStatusInvalidated,
				"refund_time":        now,
				"refund_operator_id": operatorId,
				"refund_reason":      reason,
			}).Error; err != nil {
				return err
			}
			topUp.Status = common.TopUpStatusRefunded
			topUp.RefundTime = now
			topUp.RefundOperatorId = operatorId
			topUp.RefundReason = reason
			if err := tx.Save(topUp).Error; err != nil {
				return err
			}
			result.TotalRecharge, err = getUserTotalRechargeAmountTx(tx, topUp.UserId)
			if err != nil {
				return err
			}
			var user User
			if err := tx.Select("user_level_id").Where("id = ?", topUp.UserId).First(&user).Error; err != nil {
				return err
			}
			result.PreviousLevelId = user.UserLevelId
			result.UserLevelId = user.UserLevelId
			return nil
		}

		if topUp.PaymentMethod == PaymentMethodRedemption && topUp.Amount == 0 {
			if topUp.Status != common.TopUpStatusSuccess && topUp.Status != common.TopUpStatusRefunded {
				return errors.New("仅支付成功的订阅兑换码订单可以标记退款")
			}
			sub, redemptionId, resolveErr := resolveRedemptionSubscriptionEntitlementTx(tx, topUp)
			if resolveErr != nil {
				return resolveErr
			}
			result.OrderType = TopUpOrderTypeSubscription
			result.SubscriptionId = sub.Id
			result.RedemptionId = redemptionId
			if topUp.Status == common.TopUpStatusRefunded {
				result.AlreadyRefunded = true
			} else {
				now := common.GetTimestamp()
				if err := cancelUserSubscriptionTx(tx, sub, now); err != nil {
					return err
				}
				topUp.Status = common.TopUpStatusRefunded
				topUp.RefundTime = now
				topUp.RefundOperatorId = operatorId
				topUp.RefundReason = reason
				if err := tx.Save(topUp).Error; err != nil {
					return err
				}
			}
			var err error
			result.TotalRecharge, err = getUserTotalRechargeAmountTx(tx, topUp.UserId)
			if err != nil {
				return err
			}
			var user User
			if err := tx.Select("user_level_id").Where("id = ?", topUp.UserId).First(&user).Error; err != nil {
				return err
			}
			result.PreviousLevelId = user.UserLevelId
			result.UserLevelId = user.UserLevelId
			return nil
		}

		if topUp.Status == common.TopUpStatusRefunded {
			result.AlreadyRefunded = true
			totalRecharge, totalErr := getUserTotalRechargeAmountTx(tx, topUp.UserId)
			if totalErr != nil {
				return totalErr
			}
			result.TotalRecharge = totalRecharge
			var user User
			if userErr := tx.Select("user_level_id").Where("id = ?", topUp.UserId).First(&user).Error; userErr != nil {
				return userErr
			}
			result.PreviousLevelId = user.UserLevelId
			result.UserLevelId = user.UserLevelId
			return nil
		}
		if topUp.Status != common.TopUpStatusSuccess {
			return errors.New("仅支付成功的订单可以标记退款")
		}
		if topUp.Amount <= 0 || topUp.Money <= 0 {
			return errors.New("仅计入等级累计充值的余额订单可以标记退款")
		}

		topUp.Status = common.TopUpStatusRefunded
		topUp.RefundTime = common.GetTimestamp()
		topUp.RefundOperatorId = operatorId
		topUp.RefundReason = reason
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		totalRecharge, err := getUserTotalRechargeAmountTx(tx, topUp.UserId)
		if err != nil {
			return err
		}
		result.TotalRecharge = totalRecharge
		var user User
		if err = tx.Select("user_level_id").Where("id = ?", topUp.UserId).First(&user).Error; err != nil {
			return err
		}
		result.PreviousLevelId = user.UserLevelId
		result.LevelChanged, result.LevelName, result.UserLevelId, err = recalculateUserLevelByRechargeTx(tx, topUp.UserId, totalRecharge, true)
		if !result.LevelChanged {
			result.UserLevelId = result.PreviousLevelId
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if !result.AlreadyRefunded {
		_ = invalidateUserCache(result.UserId)
	}
	return result, nil
}

func fillTopUpOrderMetadata(topups []*TopUp) error {
	if len(topups) == 0 {
		return nil
	}
	tradeNos := make([]string, 0, len(topups))
	redemptionIds := make([]int, 0)
	redemptionIdByTradeNo := make(map[string]int)
	welfareUserIdByTradeNo := make(map[string]int)
	for _, topup := range topups {
		if topup == nil {
			continue
		}
		topup.OrderType = TopUpOrderTypeWallet
		topup.Refundable = topup.Status == common.TopUpStatusSuccess && topup.Amount > 0 && topup.Money > 0
		if topup.TradeNo != "" {
			tradeNos = append(tradeNos, topup.TradeNo)
		}
		if topup.PaymentMethod == PaymentMethodRedemption && topup.Amount == 0 && topup.Money > 0 {
			if redemptionId, welfareUserId, ok := parseRedemptionTradeNo(topup.TradeNo); ok {
				redemptionIds = append(redemptionIds, redemptionId)
				redemptionIdByTradeNo[topup.TradeNo] = redemptionId
				welfareUserIdByTradeNo[topup.TradeNo] = welfareUserId
			}
		}
	}
	if len(tradeNos) == 0 {
		return nil
	}
	var orders []SubscriptionOrder
	if err := DB.Select("trade_no", "status").Where("trade_no IN ?", tradeNos).Find(&orders).Error; err != nil {
		return err
	}
	statusByTradeNo := make(map[string]string, len(orders))
	for _, order := range orders {
		statusByTradeNo[order.TradeNo] = order.Status
	}
	redemptionById := make(map[int]Redemption, len(redemptionIds))
	if len(redemptionIds) > 0 {
		var redemptions []Redemption
		if err := DB.Unscoped().Select("id", "reward_type", "plan_id", "pay_money", "used_user_id").
			Where("id IN ?", redemptionIds).Find(&redemptions).Error; err != nil {
			return err
		}
		for _, redemption := range redemptions {
			redemptionById[redemption.Id] = redemption
		}
	}
	for _, topup := range topups {
		if topup == nil {
			continue
		}
		if status, ok := statusByTradeNo[topup.TradeNo]; ok {
			topup.OrderType = TopUpOrderTypeSubscription
			topup.SubscriptionStatus = status
			topup.Refundable = topup.Status == common.TopUpStatusSuccess && status == common.TopUpStatusSuccess && topup.Money > 0
			continue
		}
		redemptionId, ok := redemptionIdByTradeNo[topup.TradeNo]
		if !ok {
			continue
		}
		redemption, exists := redemptionById[redemptionId]
		welfareUserId := welfareUserIdByTradeNo[topup.TradeNo]
		userMatches := welfareUserId == topup.UserId || (welfareUserId == 0 && redemption.UsedUserId == topup.UserId)
		if exists && userMatches && redemption.RewardType == common.RedemptionRewardTypeSubscription && redemption.PlanId > 0 && redemption.PayMoney > 0 {
			topup.OrderType = TopUpOrderTypeSubscription
			topup.SubscriptionSource = PaymentMethodRedemption
			topup.Refundable = topup.Status == common.TopUpStatusSuccess
		}
	}
	return nil
}

func TryAutoUpgradeUserLevelByRecharge(userId int) error {
	if userId <= 0 {
		return nil
	}
	var levelChanged bool
	var upgradedGroup string
	var upgradedLevelID int
	err := DB.Transaction(func(tx *gorm.DB) error {
		totalRecharge, err := getUserTotalRechargeAmountTx(tx, userId)
		if err != nil {
			return err
		}
		levelChanged, upgradedGroup, upgradedLevelID, err = applyUserLevelByRechargeTx(tx, userId, totalRecharge)
		return err
	})
	if err != nil {
		return err
	}
	if levelChanged {
		_ = invalidateUserCache(userId)
		RecordLog(userId, LogTypeManage, fmt.Sprintf("累计充值达标，用户等级自动升级为 %s (#%d)", upgradedGroup, upgradedLevelID))
	}
	return nil
}

func Recharge(referenceId string, customerId string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var quota float64
	var levelChanged bool
	var upgradedGroup string
	var upgradedLevelID int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderStripe {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		quota = topUp.Money * common.QuotaPerUnit
		err = tx.Model(&User{}).Where("id = ?", topUp.UserId).Updates(map[string]interface{}{"stripe_customer": customerId, "quota": gorm.Expr("quota + ?", quota)}).Error
		if err != nil {
			return err
		}

		totalRecharge, err := getUserTotalRechargeAmountTx(tx, topUp.UserId)
		if err != nil {
			return err
		}
		levelChanged, upgradedGroup, upgradedLevelID, err = applyUserLevelByRechargeTx(tx, topUp.UserId, totalRecharge)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("使用在线充值成功，充值金额: %v，支付金额：%d", logger.FormatQuota(int(quota)), topUp.Amount))
	if levelChanged {
		_ = invalidateUserCache(topUp.UserId)
		RecordLog(topUp.UserId, LogTypeManage, fmt.Sprintf("累计充值达标，用户等级自动升级为 %s (#%d)", upgradedGroup, upgradedLevelID))
	}

	return nil
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ?", userId).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ?", userId).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	if err = fillTopUpOrderMetadata(topups); err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	return SearchAllTopUpsWithFilter(TopUpFilter{}, pageInfo)
}

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ?", userId)
	if keyword != "" {
		like := "%%" + keyword + "%%"
		query = query.Where("trade_no LIKE ?", like)
	}

	if err = query.Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	if err = fillTopUpOrderMetadata(topups); err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	return SearchAllTopUpsWithFilter(TopUpFilter{Keyword: keyword}, pageInfo)
}

func applyTopUpFilter(query *gorm.DB, filter TopUpFilter) *gorm.DB {
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%%" + keyword + "%%"
		query = query.Where("trade_no LIKE ?", like)
	}
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if username := strings.TrimSpace(filter.Username); username != "" {
		like := "%%" + username + "%%"
		userQuery := DB.Model(&User{}).Select("id").Where("username LIKE ?", like)
		query = query.Where("user_id IN (?)", userQuery)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if paymentProvider := strings.TrimSpace(filter.PaymentProvider); paymentProvider != "" {
		query = query.Where("payment_provider = ?", paymentProvider)
	}
	if paymentMethod := strings.TrimSpace(filter.PaymentMethod); paymentMethod != "" {
		query = query.Where("payment_method = ?", paymentMethod)
	}
	if reconcileStatus := strings.TrimSpace(filter.ReconcileStatus); reconcileStatus != "" {
		query = query.Where("reconcile_status = ?", reconcileStatus)
	}
	if filter.StartTimestamp > 0 {
		query = query.Where("create_time >= ?", filter.StartTimestamp)
	}
	if filter.EndTimestamp > 0 {
		query = query.Where("create_time <= ?", filter.EndTimestamp)
	}
	if filter.MaxId > 0 {
		query = query.Where("id <= ?", filter.MaxId)
	}
	return query
}

func fillTopUpUsernames(topups []*TopUp) {
	if len(topups) == 0 {
		return
	}
	userIdSet := make(map[int]struct{})
	for _, topup := range topups {
		if topup != nil && topup.UserId > 0 {
			userIdSet[topup.UserId] = struct{}{}
		}
	}
	if len(userIdSet) == 0 {
		return
	}
	userIds := make([]int, 0, len(userIdSet))
	for userId := range userIdSet {
		userIds = append(userIds, userId)
	}
	var users []User
	if err := DB.Select("id", "username").Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return
	}
	usernameById := make(map[int]string, len(users))
	for _, user := range users {
		usernameById[user.Id] = user.Username
	}
	for _, topup := range topups {
		if topup != nil {
			topup.Username = usernameById[topup.UserId]
		}
	}
}

// SearchAllTopUpsWithFilter 按订单号、用户和时间范围搜索全平台充值记录（管理员使用）
func SearchAllTopUpsWithFilter(filter TopUpFilter, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := applyTopUpFilter(tx.Model(&TopUp{}), filter)

	if err = query.Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	fillTopUpUsernames(topups)
	if err = fillTopUpOrderMetadata(topups); err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

func CountTopUpsWithFilter(filter TopUpFilter) (int64, error) {
	var total int64
	err := applyTopUpFilter(DB.Model(&TopUp{}), filter).Count(&total).Error
	return total, err
}

func MaxTopUpIdWithFilter(filter TopUpFilter) (int, error) {
	var maxId int
	err := applyTopUpFilter(DB.Model(&TopUp{}), filter).Select("COALESCE(MAX(id), 0)").Scan(&maxId).Error
	return maxId, err
}

func FindTopUpsWithFilter(filter TopUpFilter, lastId int, limit int) ([]*TopUp, error) {
	if limit <= 0 {
		limit = 100
	}
	query := applyTopUpFilter(DB.Model(&TopUp{}), filter)
	if lastId > 0 {
		query = query.Where("id > ?", lastId)
	}
	var topups []*TopUp
	err := query.Order("id asc").Limit(limit).Find(&topups).Error
	return topups, err
}

func UpdateTopUpReconcileResult(topUpId int, status string, message string) error {
	return DB.Model(&TopUp{}).Where("id = ?", topUpId).Updates(map[string]interface{}{
		"reconcile_status":  status,
		"reconcile_message": message,
		"reconcile_time":    common.GetTimestamp(),
	}).Error
}

// ManualCompleteTopUp 管理员手动完成订单并给用户充值
func ManualCompleteTopUp(tradeNo string) error {
	if tradeNo == "" {
		return errors.New("未提供订单号")
	}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	var userId int
	var levelChanged bool
	var upgradedGroup string
	var upgradedLevelID int
	var quotaToAdd int
	var payMoney float64

	err := DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		// 行级锁，避免并发补单
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return errors.New("充值订单不存在")
		}

		// 幂等处理：已成功直接返回
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("订单状态不是待支付，无法补单")
		}

		// 计算应充值额度：
		// - Stripe 订单：Money 代表经分组倍率换算后的美元数量，直接 * QuotaPerUnit
		// - 其他订单（如易支付）：Amount 为美元数量，* QuotaPerUnit
		if topUp.PaymentProvider == PaymentProviderStripe {
			dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
			quotaToAdd = int(decimal.NewFromFloat(topUp.Money).Mul(dQuotaPerUnit).IntPart())
		} else {
			dAmount := decimal.NewFromInt(topUp.Amount)
			dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
			quotaToAdd = int(dAmount.Mul(dQuotaPerUnit).IntPart())
		}
		if quotaToAdd <= 0 {
			return errors.New("无效的充值额度")
		}

		// 标记完成
		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		if err := tx.Save(topUp).Error; err != nil {
			return err
		}

		// 增加用户额度（立即写库，保持一致性）
		if err := tx.Model(&User{}).Where("id = ?", topUp.UserId).Update("quota", gorm.Expr("quota + ?", quotaToAdd)).Error; err != nil {
			return err
		}
		totalRecharge, err := getUserTotalRechargeAmountTx(tx, topUp.UserId)
		if err != nil {
			return err
		}
		levelChanged, upgradedGroup, upgradedLevelID, err = applyUserLevelByRechargeTx(tx, topUp.UserId, totalRecharge)
		if err != nil {
			return err
		}

		userId = topUp.UserId
		payMoney = topUp.Money
		return nil
	})

	if err != nil {
		return err
	}

	// 事务外记录日志，避免阻塞
	RecordLog(userId, LogTypeTopup, fmt.Sprintf("管理员补单成功，充值金额: %v，支付金额：%f", logger.FormatQuota(quotaToAdd), payMoney))
	if levelChanged {
		_ = invalidateUserCache(userId)
		RecordLog(userId, LogTypeManage, fmt.Sprintf("累计充值达标，用户等级自动升级为 %s (#%d)", upgradedGroup, upgradedLevelID))
	}
	return nil
}
func RechargeCreem(referenceId string, customerEmail string, customerName string) (err error) {
	if referenceId == "" {
		return errors.New("未提供支付单号")
	}

	var quota int64
	var levelChanged bool
	var upgradedGroup string
	var upgradedLevelID int
	topUp := &TopUp{}

	refCol := "`trade_no`"
	if common.UsingPostgreSQL {
		refCol = `"trade_no"`
	}

	err = DB.Transaction(func(tx *gorm.DB) error {
		err := lockForUpdate(tx).Where(refCol+" = ?", referenceId).First(topUp).Error
		if err != nil {
			return errors.New("充值订单不存在")
		}

		if topUp.PaymentProvider != PaymentProviderCreem {
			return ErrPaymentMethodMismatch
		}

		if topUp.Status != common.TopUpStatusPending {
			return errors.New("充值订单状态错误")
		}

		topUp.CompleteTime = common.GetTimestamp()
		topUp.Status = common.TopUpStatusSuccess
		err = tx.Save(topUp).Error
		if err != nil {
			return err
		}

		// Creem 直接使用 Amount 作为充值额度（整数）
		quota = topUp.Amount

		// 构建更新字段，优先使用邮箱，如果邮箱为空则使用用户名
		updateFields := map[string]interface{}{
			"quota": gorm.Expr("quota + ?", quota),
		}

		// 如果有客户邮箱，尝试更新用户邮箱（仅当用户邮箱为空时）
		if customerEmail != "" {
			// 先检查用户当前邮箱是否为空
			var user User
			err = tx.Where("id = ?", topUp.UserId).First(&user).Error
			if err != nil {
				return err
			}

			// 如果用户邮箱为空，则更新为支付时使用的邮箱
			if user.Email == "" {
				updateFields["email"] = customerEmail
			}
		}

		err = tx.Model(&User{}).Where("id = ?", topUp.UserId).Updates(updateFields).Error
		if err != nil {
			return err
		}
		totalRecharge, err := getUserTotalRechargeAmountTx(tx, topUp.UserId)
		if err != nil {
			return err
		}
		levelChanged, upgradedGroup, upgradedLevelID, err = applyUserLevelByRechargeTx(tx, topUp.UserId, totalRecharge)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		common.SysError("creem topup failed: " + err.Error())
		return errors.New("充值失败，请稍后重试")
	}

	RecordLog(topUp.UserId, LogTypeTopup, fmt.Sprintf("使用Creem充值成功，充值额度: %v，支付金额：%.2f", quota, topUp.Money))
	if levelChanged {
		_ = invalidateUserCache(topUp.UserId)
		RecordLog(topUp.UserId, LogTypeManage, fmt.Sprintf("累计充值达标，用户等级自动升级为 %s (#%d)", upgradedGroup, upgradedLevelID))
	}

	return nil
}
