package service

import (
	"fmt"
	"net/url"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
)

type EpayPaymentProvider struct{}

func init() {
	RegisterPaymentProvider(&EpayPaymentProvider{})
}

func (provider *EpayPaymentProvider) ID() types.PaymentProvider {
	return types.PaymentProviderEpay
}

func (provider *EpayPaymentProvider) DisplayName() string {
	return "Epay"
}

func (provider *EpayPaymentProvider) SupportsTopup() bool {
	return true
}

func (provider *EpayPaymentProvider) SupportsSubscription() bool {
	return true
}

func (provider *EpayPaymentProvider) GetTopupMeta() *types.ProviderMeta {
	channels, err := ListPublicEpayChannels(nil)
	return &types.ProviderMeta{
		Scene: types.PaymentSceneTopup, Provider: provider.ID(), DisplayName: provider.DisplayName(),
		ActionType: types.CheckoutActionTypeFormPost, RequiresAmount: true, RequiresChannel: true,
		Enabled: true, ConfigReady: err == nil && len(channels) > 0, EpayChannels: channels,
	}
}

func (provider *EpayPaymentProvider) GetSubscriptionMeta(plan *model.SubscriptionPlan) *types.ProviderMeta {
	channels, err := ListPublicEpayChannels(plan)
	return &types.ProviderMeta{
		Scene: types.PaymentSceneSubscription, Provider: provider.ID(), DisplayName: provider.DisplayName(),
		ActionType: types.CheckoutActionTypeFormPost, RequiresChannel: true,
		Enabled: plan != nil && plan.Enabled, ConfigReady: err == nil && len(channels) > 0, EpayChannels: channels,
	}
}

func (provider *EpayPaymentProvider) ValidateTopupConfig() error {
	channels, err := ListPublicEpayChannels(nil)
	if err != nil {
		return err
	}
	if len(channels) == 0 {
		return fmt.Errorf("%w: 请启用至少一个易支付渠道", ErrPaymentProviderNotConfigured)
	}
	return nil
}

func (provider *EpayPaymentProvider) ValidateSubscriptionConfig(plan *model.SubscriptionPlan) error {
	if plan == nil {
		return fmt.Errorf("%w: subscription plan", ErrPaymentProviderNotConfigured)
	}
	return provider.ValidateTopupConfig()
}

func validateEpayQuoteToken(quote *types.EpayQuote, token string) error {
	if !quote.Payable {
		return fmt.Errorf("%s", quote.UnavailableReason)
	}
	if token == "" || token != quote.Token {
		return fmt.Errorf("支付报价已变化或尚未确认，请重新获取报价后确认")
	}
	return nil
}

func (provider *EpayPaymentProvider) CreateTopupCheckout(request *types.TopupCheckoutRequest) (*types.CheckoutResult, error) {
	if request == nil || request.UserID <= 0 {
		return nil, fmt.Errorf("参数错误")
	}
	revision, err := GetEnabledEpayRevision(request.EpayChannelID, request.PaymentMethod)
	if err != nil {
		return nil, err
	}
	quote, err := CalculateEpayQuote(revision, request.PaymentMethod, types.PaymentSceneTopup, request.AmountCNY, nil)
	if err != nil {
		return nil, err
	}
	if err := validateEpayQuoteToken(quote, request.QuoteToken); err != nil {
		return nil, err
	}
	if _, err := model.GetUserById(request.UserID, false); err != nil {
		return nil, fmt.Errorf("用户不存在")
	}
	client, err := NewEpayClient(revision)
	if err != nil {
		return nil, err
	}
	notifyURL, err := epayNotifyURL(revision, types.PaymentSceneTopup)
	if err != nil {
		return nil, err
	}
	returnURL, err := buildTopupEpayReturnURL(request.ReturnURL)
	if err != nil {
		return nil, err
	}
	tradeNo := buildTopupTradeNo(request.UserID)
	payMoney, _ := epayDecimal(quote.PayCNY, 2)
	uri, fields, err := client.Purchase(buildEpayPurchaseArgs(request.PaymentMethod, tradeNo, revision.ProductNameTemplate, "TUC"+request.AmountCNY, payMoney, notifyURL, returnURL))
	if err != nil {
		return nil, fmt.Errorf("拉起支付失败")
	}
	snapshot, err := common.Marshal(quote)
	if err != nil {
		return nil, err
	}
	topup := &model.TopUp{
		UserId: request.UserID, Amount: int64(quote.CreditQuota), Money: payMoney.InexactFloat64(), TradeNo: tradeNo,
		PaymentMethod: request.PaymentMethod, PaymentProvider: model.PaymentProviderEpay,
		CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending,
		EpayChannelId: revision.ChannelId, EpayChannelRevisionId: revision.Id, EpayChannelName: revision.Name,
		EpayInputCNY: quote.InputCNY, EpayPayCNY: quote.PayCNY, EpayCreditUSD: quote.CreditUSD, EpayBonusUSD: quote.BonusUSD,
		EpayQuoteSnapshot: string(snapshot), BaseQuota: quote.BaseQuota, BonusQuota: quote.BonusQuota, CreditQuota: quote.CreditQuota,
	}
	formJSON, err := common.Marshal(types.CheckoutForm{URL: uri, Fields: fields})
	if err != nil {
		return nil, err
	}
	topup.EpayCheckoutFormJSON = string(formJSON)
	if err := model.CreateEpayTopupOrder(topup); err != nil {
		return nil, err
	}
	return createFormCheckoutResult(types.PaymentSceneTopup, provider.ID(), uri, fields), nil
}

func (provider *EpayPaymentProvider) CreateSubscriptionCheckout(plan *model.SubscriptionPlan, request *types.SubscriptionCheckoutRequest) (*types.CheckoutResult, error) {
	if request == nil || request.UserID <= 0 || plan == nil {
		return nil, fmt.Errorf("参数错误")
	}
	if err := ensureUserCanPurchaseSubscription(request.UserID, plan); err != nil {
		return nil, err
	}
	revision, err := GetEnabledEpayRevision(request.EpayChannelID, request.PaymentMethod)
	if err != nil {
		return nil, err
	}
	quote, err := CalculateEpayQuote(revision, request.PaymentMethod, types.PaymentSceneSubscription, "", plan)
	if err != nil {
		return nil, err
	}
	if err := validateEpayQuoteToken(quote, request.QuoteToken); err != nil {
		return nil, err
	}
	client, err := NewEpayClient(revision)
	if err != nil {
		return nil, err
	}
	notifyURL, err := epayNotifyURL(revision, types.PaymentSceneSubscription)
	if err != nil {
		return nil, err
	}
	returnURL, err := url.Parse(system_setting.ServerAddress + "/console/topup")
	if err != nil {
		return nil, err
	}
	if request.ReturnURL != "" {
		returnURL, err = url.Parse(request.ReturnURL)
		if err != nil {
			return nil, err
		}
	}
	tradeNo := buildSubscriptionEpayTradeNo(request.UserID)
	payMoney, _ := epayDecimal(quote.PayCNY, 2)
	uri, fields, err := client.Purchase(buildEpayPurchaseArgs(request.PaymentMethod, tradeNo, revision.ProductNameTemplate, "SUB:"+plan.Title, payMoney, notifyURL, returnURL))
	if err != nil {
		return nil, fmt.Errorf("拉起支付失败")
	}
	snapshot, err := common.Marshal(quote)
	if err != nil {
		return nil, err
	}
	order := &model.SubscriptionOrder{
		UserId: request.UserID, PlanId: plan.Id, Money: payMoney.InexactFloat64(), TradeNo: tradeNo,
		PaymentMethod: request.PaymentMethod, PaymentProvider: model.PaymentProviderEpay, CreateTime: common.GetTimestamp(), Status: common.TopUpStatusPending,
		EpayChannelId: revision.ChannelId, EpayChannelRevisionId: revision.Id, EpayChannelName: revision.Name,
		EpayPayCNY: quote.PayCNY, EpayQuoteSnapshot: string(snapshot),
	}
	formJSON, err := common.Marshal(types.CheckoutForm{URL: uri, Fields: fields})
	if err != nil {
		return nil, err
	}
	order.EpayCheckoutFormJSON = string(formJSON)
	if err := model.CreateEpaySubscriptionOrder(order); err != nil {
		return nil, err
	}
	return createFormCheckoutResult(types.PaymentSceneSubscription, provider.ID(), uri, fields), nil
}
