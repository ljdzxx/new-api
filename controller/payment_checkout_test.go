package controller

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
)

type paymentMetaResponse struct {
	ProviderMeta struct {
		Scene      types.PaymentScene    `json:"scene"`
		Provider   types.PaymentProvider `json:"provider"`
		LegacyAuto bool                  `json:"legacy_auto"`
	} `json:"provider_meta"`
}

type topupCheckoutResponse struct {
	Scene      types.PaymentScene       `json:"scene"`
	Provider   types.PaymentProvider    `json:"provider"`
	ActionType types.CheckoutActionType `json:"action_type"`
	URL        string                   `json:"url"`
}

func TestGetPaymentTopupMetaReturnsLegacyAutoMeta(t *testing.T) {
	routeSetting := operation_setting.GetPaymentRouteSetting()
	prevTopupProvider := routeSetting.TopupProvider
	routeSetting.TopupProvider = types.PaymentProviderLegacyAuto
	defer func() {
		routeSetting.TopupProvider = prevTopupProvider
	}()

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/payment/topup/meta", nil, 1)
	GetPaymentTopupMeta(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var data paymentMetaResponse
	if err := common.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("failed to decode topup meta response: %v", err)
	}
	if data.ProviderMeta.Scene != types.PaymentSceneTopup {
		t.Fatalf("expected topup scene, got %q", data.ProviderMeta.Scene)
	}
	if data.ProviderMeta.Provider != types.PaymentProviderLegacyAuto {
		t.Fatalf("expected legacy_auto provider, got %q", data.ProviderMeta.Provider)
	}
	if !data.ProviderMeta.LegacyAuto {
		t.Fatalf("expected legacy_auto metadata flag to be true")
	}
}

func TestCreatePaymentTopupCheckoutSupportsExplicitMallProvider(t *testing.T) {
	routeSetting := operation_setting.GetPaymentRouteSetting()
	paymentSetting := operation_setting.GetPaymentSetting()
	prevTopupProvider := routeSetting.TopupProvider
	prevMallLinks := paymentSetting.MallLinks

	routeSetting.TopupProvider = types.PaymentProviderMall
	paymentSetting.MallLinks = map[int]string{
		10: "https://mall.example.com/topup/10",
	}
	defer func() {
		routeSetting.TopupProvider = prevTopupProvider
		paymentSetting.MallLinks = prevMallLinks
	}()

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/payment/topup/checkout", map[string]any{
		"amount": 10,
	}, 1)
	CreatePaymentTopupCheckout(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var data topupCheckoutResponse
	if err := common.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("failed to decode topup checkout response: %v", err)
	}
	if data.Scene != types.PaymentSceneTopup {
		t.Fatalf("expected topup scene, got %q", data.Scene)
	}
	if data.Provider != types.PaymentProviderMall {
		t.Fatalf("expected mall provider, got %q", data.Provider)
	}
	if data.ActionType != types.CheckoutActionTypeRedirectURL {
		t.Fatalf("expected redirect action type, got %q", data.ActionType)
	}
	if data.URL != "https://mall.example.com/topup/10" {
		t.Fatalf("expected mall redirect url, got %q", data.URL)
	}
}

func TestGetPaymentSubscriptionMetaReturnsLegacyAutoMeta(t *testing.T) {
	routeSetting := operation_setting.GetPaymentRouteSetting()
	prevSubscriptionProvider := routeSetting.SubscriptionProvider
	routeSetting.SubscriptionProvider = types.PaymentProviderLegacyAuto
	defer func() {
		routeSetting.SubscriptionProvider = prevSubscriptionProvider
	}()

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/payment/subscription/meta", nil, 1)
	GetPaymentSubscriptionMeta(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var data paymentMetaResponse
	if err := common.Unmarshal(response.Data, &data); err != nil {
		t.Fatalf("failed to decode subscription meta response: %v", err)
	}
	if data.ProviderMeta.Scene != types.PaymentSceneSubscription {
		t.Fatalf("expected subscription scene, got %q", data.ProviderMeta.Scene)
	}
	if data.ProviderMeta.Provider != types.PaymentProviderLegacyAuto {
		t.Fatalf("expected legacy_auto provider, got %q", data.ProviderMeta.Provider)
	}
	if !data.ProviderMeta.LegacyAuto {
		t.Fatalf("expected legacy_auto metadata flag to be true")
	}
}

func TestCreatePaymentSubscriptionCheckoutRequiresPlanID(t *testing.T) {
	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/payment/subscription/checkout", map[string]any{}, 1)
	CreatePaymentSubscriptionCheckout(ctx)

	response := decodeAPIResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected subscription checkout to fail without plan_id")
	}
	if response.Message == "" {
		t.Fatalf("expected error message for missing plan_id")
	}
	if !strings.Contains(recorder.Body.String(), "success") {
		t.Fatalf("expected api response envelope, got: %s", recorder.Body.String())
	}
}
