package service

import (
	"crypto/sha256"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/shopspring/decimal"
)

const maxEpayAmountCNY = 100000000

type EpayChannelInput struct {
	model.EpayChannelConfig
	Enabled      bool                              `json:"enabled"`
	RevisionID   int                               `json:"revision_id"`
	MerchantKey  string                            `json:"merchant_key"`
	Methods      []string                          `json:"payment_methods"`
	MethodLimits map[string]types.EpayMethodLimits `json:"payment_method_limits"`
}

type EpayChannelAdminView struct {
	model.EpayChannelConfig
	ID             int                               `json:"id"`
	Enabled        bool                              `json:"enabled"`
	SortOrder      int                               `json:"sort_order"`
	RevisionID     int                               `json:"revision_id"`
	Methods        []string                          `json:"payment_methods"`
	HasMerchantKey bool                              `json:"has_merchant_key"`
	MethodLimits   map[string]types.EpayMethodLimits `json:"payment_method_limits"`
}

func epayDecimal(value string, maxPlaces int32) (decimal.Decimal, error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 32 || strings.ContainsAny(value, "eE") {
		return decimal.Zero, fmt.Errorf("金额或比例格式无效")
	}
	number, err := decimal.NewFromString(value)
	if err != nil || number.IsNegative() || number.Exponent() < -maxPlaces || number.GreaterThan(decimal.NewFromInt(maxEpayAmountCNY)) {
		return decimal.Zero, fmt.Errorf("金额或比例超出有效范围")
	}
	return number, nil
}

func EpayMoneyMatches(actual string, expected float64) bool {
	if expected <= 0 || math.IsNaN(expected) || math.IsInf(expected, 0) {
		return false
	}
	amount, err := epayDecimal(actual, 6)
	return err == nil && amount.IsPositive() && amount.Equal(decimal.NewFromFloat(expected).Round(2))
}

func validateEpayURL(value string, optional bool) error {
	if optional && value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(value) > 2048 {
		return fmt.Errorf("支付地址和回调地址必须是有效的 HTTP(S) 地址，不可包含认证信息、查询参数或片段")
	}
	return nil
}

func SaveEpayChannel(id int, input *EpayChannelInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.PayAddress = strings.TrimRight(strings.TrimSpace(input.PayAddress), "/")
	input.CallbackAddress = strings.TrimRight(strings.TrimSpace(input.CallbackAddress), "/")
	input.MerchantId = strings.TrimSpace(input.MerchantId)
	input.MerchantKey = strings.TrimSpace(input.MerchantKey)
	if input.Name == "" || len(input.Name) > 128 || len(input.Description) > 4000 || input.MerchantId == "" || len(input.MerchantId) > 255 {
		return fmt.Errorf("请填写有效的渠道名称及商户 ID")
	}
	if err := validateEpayURL(input.PayAddress, false); err != nil {
		return err
	}
	if err := validateEpayURL(input.CallbackAddress, true); err != nil {
		return err
	}
	if len(input.Methods) == 0 || len(input.Methods) > 3 {
		return fmt.Errorf("请至少开启一种支付方式")
	}
	seen := make(map[string]bool)
	for _, method := range input.Methods {
		if (method != "wxpay" && method != "alipay" && method != "qqpay") || seen[method] {
			return fmt.Errorf("支付方式无效或重复")
		}
		seen[method] = true
	}
	for method := range seen {
		if _, exists := input.MethodLimits[method]; !exists {
			return fmt.Errorf("请为每个已启用支付方式配置限额")
		}
	}
	for method, limits := range input.MethodLimits {
		if method != "wxpay" && method != "alipay" && method != "qqpay" {
			return fmt.Errorf("支付方式限额配置无效")
		}
		if _, _, _, err := model.ValidateEpayMethodLimits(limits); err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
	}
	if input.TopupOfferType == "" {
		input.TopupOfferType = "none"
	}
	if input.TopupOfferType == "none" {
		input.TopupOfferPercent = "100"
		input.TopupOfferThresholdCNY = "0"
	}
	percent, err := epayDecimal(input.TopupOfferPercent, 6)
	if err != nil || !percent.IsPositive() || percent.GreaterThan(decimal.NewFromInt(1000000)) {
		return fmt.Errorf("余额优惠百分比无效")
	}
	switch input.TopupOfferType {
	case "bonus":
		if !percent.GreaterThan(decimal.NewFromInt(100)) {
			return fmt.Errorf("赠金百分比必须大于 100")
		}
	case "discount":
		if !percent.LessThan(decimal.NewFromInt(100)) {
			return fmt.Errorf("折扣百分比必须大于 0 且小于 100")
		}
	case "none":
	default:
		return fmt.Errorf("余额优惠类型无效")
	}
	if input.SubscriptionDiscountPercent == "" {
		input.SubscriptionDiscountPercent = "100"
	}
	subscriptionPercent, err := epayDecimal(input.SubscriptionDiscountPercent, 6)
	if err != nil || !subscriptionPercent.IsPositive() || subscriptionPercent.GreaterThan(decimal.NewFromInt(100)) {
		return fmt.Errorf("订阅折扣百分比必须大于 0 且不大于 100，100 表示无折扣")
	}
	for _, threshold := range []string{input.TopupOfferThresholdCNY, input.SubscriptionOfferThresholdCNY} {
		if _, err := epayDecimal(threshold, 2); err != nil {
			return fmt.Errorf("优惠起点金额必须不小于 0，最多两位小数")
		}
	}
	if id > 0 {
		current, err := model.GetEpayChannel(id)
		if err != nil {
			return err
		}
		if input.MerchantKey == "" {
			revision, err := model.GetEpayChannelRevision(current.CurrentRevisionId)
			if err != nil {
				return err
			}
			input.MerchantKey = revision.MerchantKey
		}
	}
	if input.MerchantKey == "" || len(input.MerchantKey) > 4096 {
		return fmt.Errorf("请填写有效的商户密钥")
	}
	methods, err := common.Marshal(input.Methods)
	if err != nil {
		return err
	}
	input.PaymentMethods = string(methods)
	limitsJSON, err := common.Marshal(input.MethodLimits)
	if err != nil {
		return err
	}
	input.PaymentMethodLimits = string(limitsJSON)
	channel := &model.EpayChannel{Id: id, Enabled: input.Enabled}
	revision := &model.EpayChannelRevision{EpayChannelConfig: input.EpayChannelConfig, MerchantKey: input.MerchantKey}
	return model.SaveEpayChannel(channel, revision, input.RevisionID)
}

func ListAdminEpayChannels() ([]EpayChannelAdminView, error) {
	channels, err := model.ListEpayChannels(false)
	if err != nil {
		return nil, err
	}
	result := make([]EpayChannelAdminView, 0, len(channels))
	for _, channel := range channels {
		revision, err := model.GetEpayChannelRevision(channel.CurrentRevisionId)
		if err != nil {
			return nil, err
		}
		var methods []string
		if err := common.UnmarshalJsonStr(revision.PaymentMethods, &methods); err != nil {
			return nil, err
		}
		limits, err := model.EpayRevisionMethodLimits(revision)
		if err != nil {
			return nil, err
		}
		result = append(result, EpayChannelAdminView{EpayChannelConfig: revision.EpayChannelConfig, ID: channel.Id, Enabled: channel.Enabled, SortOrder: channel.SortOrder, RevisionID: revision.Id, Methods: methods, HasMerchantKey: revision.MerchantKey != "", MethodLimits: limits})
	}
	return result, nil
}

func ListPublicEpayChannels(plan *model.SubscriptionPlan) ([]types.EpayChannelInfo, error) {
	channels, err := model.ListEpayChannels(true)
	if err != nil {
		return nil, err
	}
	result := make([]types.EpayChannelInfo, 0, len(channels))
	for _, channel := range channels {
		revision, err := model.GetEpayChannelRevision(channel.CurrentRevisionId)
		if err != nil {
			return nil, err
		}
		var methods []string
		if revision.MerchantKey == "" || common.UnmarshalJsonStr(revision.PaymentMethods, &methods) != nil || len(methods) == 0 {
			continue
		}
		if _, err := epayNotifyURL(revision, types.PaymentSceneTopup); err != nil {
			continue
		}
		limits, err := model.EpayRevisionMethodLimits(revision)
		if err != nil {
			return nil, err
		}
		info := types.EpayChannelInfo{ID: channel.Id, RevisionID: revision.Id, Name: revision.Name, Description: revision.Description, PaymentMethods: methods, TopupOfferType: revision.TopupOfferType, TopupOfferPercent: revision.TopupOfferPercent, TopupOfferThresholdCNY: revision.TopupOfferThresholdCNY, SubscriptionDiscountPercent: revision.SubscriptionDiscountPercent, SubscriptionOfferThresholdCNY: revision.SubscriptionOfferThresholdCNY, MethodLimits: make(map[string]types.EpayMethodInfo)}
		for _, method := range methods {
			methodInfo := types.EpayMethodInfo{EpayMethodLimits: limits[method]}
			if _, _, _, err := model.ValidateEpayMethodLimits(limits[method]); err != nil {
				return nil, err
			}
			remaining, reset, err := epayDailyRemaining(revision.ChannelId, method, limits[method].DailyLimitCNY)
			if err != nil {
				return nil, err
			}
			methodInfo.DailyRemainingCNY, methodInfo.DailyResetAt = remaining, reset
			if plan != nil {
				quote, err := CalculateEpayQuote(revision, method, types.PaymentSceneSubscription, "", plan)
				if err != nil {
					return nil, err
				}
				applyEpayDailyAvailability(quote, remaining, reset)
				methodInfo.SubscriptionQuote = quote
			}
			info.MethodLimits[method] = methodInfo
		}
		result = append(result, info)
	}
	return result, nil
}

func GetEnabledEpayRevision(channelID int, method string) (*model.EpayChannelRevision, error) {
	channel, err := model.GetEpayChannel(channelID)
	if err != nil || !channel.Enabled {
		return nil, fmt.Errorf("易支付渠道不存在或已停用")
	}
	revision, err := model.GetEpayChannelRevision(channel.CurrentRevisionId)
	if err != nil {
		return nil, err
	}
	var methods []string
	if err := common.UnmarshalJsonStr(revision.PaymentMethods, &methods); err != nil {
		return nil, err
	}
	for _, available := range methods {
		if available == method {
			return revision, nil
		}
	}
	return nil, fmt.Errorf("该易支付渠道不支持所选支付方式")
}

func CalculateEpayQuote(revision *model.EpayChannelRevision, method string, scene types.PaymentScene, amount string, plan *model.SubscriptionPlan) (*types.EpayQuote, error) {
	if revision == nil {
		return nil, fmt.Errorf("易支付渠道版本不存在")
	}
	quote := &types.EpayQuote{ChannelID: revision.ChannelId, RevisionID: revision.Id, Scene: scene, OfferType: "none", OfferPercent: "100", BonusUSD: "0", BaseUSD: "0", CreditUSD: "0", Payable: true}
	configured, err := model.EpayRevisionMethodLimits(revision)
	if err != nil {
		return nil, err
	}
	limits, exists := configured[method]
	if !exists {
		return nil, fmt.Errorf("所选支付方式未配置限额")
	}
	minimum, maximum, _, err := model.ValidateEpayMethodLimits(limits)
	if err != nil {
		return nil, err
	}
	quote.PaymentMethod = method
	quote.MinPayCNY, quote.MaxPayCNY, quote.DailyLimitCNY = limits.MinPayCNY, limits.MaxPayCNY, limits.DailyLimitCNY
	if operation_setting.USDExchangeRate <= 0 || math.IsNaN(operation_setting.USDExchangeRate) || math.IsInf(operation_setting.USDExchangeRate, 0) {
		return nil, fmt.Errorf("系统美元汇率无效")
	}
	rate := decimal.NewFromFloat(operation_setting.USDExchangeRate)
	quote.USDExchangeRate = rate.String()
	var input decimal.Decimal
	var threshold decimal.Decimal
	var percent decimal.Decimal
	if scene == types.PaymentSceneTopup {
		if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
			return nil, fmt.Errorf("余额额度单位配置无效")
		}
		input, err = epayDecimal(amount, 2)
		if err != nil || !input.IsPositive() {
			return nil, fmt.Errorf("充值金额必须大于 0，最多两位小数且不超过 %d CNY", maxEpayAmountCNY)
		}
		threshold, err = epayDecimal(revision.TopupOfferThresholdCNY, 2)
		if err != nil {
			return nil, err
		}
		percent, err = epayDecimal(revision.TopupOfferPercent, 6)
		if err != nil || !percent.IsPositive() || percent.GreaterThan(decimal.NewFromInt(1000000)) || (revision.TopupOfferType == "bonus" && !percent.GreaterThan(decimal.NewFromInt(100))) || (revision.TopupOfferType == "discount" && !percent.LessThan(decimal.NewFromInt(100))) || (revision.TopupOfferType != "none" && revision.TopupOfferType != "bonus" && revision.TopupOfferType != "discount") {
			return nil, fmt.Errorf("余额优惠配置无效")
		}
		quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quote.BaseQuota, err = common.QuotaFromDecimalStrict(input.Mul(quotaPerUnit).Div(rate))
		if err != nil || quote.BaseQuota <= 0 {
			return nil, fmt.Errorf("充值额度超出安全范围")
		}
		quote.CreditQuota = quote.BaseQuota
		if input.GreaterThanOrEqual(threshold) && revision.TopupOfferType != "none" {
			quote.OfferType = revision.TopupOfferType
			quote.OfferPercent = percent.String()
			quote.OfferApplied = true
			if quote.OfferType == "bonus" {
				bonus := input.Mul(quotaPerUnit).Mul(percent.Sub(decimal.NewFromInt(100))).Div(rate.Mul(decimal.NewFromInt(100)))
				quote.BonusQuota, err = common.QuotaFromDecimalStrict(bonus)
				if err != nil || quote.BonusQuota < 0 || quote.BaseQuota >= common.MaxQuota-quote.BonusQuota {
					return nil, fmt.Errorf("含赠金的充值额度超出安全范围")
				}
				quote.CreditQuota += quote.BonusQuota
			}
		}
		quote.BaseUSD = decimal.NewFromInt(int64(quote.BaseQuota)).Div(quotaPerUnit).String()
		quote.BonusUSD = decimal.NewFromInt(int64(quote.BonusQuota)).Div(quotaPerUnit).String()
		quote.CreditUSD = decimal.NewFromInt(int64(quote.CreditQuota)).Div(quotaPerUnit).String()
	} else if scene == types.PaymentSceneSubscription && plan != nil {
		if plan.PriceAmount <= 0 || plan.PriceAmount > 9999 || math.IsNaN(plan.PriceAmount) || math.IsInf(plan.PriceAmount, 0) {
			return nil, fmt.Errorf("套餐价格无效")
		}
		input = decimal.NewFromFloat(plan.PriceAmount).Mul(rate).Round(2)
		quote.PlanID = plan.Id
		quote.BaseUSD = decimal.NewFromFloat(plan.PriceAmount).String()
		threshold, err = epayDecimal(revision.SubscriptionOfferThresholdCNY, 2)
		if err != nil {
			return nil, err
		}
		percent, err = epayDecimal(revision.SubscriptionDiscountPercent, 6)
		if err != nil || !percent.IsPositive() || percent.GreaterThan(decimal.NewFromInt(100)) {
			return nil, fmt.Errorf("订阅折扣配置无效")
		}
		if percent.LessThan(decimal.NewFromInt(100)) && input.GreaterThanOrEqual(threshold) {
			quote.OfferType = "discount"
			quote.OfferPercent = percent.String()
			quote.OfferApplied = true
		}
	} else {
		return nil, fmt.Errorf("支付场景无效")
	}
	pay := input
	if quote.OfferType == "discount" {
		pay = input.Mul(percent).Div(decimal.NewFromInt(100)).Round(2)
	}
	quote.InputCNY = input.StringFixed(2)
	quote.PayCNY = pay.StringFixed(2)
	payCents, err := model.EpayAmountCents(quote.PayCNY)
	if err != nil {
		return nil, err
	}
	if payCents <= 0 || payCents < minimum || payCents > maximum {
		quote.Payable = false
		quote.UnavailableReason = fmt.Sprintf("实付金额须在 %s 至 %s CNY 之间", limits.MinPayCNY, limits.MaxPayCNY)
	}
	payload, err := common.Marshal(struct {
		Quote *types.EpayQuote
		Quota int
	}{quote, quote.CreditQuota})
	if err != nil {
		return nil, err
	}
	quote.Token = fmt.Sprintf("%x", sha256.Sum256(payload))
	return quote, nil
}

func QuoteEpay(channelID int, method string, scene types.PaymentScene, amount string, plan *model.SubscriptionPlan) (*types.EpayQuote, error) {
	revision, err := GetEnabledEpayRevision(channelID, method)
	if err != nil {
		return nil, err
	}
	quote, err := CalculateEpayQuote(revision, method, scene, amount, plan)
	if err != nil {
		return nil, err
	}
	remaining, reset, err := epayDailyRemaining(channelID, method, quote.DailyLimitCNY)
	if err != nil {
		return nil, err
	}
	applyEpayDailyAvailability(quote, remaining, reset)
	return quote, nil
}

func epayDailyRemaining(channelID int, method, dailyLimit string) (string, int64, error) {
	limit, err := model.EpayAmountCents(dailyLimit)
	if err != nil {
		return "", 0, err
	}
	now := time.Now()
	_, reset := model.EpayLocalDayBounds(now)
	if limit == 0 {
		return "", reset, nil
	}
	used, reset, err := model.GetEpayDailyUsed(channelID, method, now)
	if err != nil {
		return "", 0, err
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	return decimal.NewFromInt(remaining).Div(decimal.NewFromInt(100)).StringFixed(2), reset, nil
}

func applyEpayDailyAvailability(quote *types.EpayQuote, remaining string, reset int64) {
	quote.DailyRemainingCNY, quote.DailyResetAt = remaining, reset
	if quote.Payable && remaining != "" {
		available, err := decimal.NewFromString(remaining)
		pay, payErr := decimal.NewFromString(quote.PayCNY)
		if err != nil || payErr != nil || pay.GreaterThan(available) {
			quote.Payable = false
			quote.UnavailableReason = "所选支付方式今日剩余额度不足"
		}
	}
}

func NewEpayClient(revision *model.EpayChannelRevision) (*epay.Client, error) {
	if revision == nil || revision.MerchantKey == "" {
		return nil, fmt.Errorf("易支付渠道配置不存在")
	}
	return epay.NewClient(&epay.Config{PartnerID: revision.MerchantId, Key: revision.MerchantKey}, revision.PayAddress)
}

func epayNotifyURL(revision *model.EpayChannelRevision, scene types.PaymentScene) (*url.URL, error) {
	base := revision.CallbackAddress
	if base == "" {
		base = system_setting.ServerAddress
	}
	if err := validateEpayURL(base, false); err != nil {
		return nil, err
	}
	path := "/api/user/epay/notify"
	if scene == types.PaymentSceneSubscription {
		path = "/api/subscription/epay/notify"
	}
	return url.Parse(strings.TrimRight(base, "/") + path)
}
