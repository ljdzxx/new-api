package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
)

type CreemPaymentProvider struct{}

func init() {
	RegisterPaymentProvider(&CreemPaymentProvider{})
}

func (p *CreemPaymentProvider) ID() types.PaymentProvider {
	return types.PaymentProviderCreem
}

func (p *CreemPaymentProvider) DisplayName() string {
	return "Creem"
}

func (p *CreemPaymentProvider) SupportsTopup() bool {
	return true
}

func (p *CreemPaymentProvider) SupportsSubscription() bool {
	return true
}

func (p *CreemPaymentProvider) GetTopupMeta() *types.ProviderMeta {
	return &types.ProviderMeta{
		Scene:                types.PaymentSceneTopup,
		Provider:             p.ID(),
		DisplayName:          p.DisplayName(),
		ActionType:           types.CheckoutActionTypeRedirectURL,
		RequiresProduct:      true,
		Enabled:              true,
		ConfigReady:          p.ValidateTopupConfig() == nil,
		RawProductConfigJSON: setting.CreemProducts,
	}
}

func (p *CreemPaymentProvider) GetSubscriptionMeta(plan *model.SubscriptionPlan) *types.ProviderMeta {
	return &types.ProviderMeta{
		Scene:       types.PaymentSceneSubscription,
		Provider:    p.ID(),
		DisplayName: p.DisplayName(),
		ActionType:  types.CheckoutActionTypeRedirectURL,
		Enabled:     plan != nil && plan.Enabled,
		ConfigReady: p.ValidateSubscriptionConfig(plan) == nil,
	}
}

func (p *CreemPaymentProvider) ValidateTopupConfig() error {
	if setting.CreemApiKey == "" {
		return fmt.Errorf("%w: CreemApiKey", ErrPaymentProviderNotConfigured)
	}
	if setting.CreemProducts == "" || setting.CreemProducts == "[]" {
		return fmt.Errorf("%w: CreemProducts", ErrPaymentProviderNotConfigured)
	}
	if setting.CreemWebhookSecret == "" && !setting.CreemTestMode {
		return fmt.Errorf("%w: CreemWebhookSecret", ErrPaymentProviderNotConfigured)
	}
	return nil
}

func (p *CreemPaymentProvider) ValidateSubscriptionConfig(plan *model.SubscriptionPlan) error {
	if plan == nil {
		return fmt.Errorf("%w: subscription plan", ErrPaymentProviderNotConfigured)
	}
	if err := p.ValidateTopupConfig(); err != nil {
		return err
	}
	if plan.CreemProductId == "" {
		return fmt.Errorf("%w: creem_product_id", ErrPaymentProviderNotConfigured)
	}
	return nil
}

func (p *CreemPaymentProvider) CreateTopupCheckout(req *types.TopupCheckoutRequest) (*types.CheckoutResult, error) {
	if req == nil {
		return nil, fmt.Errorf("参数错误")
	}
	if req.PaymentMethod != "" && req.PaymentMethod != string(types.PaymentProviderCreem) {
		return nil, fmt.Errorf("不支持的支付渠道")
	}
	if req.ProductID == "" {
		return nil, fmt.Errorf("请选择产品")
	}
	product, err := findCreemProduct(req.ProductID)
	if err != nil {
		return nil, err
	}
	user, err := model.GetUserById(req.UserID, false)
	if err != nil || user == nil {
		return nil, fmt.Errorf("用户不存在")
	}
	referenceID := buildCreemTopupReference(user.Id)
	topUp := &model.TopUp{
		UserId:        user.Id,
		Amount:        product.Quota,
		Money:         product.Price,
		TradeNo:       referenceID,
		PaymentMethod: string(types.PaymentProviderCreem),
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		return nil, fmt.Errorf("创建订单失败")
	}
	checkoutURL, err := buildCreemCheckoutLink(referenceID, product, user.Email, user.Username)
	if err != nil {
		return nil, err
	}
	return createRedirectCheckoutResult(types.PaymentSceneTopup, p.ID(), checkoutURL), nil
}

func (p *CreemPaymentProvider) CreateSubscriptionCheckout(plan *model.SubscriptionPlan, req *types.SubscriptionCheckoutRequest) (*types.CheckoutResult, error) {
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
	referenceID := buildCreemSubscriptionReference(user)
	order := &model.SubscriptionOrder{
		UserId:        user.Id,
		PlanId:        plan.Id,
		Money:         plan.PriceAmount,
		TradeNo:       referenceID,
		PaymentMethod: string(types.PaymentProviderCreem),
		CreateTime:    common.GetTimestamp(),
		Status:        common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		return nil, fmt.Errorf("创建订单失败")
	}
	checkoutURL, err := buildCreemCheckoutLink(referenceID, buildSubscriptionCreemProduct(plan), user.Email, user.Username)
	if err != nil {
		return nil, err
	}
	return createRedirectCheckoutResult(types.PaymentSceneSubscription, p.ID(), checkoutURL), nil
}
