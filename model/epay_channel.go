package model

import (
	"errors"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type EpayChannel struct {
	Id                int   `json:"id"`
	Enabled           bool  `json:"enabled" gorm:"index"`
	SortOrder         int   `json:"sort_order"`
	CurrentRevisionId int   `json:"current_revision_id"`
	CreatedAt         int64 `json:"created_at"`
	UpdatedAt         int64 `json:"updated_at"`
}

type EpayChannelConfig struct {
	Name                          string `json:"name" gorm:"type:varchar(128)"`
	Description                   string `json:"description" gorm:"type:text"`
	PayAddress                    string `json:"pay_address" gorm:"type:varchar(2048)"`
	QueryAPI                      string `json:"query_api" gorm:"type:varchar(32);default:'api_pay_query'"`
	MerchantId                    string `json:"merchant_id" gorm:"type:varchar(255)"`
	CallbackAddress               string `json:"callback_address" gorm:"type:varchar(2048)"`
	PaymentMethods                string `json:"-" gorm:"type:text"`
	PaymentMethodLimits           string `json:"-" gorm:"type:text"`
	MinPayCNY                     string `json:"-" gorm:"type:varchar(64)"`
	MaxPayCNY                     string `json:"-" gorm:"type:varchar(64)"`
	TopupOfferType                string `json:"topup_offer_type" gorm:"type:varchar(16)"`
	TopupOfferPercent             string `json:"topup_offer_percent" gorm:"type:varchar(64)"`
	TopupOfferThresholdCNY        string `json:"topup_offer_threshold_cny" gorm:"type:varchar(64)"`
	SubscriptionDiscountPercent   string `json:"subscription_discount_percent" gorm:"type:varchar(64)"`
	SubscriptionOfferThresholdCNY string `json:"subscription_offer_threshold_cny" gorm:"type:varchar(64)"`
}

type EpayChannelRevision struct {
	Id        int `json:"id"`
	ChannelId int `json:"channel_id" gorm:"index"`
	EpayChannelConfig
	MerchantKey string `json:"-" gorm:"type:text"`
	CreatedAt   int64  `json:"created_at"`
}

func ListEpayChannels(enabledOnly bool) ([]*EpayChannel, error) {
	channels := make([]*EpayChannel, 0)
	query := DB.Order("sort_order asc, id asc")
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	err := query.Find(&channels).Error
	return channels, err
}

func GetEpayChannel(id int) (*EpayChannel, error) {
	if id <= 0 {
		return nil, errors.New("请选择易支付渠道")
	}
	var channel EpayChannel
	err := DB.First(&channel, id).Error
	return &channel, err
}

func GetEpayChannelRevision(id int) (*EpayChannelRevision, error) {
	if id <= 0 {
		return nil, errors.New("旧易支付订单不支持新支付体系")
	}
	var revision EpayChannelRevision
	err := DB.First(&revision, id).Error
	return &revision, err
}

func SaveEpayChannel(channel *EpayChannel, revision *EpayChannelRevision, expectedRevision int) error {
	return DB.Transaction(func(transaction *gorm.DB) error {
		now := common.GetTimestamp()
		if channel.Id == 0 {
			var last EpayChannel
			if err := transaction.Order("sort_order desc, id desc").Limit(1).Find(&last).Error; err != nil {
				return err
			}
			channel.SortOrder = last.SortOrder + 1
			channel.CreatedAt = now
			if err := transaction.Create(channel).Error; err != nil {
				return err
			}
		} else {
			var current EpayChannel
			if err := lockForUpdate(transaction).First(&current, channel.Id).Error; err != nil {
				return err
			}
			if current.CurrentRevisionId != expectedRevision {
				return errors.New("渠道配置已更新，请刷新后重试")
			}
			channel.CreatedAt = current.CreatedAt
			channel.SortOrder = current.SortOrder
		}
		revision.ChannelId = channel.Id
		revision.CreatedAt = now
		if err := transaction.Create(revision).Error; err != nil {
			return err
		}
		channel.CurrentRevisionId = revision.Id
		channel.UpdatedAt = now
		return transaction.Save(channel).Error
	})
}

func ReorderEpayChannels(channelIDs, expectedIDs []int) error {
	if len(channelIDs) == 0 || len(channelIDs) != len(expectedIDs) {
		return errors.New("渠道排序参数无效")
	}
	return DB.Transaction(func(transaction *gorm.DB) error {
		var channels []EpayChannel
		if err := lockForUpdate(transaction).Order("sort_order asc, id asc").Find(&channels).Error; err != nil {
			return err
		}
		if len(channels) != len(expectedIDs) {
			return errors.New("渠道列表已更新，请刷新后重试")
		}
		sort.Slice(channels, func(first, second int) bool {
			if channels[first].SortOrder == channels[second].SortOrder {
				return channels[first].Id < channels[second].Id
			}
			return channels[first].SortOrder < channels[second].SortOrder
		})
		remaining := make(map[int]bool, len(channels))
		for index, channel := range channels {
			if channel.Id != expectedIDs[index] {
				return errors.New("渠道排序已更新，请刷新后重试")
			}
			remaining[channel.Id] = true
		}
		for _, id := range channelIDs {
			if !remaining[id] {
				return errors.New("渠道排序包含重复或无效的渠道")
			}
			delete(remaining, id)
		}
		for index, id := range channelIDs {
			if err := transaction.Model(&EpayChannel{}).Where("id = ?", id).
				Updates(map[string]interface{}{"sort_order": index + 1, "updated_at": common.GetTimestamp()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func SetEpayChannelEnabled(id int, enabled bool, expectedRevision int) error {
	if id <= 0 || expectedRevision <= 0 {
		return errors.New("渠道参数无效")
	}
	return DB.Transaction(func(transaction *gorm.DB) error {
		var channel EpayChannel
		if err := lockForUpdate(transaction).First(&channel, id).Error; err != nil {
			return err
		}
		if channel.CurrentRevisionId != expectedRevision {
			return errors.New("渠道配置已更新，请刷新后重试")
		}
		return transaction.Model(&channel).
			Updates(map[string]interface{}{"enabled": enabled, "updated_at": common.GetTimestamp()}).Error
	})
}

// addEpayDifference applies the verified PHP gateway surcharge at the original
// exchange rate, independently of any discount or bonus on the quoted amount.
func (topup *TopUp) addEpayDifference(difference decimal.Decimal) error {
	var quote types.EpayQuote
	if err := common.UnmarshalJsonStr(topup.EpayQuoteSnapshot, &quote); err != nil {
		return errors.New("易支付报价快照无效")
	}
	rate, err := decimal.NewFromString(quote.USDExchangeRate)
	if err != nil || !rate.IsPositive() {
		return errors.New("易支付报价汇率无效")
	}
	var units decimal.Decimal
	if quote.QuotaPerUnit == "" {
		// Older quotes saved the base quota and its USD equivalent.
		baseUSD, err := decimal.NewFromString(quote.BaseUSD)
		if err != nil || !baseUSD.IsPositive() {
			return errors.New("易支付报价基础金额无效")
		}
		units = decimal.NewFromInt(int64(topup.BaseQuota)).Div(baseUSD)
	} else {
		units, err = decimal.NewFromString(quote.QuotaPerUnit)
		if err != nil {
			return errors.New("易支付报价额度单位无效")
		}
	}
	if !units.IsPositive() {
		return errors.New("易支付报价额度单位无效")
	}
	additional, err := common.QuotaFromDecimalStrict(difference.Mul(units).Div(rate))
	if err != nil || additional < 0 || topup.CreditQuota >= common.MaxQuota-additional {
		return errors.New("含支付尾差的充值额度超出安全范围")
	}
	actual := decimal.NewFromFloat(topup.EpayQuotedMoney()).Add(difference)
	topup.Money = actual.InexactFloat64()
	// Conservatively account for any fraction of a cent in daily channel limits.
	topup.EpayPayCents = actual.Shift(2).Ceil().IntPart()
	topup.BaseQuota += additional
	topup.CreditQuota += additional
	topup.Amount = int64(topup.CreditQuota)
	topup.EpayCreditUSD = decimal.NewFromInt(topup.Amount).Div(units).String()
	return nil
}

func CompleteEpayTopup(tradeNo string) error {
	return CompleteEpayTopupWithDifference(tradeNo, decimal.Zero)
}

func CompleteEpayTopupWithDifference(tradeNo string, difference decimal.Decimal) error {
	var completed TopUp
	changed := false
	err := DB.Transaction(func(transaction *gorm.DB) error {
		var topup TopUp
		if err := lockForUpdate(transaction).Where("trade_no = ?", tradeNo).First(&topup).Error; err != nil {
			return err
		}
		if topup.PaymentProvider != PaymentProviderEpay || topup.EpayChannelRevisionId <= 0 {
			return ErrPaymentMethodMismatch
		}
		if topup.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topup.Status != common.TopUpStatusPending || epayOrderExpired(topup.CreateTime, common.GetTimestamp()) {
			return ErrTopUpStatusInvalid
		}
		if topup.CreditQuota <= 0 || topup.CreditQuota >= common.MaxQuota || topup.BaseQuota <= 0 || topup.BonusQuota < 0 || topup.BaseQuota != topup.CreditQuota-topup.BonusQuota {
			return errors.New("无效的易支付入账额度")
		}
		updates := map[string]interface{}{"status": common.TopUpStatusSuccess, "complete_time": common.GetTimestamp()}
		if !difference.IsZero() {
			if difference.IsNegative() || !difference.LessThan(decimal.NewFromInt(1)) || topup.EpayPayCNY == "" || topup.Money <= 0 || topup.EpayQuotedMoney() != topup.Money {
				return errors.New("易支付尾差无效")
			}
			var revision EpayChannelRevision
			if err := transaction.Where("id = ? AND channel_id = ? AND query_api = ?", topup.EpayChannelRevisionId, topup.EpayChannelId, types.EpayQueryAPIPHP).First(&revision).Error; err != nil {
				return errors.New("易支付尾差渠道版本无效")
			}
			if err := topup.addEpayDifference(difference); err != nil {
				return err
			}
			updates["money"], updates["epay_pay_cents"] = topup.Money, topup.EpayPayCents
			updates["amount"], updates["epay_credit_usd"] = topup.Amount, topup.EpayCreditUSD
			updates["base_quota"], updates["credit_quota"] = topup.BaseQuota, topup.CreditQuota
		}
		result := transaction.Model(&TopUp{}).Where("id = ? AND status = ?", topup.Id, common.TopUpStatusPending).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTopUpStatusInvalid
		}
		result = transaction.Model(&User{}).
			Where("id = ? AND quota <= ?", topup.UserId, common.MaxQuota-topup.CreditQuota).
			Update("quota", gorm.Expr("quota + ?", topup.CreditQuota))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("用户不存在或余额超过安全上限")
		}
		totalRecharge, err := getUserTotalRechargeAmountTx(transaction, topup.UserId)
		if err != nil {
			return err
		}
		if _, _, _, err = applyUserLevelByRechargeTx(transaction, topup.UserId, totalRecharge); err != nil {
			return err
		}
		completed = topup
		changed = true
		return nil
	})
	if err == nil && changed {
		_ = invalidateUserCache(completed.UserId)
		RecordLog(completed.UserId, LogTypeTopup, "易支付充值成功，渠道："+completed.EpayChannelName+"，实付 CNY："+strconv.FormatFloat(completed.Money, 'f', -1, 64)+"，到账 USD："+completed.EpayCreditUSD+"，赠金 USD："+completed.EpayBonusUSD)
	}
	return err
}
