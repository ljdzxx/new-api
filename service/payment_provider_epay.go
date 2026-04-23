package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

type EpayPaymentProvider struct{}

func init() {
	RegisterPaymentProvider(&EpayPaymentProvider{})
}

func (p *EpayPaymentProvider) ID() types.PaymentProvider {
	return types.PaymentProviderEpay
}

func (p *EpayPaymentProvider) DisplayName() string {
	return "Epay"
}

func (p *EpayPaymentProvider) SupportsTopup() bool {
	return true
}

func (p *EpayPaymentProvider) SupportsSubscription() bool {
	return true
}

func (p *EpayPaymentProvider) GetTopupMeta() *types.ProviderMeta {
	return &types.ProviderMeta{
		Scene:             types.PaymentSceneTopup,
		Provider:          p.ID(),
		DisplayName:       p.DisplayName(),
		ActionType:        types.CheckoutActionTypeFormPost,
		RequiresAmount:    true,
		RequiresChannel:   true,
		Enabled:           true,
		ConfigReady:       p.ValidateTopupConfig() == nil,
		AvailableChannels: operation_setting.PayMethods,
	}
}

func (p *EpayPaymentProvider) GetSubscriptionMeta(plan *model.SubscriptionPlan) *types.ProviderMeta {
	return &types.ProviderMeta{
		Scene:             types.PaymentSceneSubscription,
		Provider:          p.ID(),
		DisplayName:       p.DisplayName(),
		ActionType:        types.CheckoutActionTypeFormPost,
		RequiresChannel:   true,
		Enabled:           plan != nil && plan.Enabled,
		ConfigReady:       p.ValidateSubscriptionConfig(plan) == nil,
		AvailableChannels: operation_setting.PayMethods,
	}
}

func (p *EpayPaymentProvider) ValidateTopupConfig() error {
	if operation_setting.PayAddress == "" {
		return fmt.Errorf("%w: PayAddress", ErrPaymentProviderNotConfigured)
	}
	if operation_setting.EpayId == "" {
		return fmt.Errorf("%w: EpayId", ErrPaymentProviderNotConfigured)
	}
	if operation_setting.EpayKey == "" {
		return fmt.Errorf("%w: EpayKey", ErrPaymentProviderNotConfigured)
	}
	if len(operation_setting.PayMethods) == 0 {
		return fmt.Errorf("%w: PayMethods", ErrPaymentProviderNotConfigured)
	}
	return nil
}

func (p *EpayPaymentProvider) ValidateSubscriptionConfig(plan *model.SubscriptionPlan) error {
	if plan == nil {
		return fmt.Errorf("%w: subscription plan", ErrPaymentProviderNotConfigured)
	}
	return p.ValidateTopupConfig()
}

func (p *EpayPaymentProvider) CreateTopupCheckout(req *types.TopupCheckoutRequest) (*types.CheckoutResult, error) {
	if req == nil {
		return nil, fmt.Errorf("参数错误")
	}
	if req.Amount < getTopupMinAmount() {
		return nil, fmt.Errorf("充值数量不能小于%d", getTopupMinAmount())
	}
	if !operation_setting.ContainsPayMethod(req.PaymentMethod) {
		return nil, fmt.Errorf("支付方式不存在")
	}

	group, err := model.GetUserGroup(req.UserID, true)
	if err != nil {
		return nil, fmt.Errorf("获取用户分组失败")
	}
	payMoney := getTopupPayMoney(req.Amount, group)
	if payMoney < 0.01 {
		return nil, fmt.Errorf("充值金额过低")
	}

	client, err := newEpayClient()
	if err != nil {
		return nil, err
	}
	returnURL, err := buildTopupEpayReturnURL()
	if err != nil {
		return nil, fmt.Errorf("回调地址配置错误")
	}
	notifyURL, err := buildTopupEpayNotifyURL()
	if err != nil {
		return nil, fmt.Errorf("回调地址配置错误")
	}

	tradeNo := buildTopupTradeNo(req.UserID)
	uri, params, err := client.Purchase(buildTopupPurchaseArgs(req.PaymentMethod, tradeNo, req.Amount, payMoney, notifyURL, returnURL))
	if err != nil {
		return nil, fmt.Errorf("拉起支付失败")
	}

	topUp := &model.TopUp{
		UserId:        req.UserID,
		Amount:        normalizeTopupStoredAmount(req.Amount),
		Money:         payMoney,
		TradeNo:       tradeNo,
		PaymentMethod: req.PaymentMethod,
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}
	if err = topUp.Insert(); err != nil {
		return nil, fmt.Errorf("创建订单失败")
	}
	return createFormCheckoutResult(types.PaymentSceneTopup, p.ID(), uri, stringifyMap(params)), nil
}

func (p *EpayPaymentProvider) CreateSubscriptionCheckout(plan *model.SubscriptionPlan, req *types.SubscriptionCheckoutRequest) (*types.CheckoutResult, error) {
	if req == nil || plan == nil {
		return nil, fmt.Errorf("参数错误")
	}
	if plan.PriceAmount < 0.01 {
		return nil, fmt.Errorf("套餐金额过低")
	}
	if !operation_setting.ContainsPayMethod(req.PaymentMethod) {
		return nil, fmt.Errorf("支付方式不存在")
	}
	if err := ensureUserCanPurchaseSubscription(req.UserID, plan); err != nil {
		return nil, err
	}

	client, err := newEpayClient()
	if err != nil {
		return nil, err
	}
	returnURL, err := buildSubscriptionEpayReturnURL()
	if err != nil {
		return nil, fmt.Errorf("回调地址配置错误")
	}
	notifyURL, err := buildSubscriptionEpayNotifyURL()
	if err != nil {
		return nil, fmt.Errorf("回调地址配置错误")
	}

	tradeNo := buildSubscriptionEpayTradeNo(req.UserID)
	order := &model.SubscriptionOrder{
		UserId:        req.UserID,
		PlanId:        plan.Id,
		Money:         plan.PriceAmount,
		TradeNo:       tradeNo,
		PaymentMethod: req.PaymentMethod,
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		return nil, fmt.Errorf("创建订单失败")
	}

	uri, params, err := client.Purchase(buildSubscriptionPurchaseArgs(req.PaymentMethod, tradeNo, plan.Title, plan.PriceAmount, notifyURL, returnURL))
	if err != nil {
		_ = model.ExpireSubscriptionOrder(tradeNo)
		return nil, fmt.Errorf("拉起支付失败")
	}
	return createFormCheckoutResult(types.PaymentSceneSubscription, p.ID(), uri, stringifyMap(params)), nil
}
