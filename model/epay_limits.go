package model

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func EpayLocalDayBounds(now time.Time) (int64, int64) {
	local := now.In(time.Local)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	return start.Unix(), start.AddDate(0, 0, 1).Unix()
}

func EpayAmountCents(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 32 || strings.ContainsAny(value, "eE") {
		return 0, errors.New("易支付金额格式无效")
	}
	amount, err := decimal.NewFromString(value)
	if err != nil || amount.IsNegative() || amount.Exponent() < -2 || amount.GreaterThan(decimal.NewFromInt(100000000)) {
		return 0, errors.New("易支付金额超出有效范围")
	}
	return amount.Mul(decimal.NewFromInt(100)).IntPart(), nil
}

func EpayRevisionMethodLimits(revision *EpayChannelRevision) (map[string]types.EpayMethodLimits, error) {
	if revision == nil {
		return nil, errors.New("易支付渠道版本不存在")
	}
	limits := make(map[string]types.EpayMethodLimits)
	if revision.PaymentMethodLimits != "" {
		if err := common.UnmarshalJsonStr(revision.PaymentMethodLimits, &limits); err != nil {
			return nil, err
		}
		return limits, nil
	}
	var methods []string
	if err := common.UnmarshalJsonStr(revision.PaymentMethods, &methods); err != nil {
		return nil, err
	}
	for _, method := range methods {
		limits[method] = types.EpayMethodLimits{MinPayCNY: revision.MinPayCNY, MaxPayCNY: revision.MaxPayCNY, DailyLimitCNY: "0"}
	}
	return limits, nil
}

func ValidateEpayMethodLimits(limits types.EpayMethodLimits) (int64, int64, int64, error) {
	minimum, err := EpayAmountCents(limits.MinPayCNY)
	if err != nil || minimum <= 0 {
		return 0, 0, 0, errors.New("单笔最低必须大于 0，最多两位小数")
	}
	maximum, err := EpayAmountCents(limits.MaxPayCNY)
	if err != nil || maximum < minimum {
		return 0, 0, 0, errors.New("单笔最高必须不小于单笔最低，最多两位小数")
	}
	daily, err := EpayAmountCents(limits.DailyLimitCNY)
	if err != nil {
		return 0, 0, 0, errors.New("每日限额必须不小于 0，最多两位小数，0 表示不限")
	}
	return minimum, maximum, daily, nil
}

func GetEpayDailyUsed(channelID int, method string, now time.Time) (int64, int64, error) {
	start, end := EpayLocalDayBounds(now)
	used, err := getEpayDailyUsedTx(DB, channelID, method, start, end)
	return used, end, err
}

func getEpayDailyUsedTx(transaction *gorm.DB, channelID int, method string, start, end int64) (int64, error) {
	var total int64
	for _, orderModel := range []interface{}{&TopUp{}, &SubscriptionOrder{}} {
		query := transaction.Model(orderModel).
			Where("epay_channel_id = ? AND payment_method = ? AND payment_provider = ? AND create_time >= ? AND create_time < ?", channelID, method, PaymentProviderEpay, start, end).
			Where("status IN ?", []string{common.TopUpStatusPending, common.TopUpStatusSuccess, common.TopUpStatusRefunded, SubscriptionOrderStatusInvalidated})
		if _, wallet := orderModel.(*TopUp); wallet {
			query = query.Where("amount > ?", 0)
		}
		var sum struct{ Total int64 }
		if err := query.Session(&gorm.Session{}).Select("COALESCE(SUM(epay_pay_cents), 0) AS total").Scan(&sum).Error; err != nil {
			return 0, err
		}
		if sum.Total < 0 || sum.Total > math.MaxInt64-total {
			return 0, errors.New("易支付日限统计超出安全范围")
		}
		total += sum.Total
		var legacy []struct {
			EpayPayCNY string
			Money      float64
		}
		if err := query.Session(&gorm.Session{}).Where("epay_pay_cents IS NULL OR epay_pay_cents = ?", 0).
			Select("epay_pay_cny, money").Find(&legacy).Error; err != nil {
			return 0, err
		}
		for _, order := range legacy {
			amount := order.EpayPayCNY
			if amount == "" && order.Money > 0 && !math.IsNaN(order.Money) && !math.IsInf(order.Money, 0) {
				amount = decimal.NewFromFloat(order.Money).Round(2).StringFixed(2)
			}
			cents, err := EpayAmountCents(amount)
			if err != nil || cents <= 0 || cents > math.MaxInt64-total {
				return 0, errors.New("易支付历史订单金额无效，无法计算日限")
			}
			total += cents
		}
	}
	return total, nil
}

func createEpayLimitedOrder(userID int, tradeNo, formJSON string, channelID, revisionID int, method, payCNY string, insert func(*gorm.DB, int64, int64) error) error {
	return DB.Transaction(func(transaction *gorm.DB) error {
		if err := reserveEpayCheckout(transaction, userID, tradeNo, formJSON); err != nil {
			return err
		}
		var channel EpayChannel
		if err := lockForUpdate(transaction).First(&channel, channelID).Error; err != nil {
			return err
		}
		if !channel.Enabled || channel.CurrentRevisionId != revisionID {
			return errors.New("渠道已停用或配置已更新，请重新获取报价")
		}
		now := time.Now()
		if err := checkEpayPendingLimit(transaction, userID, now.Unix()); err != nil {
			return err
		}
		var revision EpayChannelRevision
		if err := transaction.First(&revision, revisionID).Error; err != nil {
			return err
		}
		var methods []string
		if err := common.UnmarshalJsonStr(revision.PaymentMethods, &methods); err != nil {
			return err
		}
		allowed := false
		for _, available := range methods {
			allowed = allowed || available == method
		}
		if !allowed || revision.ChannelId != channelID {
			return errors.New("该易支付渠道不支持所选支付方式")
		}
		configured, err := EpayRevisionMethodLimits(&revision)
		if err != nil {
			return err
		}
		minimum, maximum, daily, err := ValidateEpayMethodLimits(configured[method])
		if err != nil {
			return err
		}
		cents, err := EpayAmountCents(payCNY)
		if err != nil || cents < minimum || cents > maximum {
			return errors.New("实付金额不符合所选支付方式的单笔限额")
		}
		if daily > 0 {
			start, end := EpayLocalDayBounds(now)
			used, err := getEpayDailyUsedTx(transaction, channelID, method, start, end)
			if err != nil {
				return err
			}
			if cents > daily || used > daily-cents {
				return fmt.Errorf("所选支付方式今日剩余额度不足，每日限额 %s CNY", configured[method].DailyLimitCNY)
			}
		}
		return insert(transaction, cents, now.Unix())
	})
}

func CreateEpayTopupOrder(topup *TopUp) error {
	if topup == nil || topup.PaymentProvider != PaymentProviderEpay || topup.Status != common.TopUpStatusPending || topup.Amount <= 0 {
		return errors.New("易支付充值订单参数无效")
	}
	return createEpayLimitedOrder(topup.UserId, topup.TradeNo, topup.EpayCheckoutFormJSON, topup.EpayChannelId, topup.EpayChannelRevisionId, topup.PaymentMethod, topup.EpayPayCNY,
		func(transaction *gorm.DB, cents, now int64) error {
			topup.EpayPayCents, topup.CreateTime = cents, now
			return transaction.Create(topup).Error
		})
}

func CreateEpaySubscriptionOrder(order *SubscriptionOrder) error {
	if order == nil || order.PaymentProvider != PaymentProviderEpay || order.Status != common.TopUpStatusPending {
		return errors.New("易支付订阅订单参数无效")
	}
	return createEpayLimitedOrder(order.UserId, order.TradeNo, order.EpayCheckoutFormJSON, order.EpayChannelId, order.EpayChannelRevisionId, order.PaymentMethod, order.EpayPayCNY,
		func(transaction *gorm.DB, cents, now int64) error {
			order.EpayPayCents, order.CreateTime = cents, now
			return transaction.Create(order).Error
		})
}
