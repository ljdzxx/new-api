package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
)

type StripePaymentProvider struct{}

func init() {
	RegisterPaymentProvider(&StripePaymentProvider{})
}

func (p *StripePaymentProvider) ID() types.PaymentProvider {
	return types.PaymentProviderStripe
}

func (p *StripePaymentProvider) DisplayName() string {
	return "Stripe"
}

func (p *StripePaymentProvider) SupportsTopup() bool {
	return true
}

func (p *StripePaymentProvider) SupportsSubscription() bool {
	return true
}

func (p *StripePaymentProvider) GetTopupMeta() *types.ProviderMeta {
	return &types.ProviderMeta{
		Scene:          types.PaymentSceneTopup,
		Provider:       p.ID(),
		DisplayName:    p.DisplayName(),
		ActionType:     types.CheckoutActionTypeRedirectURL,
		RequiresAmount: true,
		Enabled:        true,
		ConfigReady:    p.ValidateTopupConfig() == nil,
	}
}

func (p *StripePaymentProvider) GetSubscriptionMeta(plan *model.SubscriptionPlan) *types.ProviderMeta {
	return &types.ProviderMeta{
		Scene:       types.PaymentSceneSubscription,
		Provider:    p.ID(),
		DisplayName: p.DisplayName(),
		ActionType:  types.CheckoutActionTypeRedirectURL,
		Enabled:     plan != nil && plan.Enabled,
		ConfigReady: p.ValidateSubscriptionConfig(plan) == nil,
	}
}

func (p *StripePaymentProvider) ValidateTopupConfig() error {
	if setting.StripeApiSecret == "" {
		return fmt.Errorf("%w: StripeApiSecret", ErrPaymentProviderNotConfigured)
	}
	if setting.StripeWebhookSecret == "" {
		return fmt.Errorf("%w: StripeWebhookSecret", ErrPaymentProviderNotConfigured)
	}
	if setting.StripePriceId == "" {
		return fmt.Errorf("%w: StripePriceId", ErrPaymentProviderNotConfigured)
	}
	return nil
}

func (p *StripePaymentProvider) ValidateSubscriptionConfig(plan *model.SubscriptionPlan) error {
	if plan == nil {
		return fmt.Errorf("%w: subscription plan", ErrPaymentProviderNotConfigured)
	}
	if err := p.ValidateTopupConfig(); err != nil {
		return err
	}
	if plan.StripePriceId == "" {
		return fmt.Errorf("%w: stripe_price_id", ErrPaymentProviderNotConfigured)
	}
	return nil
}

func (p *StripePaymentProvider) CreateTopupCheckout(req *types.TopupCheckoutRequest) (*types.CheckoutResult, error) {
	if req == nil {
		return nil, fmt.Errorf("参数错误")
	}
	if req.PaymentMethod != "" && req.PaymentMethod != string(types.PaymentProviderStripe) {
		return nil, fmt.Errorf("不支持的支付渠道")
	}
	if req.Amount < getStripeMinTopupAmount() {
		return nil, fmt.Errorf("充值数量不能小于%d", getStripeMinTopupAmount())
	}
	if req.Amount > 10000 {
		return nil, fmt.Errorf("充值数量不能大于10000")
	}
	if req.SuccessURL != "" && common.ValidateRedirectURL(req.SuccessURL) != nil {
		return nil, fmt.Errorf("支付成功重定向URL不在可信任域名列表中")
	}
	if req.CancelURL != "" && common.ValidateRedirectURL(req.CancelURL) != nil {
		return nil, fmt.Errorf("支付取消重定向URL不在可信任域名列表中")
	}

	user, err := model.GetUserById(req.UserID, false)
	if err != nil || user == nil {
		return nil, fmt.Errorf("用户不存在")
	}
	referenceID := buildStripeTopupReference(user.Id)
	payLink, err := buildStripeTopupLink(referenceID, user.StripeCustomer, user.Email, req.Amount, req.SuccessURL, req.CancelURL)
	if err != nil {
		return nil, err
	}

	topUp := &model.TopUp{
		UserId:        user.Id,
		Amount:        req.Amount,
		Money:         getStripeChargedAmount(float64(req.Amount), *user),
		TradeNo:       referenceID,
		PaymentMethod: string(types.PaymentProviderStripe),
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		return nil, fmt.Errorf("创建订单失败")
	}
	return createRedirectCheckoutResult(types.PaymentSceneTopup, p.ID(), payLink), nil
}

func (p *StripePaymentProvider) CreateSubscriptionCheckout(plan *model.SubscriptionPlan, req *types.SubscriptionCheckoutRequest) (*types.CheckoutResult, error) {
	if req == nil || plan == nil {
		return nil, fmt.Errorf("参数错误")
	}
	if err := ensureUserCanPurchaseSubscription(req.UserID, plan); err != nil {
		return nil, err
	}
	user, err := model.GetUserById(req.UserID, false)
	if err != nil || user == nil {
		return nil, fmt.Errorf("用户不存在")
	}

	referenceID := buildStripeSubscriptionReference(user.Id)
	payLink, err := buildStripeSubscriptionLink(referenceID, user.StripeCustomer, user.Email, plan.StripePriceId)
	if err != nil {
		return nil, err
	}
	order := &model.SubscriptionOrder{
		UserId:        req.UserID,
		PlanId:        plan.Id,
		Money:         plan.PriceAmount,
		TradeNo:       referenceID,
		PaymentMethod: string(types.PaymentProviderStripe),
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		return nil, fmt.Errorf("创建订单失败")
	}
	return createRedirectCheckoutResult(types.PaymentSceneSubscription, p.ID(), payLink), nil
}
