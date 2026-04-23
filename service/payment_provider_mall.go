package service

import (
	"fmt"
	"net/url"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

type MallPaymentProvider struct{}

func init() {
	RegisterPaymentProvider(&MallPaymentProvider{})
}

func (p *MallPaymentProvider) ID() types.PaymentProvider {
	return types.PaymentProviderMall
}

func (p *MallPaymentProvider) DisplayName() string {
	return "Mall"
}

func (p *MallPaymentProvider) SupportsTopup() bool {
	return true
}

func (p *MallPaymentProvider) SupportsSubscription() bool {
	return true
}

func (p *MallPaymentProvider) GetTopupMeta() *types.ProviderMeta {
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

func (p *MallPaymentProvider) GetSubscriptionMeta(plan *model.SubscriptionPlan) *types.ProviderMeta {
	return &types.ProviderMeta{
		Scene:       types.PaymentSceneSubscription,
		Provider:    p.ID(),
		DisplayName: p.DisplayName(),
		ActionType:  types.CheckoutActionTypeRedirectURL,
		Enabled:     plan != nil && plan.Enabled,
		ConfigReady: p.ValidateSubscriptionConfig(plan) == nil,
	}
}

func (p *MallPaymentProvider) ValidateTopupConfig() error {
	if len(operation_setting.GetPaymentSetting().MallLinks) == 0 {
		return fmt.Errorf("%w: payment_setting.mall_links", ErrPaymentProviderNotConfigured)
	}
	return nil
}

func (p *MallPaymentProvider) ValidateSubscriptionConfig(plan *model.SubscriptionPlan) error {
	if plan == nil {
		return fmt.Errorf("%w: subscription plan", ErrPaymentProviderNotConfigured)
	}
	if plan.MallLink == "" {
		return fmt.Errorf("%w: mall_link", ErrPaymentProviderNotConfigured)
	}
	parsed, err := url.Parse(plan.MallLink)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: mall_link", ErrPaymentProviderNotConfigured)
	}
	return nil
}

func (p *MallPaymentProvider) CreateTopupCheckout(req *types.TopupCheckoutRequest) (*types.CheckoutResult, error) {
	if req == nil {
		return nil, fmt.Errorf("参数错误")
	}
	if req.Amount < getTopupMinAmount() {
		return nil, fmt.Errorf("充值数量不能小于%d", getTopupMinAmount())
	}
	link, err := buildMallTopupLink(req.Amount)
	if err != nil {
		return nil, err
	}
	return createRedirectCheckoutResult(types.PaymentSceneTopup, p.ID(), link), nil
}

func (p *MallPaymentProvider) CreateSubscriptionCheckout(plan *model.SubscriptionPlan, req *types.SubscriptionCheckoutRequest) (*types.CheckoutResult, error) {
	if req == nil || plan == nil {
		return nil, fmt.Errorf("参数错误")
	}
	if err := ensureUserCanPurchaseSubscription(req.UserID, plan); err != nil {
		return nil, err
	}
	return createRedirectCheckoutResult(types.PaymentSceneSubscription, p.ID(), plan.MallLink), nil
}
