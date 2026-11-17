package types

type PaymentScene string

const (
	PaymentSceneTopup        PaymentScene = "topup"
	PaymentSceneSubscription PaymentScene = "subscription"
)

type PaymentProvider string

const (
	PaymentProviderDisabled   PaymentProvider = "disabled"
	PaymentProviderLegacyAuto PaymentProvider = "legacy_auto"
	PaymentProviderEpay       PaymentProvider = "epay"
	PaymentProviderStripe     PaymentProvider = "stripe"
	PaymentProviderCreem      PaymentProvider = "creem"
	PaymentProviderMall       PaymentProvider = "mall"
)

type CheckoutActionType string

const (
	CheckoutActionTypeRedirectURL CheckoutActionType = "redirect_url"
	CheckoutActionTypeFormPost    CheckoutActionType = "form_post"
	CheckoutActionTypeQRCode      CheckoutActionType = "qr_code"
)

type ProviderMeta struct {
	Scene                PaymentScene        `json:"scene"`
	Provider             PaymentProvider     `json:"provider"`
	DisplayName          string              `json:"display_name"`
	ActionType           CheckoutActionType  `json:"action_type"`
	RequiresAmount       bool                `json:"requires_amount"`
	RequiresChannel      bool                `json:"requires_channel"`
	RequiresProduct      bool                `json:"requires_product"`
	Enabled              bool                `json:"enabled"`
	LegacyAuto           bool                `json:"legacy_auto"`
	ConfigReady          bool                `json:"config_ready"`
	MissingKeys          []string            `json:"missing_keys,omitempty"`
	Warnings             []string            `json:"warnings,omitempty"`
	AvailableChannels    []map[string]string `json:"available_channels,omitempty"`
	RawProductConfigJSON string              `json:"raw_product_config_json,omitempty"`
	EpayChannels         []EpayChannelInfo   `json:"epay_channels,omitempty"`
}

type CheckoutForm struct {
	URL    string            `json:"url"`
	Fields map[string]string `json:"fields,omitempty"`
}

type CheckoutResult struct {
	Scene      PaymentScene       `json:"scene"`
	Provider   PaymentProvider    `json:"provider"`
	ActionType CheckoutActionType `json:"action_type"`
	URL        string             `json:"url,omitempty"`
	Form       *CheckoutForm      `json:"form,omitempty"`
	QRCodeURL  string             `json:"qr_code_url,omitempty"`
}

type TopupCheckoutRequest struct {
	EpayChannelID int     `json:"epay_channel_id,omitempty"`
	AmountCNY     string  `json:"amount_cny,omitempty"`
	QuoteToken    string  `json:"quote_token,omitempty"`
	UserID        int     `json:"-"`
	Amount        int64   `json:"amount,omitempty"`
	PaymentMethod string  `json:"payment_method,omitempty"`
	ProductID     string  `json:"product_id,omitempty"`
	SuccessURL    string  `json:"success_url,omitempty"`
	CancelURL     string  `json:"cancel_url,omitempty"`
	PayMoney      float64 `json:"pay_money,omitempty"`
	ReturnURL     string  `json:"-"`
}

type SubscriptionCheckoutRequest struct {
	EpayChannelID int    `json:"epay_channel_id,omitempty"`
	QuoteToken    string `json:"quote_token,omitempty"`
	UserID        int    `json:"-"`
	PlanID        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method,omitempty"`
	SuccessURL    string `json:"success_url,omitempty"`
	CancelURL     string `json:"cancel_url,omitempty"`
	ReturnURL     string `json:"-"`
}

const (
	EpayQueryAPISigned = "api_pay_query"
	EpayQueryAPIPHP    = "api_php"
)

type EpayMethodLimits struct {
	MinPayCNY     string `json:"min_pay_cny"`
	MaxPayCNY     string `json:"max_pay_cny"`
	DailyLimitCNY string `json:"daily_limit_cny"`
}

type EpayMethodInfo struct {
	EpayMethodLimits
	DailyRemainingCNY string     `json:"daily_remaining_cny"`
	DailyResetAt      int64      `json:"daily_reset_at"`
	SubscriptionQuote *EpayQuote `json:"subscription_quote,omitempty"`
}

type EpayChannelInfo struct {
	ID                            int                       `json:"id"`
	RevisionID                    int                       `json:"revision_id"`
	Name                          string                    `json:"name"`
	Description                   string                    `json:"description"`
	PaymentMethods                []string                  `json:"payment_methods"`
	MethodLimits                  map[string]EpayMethodInfo `json:"payment_method_limits"`
	TopupOfferType                string                    `json:"topup_offer_type"`
	TopupOfferPercent             string                    `json:"topup_offer_percent"`
	TopupOfferThresholdCNY        string                    `json:"topup_offer_threshold_cny"`
	SubscriptionDiscountPercent   string                    `json:"subscription_discount_percent"`
	SubscriptionOfferThresholdCNY string                    `json:"subscription_offer_threshold_cny"`
}

type EpayQuote struct {
	ChannelID         int          `json:"channel_id"`
	RevisionID        int          `json:"revision_id"`
	PaymentMethod     string       `json:"payment_method"`
	MinPayCNY         string       `json:"min_pay_cny"`
	MaxPayCNY         string       `json:"max_pay_cny"`
	DailyLimitCNY     string       `json:"daily_limit_cny"`
	DailyRemainingCNY string       `json:"daily_remaining_cny"`
	DailyResetAt      int64        `json:"daily_reset_at"`
	Scene             PaymentScene `json:"scene"`
	PlanID            int          `json:"plan_id"`
	InputCNY          string       `json:"input_cny"`
	PayCNY            string       `json:"pay_cny"`
	BaseUSD           string       `json:"base_usd"`
	BonusUSD          string       `json:"bonus_usd"`
	CreditUSD         string       `json:"credit_usd"`
	USDExchangeRate   string       `json:"usd_exchange_rate"`
	OfferType         string       `json:"offer_type"`
	OfferPercent      string       `json:"offer_percent"`
	OfferApplied      bool         `json:"offer_applied"`
	BaseQuota         int          `json:"-"`
	BonusQuota        int          `json:"-"`
	CreditQuota       int          `json:"-"`
	Payable           bool         `json:"payable"`
	UnavailableReason string       `json:"unavailable_reason"`
	Token             string       `json:"quote_token"`
}
