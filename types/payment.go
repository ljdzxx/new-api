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
	UserID        int     `json:"-"`
	Amount        int64   `json:"amount,omitempty"`
	PaymentMethod string  `json:"payment_method,omitempty"`
	ProductID     string  `json:"product_id,omitempty"`
	SuccessURL    string  `json:"success_url,omitempty"`
	CancelURL     string  `json:"cancel_url,omitempty"`
	PayMoney      float64 `json:"pay_money,omitempty"`
}

type SubscriptionCheckoutRequest struct {
	UserID        int    `json:"-"`
	PlanID        int    `json:"plan_id"`
	PaymentMethod string `json:"payment_method,omitempty"`
	SuccessURL    string `json:"success_url,omitempty"`
	CancelURL     string `json:"cancel_url,omitempty"`
}
