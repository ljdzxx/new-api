package service

import (
	"errors"
	"sync"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
)

var (
	ErrPaymentProviderNotFound       = errors.New("payment provider not found")
	ErrPaymentProviderNotConfigured  = errors.New("payment provider is not configured")
	ErrPaymentProviderDisabled       = errors.New("payment provider is disabled")
	ErrLegacyAutoRouting             = errors.New("legacy auto routing is not implemented in the new router yet")
	ErrPaymentProviderNotImplemented = errors.New("payment provider behavior is not implemented yet")
)

type PaymentProvider interface {
	ID() types.PaymentProvider
	DisplayName() string
	SupportsTopup() bool
	SupportsSubscription() bool
	GetTopupMeta() *types.ProviderMeta
	GetSubscriptionMeta(plan *model.SubscriptionPlan) *types.ProviderMeta
	ValidateTopupConfig() error
	ValidateSubscriptionConfig(plan *model.SubscriptionPlan) error
	CreateTopupCheckout(req *types.TopupCheckoutRequest) (*types.CheckoutResult, error)
	CreateSubscriptionCheckout(plan *model.SubscriptionPlan, req *types.SubscriptionCheckoutRequest) (*types.CheckoutResult, error)
}

var (
	paymentProviders     = make(map[types.PaymentProvider]PaymentProvider)
	paymentProvidersLock sync.RWMutex
)

func RegisterPaymentProvider(provider PaymentProvider) {
	if provider == nil {
		return
	}
	paymentProvidersLock.Lock()
	defer paymentProvidersLock.Unlock()
	paymentProviders[provider.ID()] = provider
}

func GetPaymentProvider(providerID types.PaymentProvider) (PaymentProvider, bool) {
	paymentProvidersLock.RLock()
	defer paymentProvidersLock.RUnlock()
	provider, ok := paymentProviders[providerID]
	return provider, ok
}

func ListPaymentProviders() []PaymentProvider {
	paymentProvidersLock.RLock()
	defer paymentProvidersLock.RUnlock()
	providers := make([]PaymentProvider, 0, len(paymentProviders))
	for _, provider := range paymentProviders {
		providers = append(providers, provider)
	}
	return providers
}
