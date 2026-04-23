package service

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

func ResolveTopupProviderID() types.PaymentProvider {
	return operation_setting.GetTopupPaymentProvider()
}

func ResolveSubscriptionProviderID() types.PaymentProvider {
	return operation_setting.GetSubscriptionPaymentProvider()
}

func ResolveTopupProvider() (PaymentProvider, error) {
	providerID := ResolveTopupProviderID()
	switch providerID {
	case types.PaymentProviderDisabled:
		return nil, ErrPaymentProviderDisabled
	case types.PaymentProviderLegacyAuto:
		return nil, ErrLegacyAutoRouting
	}
	provider, ok := GetPaymentProvider(providerID)
	if !ok {
		return nil, ErrPaymentProviderNotFound
	}
	if !provider.SupportsTopup() {
		return nil, ErrPaymentProviderNotConfigured
	}
	return provider, nil
}

func ResolveSubscriptionProvider(plan *model.SubscriptionPlan) (PaymentProvider, error) {
	providerID := ResolveSubscriptionProviderID()
	switch providerID {
	case types.PaymentProviderDisabled:
		return nil, ErrPaymentProviderDisabled
	case types.PaymentProviderLegacyAuto:
		return nil, ErrLegacyAutoRouting
	}
	provider, ok := GetPaymentProvider(providerID)
	if !ok {
		return nil, ErrPaymentProviderNotFound
	}
	if !provider.SupportsSubscription() {
		return nil, ErrPaymentProviderNotConfigured
	}
	if err := provider.ValidateSubscriptionConfig(plan); err != nil {
		return nil, err
	}
	return provider, nil
}

func GetTopupMeta() (*types.ProviderMeta, error) {
	providerID := ResolveTopupProviderID()
	if providerID == types.PaymentProviderLegacyAuto {
		return &types.ProviderMeta{
			Scene:       types.PaymentSceneTopup,
			Provider:    providerID,
			DisplayName: "Legacy Auto",
			LegacyAuto:  true,
			Enabled:     true,
			ConfigReady: true,
		}, nil
	}
	if providerID == types.PaymentProviderDisabled {
		return &types.ProviderMeta{
			Scene:       types.PaymentSceneTopup,
			Provider:    providerID,
			DisplayName: "Disabled",
			Enabled:     false,
			ConfigReady: false,
		}, nil
	}
	provider, err := ResolveTopupProvider()
	if err != nil {
		return nil, err
	}
	return provider.GetTopupMeta(), nil
}

func GetSubscriptionMeta(plan *model.SubscriptionPlan) (*types.ProviderMeta, error) {
	providerID := ResolveSubscriptionProviderID()
	if providerID == types.PaymentProviderLegacyAuto {
		return &types.ProviderMeta{
			Scene:       types.PaymentSceneSubscription,
			Provider:    providerID,
			DisplayName: "Legacy Auto",
			LegacyAuto:  true,
			Enabled:     true,
			ConfigReady: true,
		}, nil
	}
	if providerID == types.PaymentProviderDisabled {
		return &types.ProviderMeta{
			Scene:       types.PaymentSceneSubscription,
			Provider:    providerID,
			DisplayName: "Disabled",
			Enabled:     false,
			ConfigReady: false,
		}, nil
	}
	provider, err := ResolveSubscriptionProvider(plan)
	if err != nil {
		return nil, err
	}
	return provider.GetSubscriptionMeta(plan), nil
}

func CreateTopupCheckout(req *types.TopupCheckoutRequest) (*types.CheckoutResult, error) {
	if ResolveTopupProviderID() == types.PaymentProviderLegacyAuto {
		providerID, err := inferLegacyTopupProvider(req)
		if err != nil {
			return nil, err
		}
		return CreateTopupCheckoutWithProvider(providerID, req)
	}
	provider, err := ResolveTopupProvider()
	if err != nil {
		return nil, err
	}
	if err := provider.ValidateTopupConfig(); err != nil {
		return nil, err
	}
	return provider.CreateTopupCheckout(req)
}

func CreateSubscriptionCheckout(plan *model.SubscriptionPlan, req *types.SubscriptionCheckoutRequest) (*types.CheckoutResult, error) {
	if ResolveSubscriptionProviderID() == types.PaymentProviderLegacyAuto {
		providerID, err := inferLegacySubscriptionProvider(plan, req)
		if err != nil {
			return nil, err
		}
		return CreateSubscriptionCheckoutWithProvider(providerID, plan, req)
	}
	provider, err := ResolveSubscriptionProvider(plan)
	if err != nil {
		return nil, err
	}
	return provider.CreateSubscriptionCheckout(plan, req)
}

func inferLegacyTopupProvider(req *types.TopupCheckoutRequest) (types.PaymentProvider, error) {
	if req == nil {
		return "", ErrLegacyAutoRouting
	}
	if req.ProductID != "" {
		return types.PaymentProviderCreem, nil
	}
	switch req.PaymentMethod {
	case string(types.PaymentProviderStripe):
		return types.PaymentProviderStripe, nil
	case string(types.PaymentProviderCreem):
		return types.PaymentProviderCreem, nil
	case string(types.PaymentProviderMall):
		return types.PaymentProviderMall, nil
	case "":
		return "", ErrLegacyAutoRouting
	default:
		if operation_setting.ContainsPayMethod(req.PaymentMethod) {
			return types.PaymentProviderEpay, nil
		}
		return "", ErrLegacyAutoRouting
	}
}

func inferLegacySubscriptionProvider(plan *model.SubscriptionPlan, req *types.SubscriptionCheckoutRequest) (types.PaymentProvider, error) {
	if plan == nil || req == nil {
		return "", ErrLegacyAutoRouting
	}
	switch req.PaymentMethod {
	case string(types.PaymentProviderStripe):
		return types.PaymentProviderStripe, nil
	case string(types.PaymentProviderCreem):
		return types.PaymentProviderCreem, nil
	case string(types.PaymentProviderMall):
		return types.PaymentProviderMall, nil
	case "":
		if plan.MallLink != "" {
			return types.PaymentProviderMall, nil
		}
		return "", ErrLegacyAutoRouting
	default:
		if operation_setting.ContainsPayMethod(req.PaymentMethod) {
			return types.PaymentProviderEpay, nil
		}
		return "", ErrLegacyAutoRouting
	}
}

func CreateTopupCheckoutWithProvider(providerID types.PaymentProvider, req *types.TopupCheckoutRequest) (*types.CheckoutResult, error) {
	provider, ok := GetPaymentProvider(providerID)
	if !ok {
		return nil, ErrPaymentProviderNotFound
	}
	if !provider.SupportsTopup() {
		return nil, ErrPaymentProviderNotConfigured
	}
	if err := provider.ValidateTopupConfig(); err != nil {
		return nil, err
	}
	return provider.CreateTopupCheckout(req)
}

func CreateSubscriptionCheckoutWithProvider(providerID types.PaymentProvider, plan *model.SubscriptionPlan, req *types.SubscriptionCheckoutRequest) (*types.CheckoutResult, error) {
	provider, ok := GetPaymentProvider(providerID)
	if !ok {
		return nil, ErrPaymentProviderNotFound
	}
	if !provider.SupportsSubscription() {
		return nil, ErrPaymentProviderNotConfigured
	}
	if err := provider.ValidateSubscriptionConfig(plan); err != nil {
		return nil, err
	}
	return provider.CreateSubscriptionCheckout(plan, req)
}
