package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Calcium-Ion/go-epay/epay"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/thanhpk/randstr"
)

type creemProduct struct {
	ProductID string  `json:"productId"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	Quota     int64   `json:"quota"`
}

type creemCheckoutRequest struct {
	ProductID string `json:"product_id"`
	RequestID string `json:"request_id"`
	Customer  struct {
		Email string `json:"email"`
	} `json:"customer"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type creemCheckoutResponse struct {
	CheckoutURL string `json:"checkout_url"`
	ID          string `json:"id"`
}

func newEpayClient() (*epay.Client, error) {
	if operation_setting.PayAddress == "" || operation_setting.EpayId == "" || operation_setting.EpayKey == "" {
		return nil, fmt.Errorf("当前管理员未配置支付信息")
	}
	client, err := epay.NewClient(&epay.Config{
		PartnerID: operation_setting.EpayId,
		Key:       operation_setting.EpayKey,
	}, operation_setting.PayAddress)
	if err != nil {
		return nil, fmt.Errorf("当前管理员未配置支付信息")
	}
	return client, nil
}

func getTopupPayMoney(amount int64, group string) float64 {
	dAmount := decimal.NewFromInt(amount)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		dAmount = dAmount.Div(dQuotaPerUnit)
	}

	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}

	dTopupGroupRatio := decimal.NewFromFloat(topupGroupRatio)
	dPrice := decimal.NewFromFloat(operation_setting.Price)
	discount := 1.0
	if ds, ok := operation_setting.GetPaymentSetting().AmountDiscount[int(amount)]; ok && ds > 0 {
		discount = ds
	}
	dDiscount := decimal.NewFromFloat(discount)

	payMoney := dAmount.Mul(dPrice).Mul(dTopupGroupRatio).Mul(dDiscount)
	return payMoney.InexactFloat64()
}

func getTopupMinAmount() int64 {
	minTopup := operation_setting.MinTopUp
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		dMinTopup := decimal.NewFromInt(int64(minTopup))
		dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		minTopup = int(dMinTopup.Mul(dQuotaPerUnit).IntPart())
	}
	return int64(minTopup)
}

func normalizeTopupStoredAmount(amount int64) int64 {
	if operation_setting.GetQuotaDisplayType() != operation_setting.QuotaDisplayTypeTokens {
		return amount
	}
	dAmount := decimal.NewFromInt(amount)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	return dAmount.Div(dQuotaPerUnit).IntPart()
}

func getStripeChargedAmount(count float64, user model.User) float64 {
	topupGroupRatio := common.GetTopupGroupRatio(user.Group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}
	return count * topupGroupRatio
}

func getStripeMinTopupAmount() int64 {
	minTopup := setting.StripeMinTopUp
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		minTopup = minTopup * int(common.QuotaPerUnit)
	}
	return int64(minTopup)
}

func buildStripeTopupLink(referenceID, customerID, email string, amount int64, successURL, cancelURL string) (string, error) {
	if !strings.HasPrefix(setting.StripeApiSecret, "sk_") && !strings.HasPrefix(setting.StripeApiSecret, "rk_") {
		return "", fmt.Errorf("Stripe 未配置或密钥无效")
	}

	stripe.Key = setting.StripeApiSecret
	if successURL == "" {
		successURL = system_setting.ServerAddress + "/console/log"
	}
	if cancelURL == "" {
		cancelURL = system_setting.ServerAddress + "/console/topup"
	}

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(referenceID),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(setting.StripePriceId),
				Quantity: stripe.Int64(amount),
			},
		},
		Mode:                stripe.String(string(stripe.CheckoutSessionModePayment)),
		AllowPromotionCodes: stripe.Bool(setting.StripePromotionCodesEnabled),
	}

	if customerID == "" {
		if email != "" {
			params.CustomerEmail = stripe.String(email)
		}
		params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
	} else {
		params.Customer = stripe.String(customerID)
	}

	result, err := session.New(params)
	if err != nil {
		return "", fmt.Errorf("拉起支付失败")
	}
	return result.URL, nil
}

func buildStripeSubscriptionLink(referenceID, customerID, email, priceID string) (string, error) {
	stripe.Key = setting.StripeApiSecret

	params := &stripe.CheckoutSessionParams{
		ClientReferenceID: stripe.String(referenceID),
		SuccessURL:        stripe.String(system_setting.ServerAddress + "/console/topup"),
		CancelURL:         stripe.String(system_setting.ServerAddress + "/console/topup"),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
	}

	if customerID == "" {
		if email != "" {
			params.CustomerEmail = stripe.String(email)
		}
		params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
	} else {
		params.Customer = stripe.String(customerID)
	}

	result, err := session.New(params)
	if err != nil {
		return "", fmt.Errorf("拉起支付失败")
	}
	return result.URL, nil
}

func parseCreemProducts() ([]creemProduct, error) {
	var products []creemProduct
	if err := common.UnmarshalJsonStr(setting.CreemProducts, &products); err != nil {
		return nil, fmt.Errorf("产品配置错误")
	}
	return products, nil
}

func findCreemProduct(productID string) (*creemProduct, error) {
	products, err := parseCreemProducts()
	if err != nil {
		return nil, err
	}
	for _, product := range products {
		if product.ProductID == productID {
			productCopy := product
			return &productCopy, nil
		}
	}
	return nil, fmt.Errorf("产品不存在")
}

func buildCreemCheckoutLink(referenceID string, product *creemProduct, email, username string) (string, error) {
	if setting.CreemApiKey == "" {
		return "", fmt.Errorf("未配置 Creem API 密钥")
	}

	apiURL := "https://api.creem.io/v1/checkouts"
	if setting.CreemTestMode {
		apiURL = "https://test-api.creem.io/v1/checkouts"
	}

	requestData := creemCheckoutRequest{
		ProductID: product.ProductID,
		RequestID: referenceID,
		Metadata: map[string]string{
			"username":     username,
			"reference_id": referenceID,
			"product_name": product.Name,
			"quota":        fmt.Sprintf("%d", product.Quota),
		},
	}
	requestData.Customer.Email = email

	jsonData, err := common.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("产品配置错误")
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("拉起支付失败")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", setting.CreemApiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("拉起支付失败")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("拉起支付失败")
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("拉起支付失败")
	}

	var checkoutResp creemCheckoutResponse
	if err := common.Unmarshal(body, &checkoutResp); err != nil {
		return "", fmt.Errorf("拉起支付失败")
	}
	if checkoutResp.CheckoutURL == "" {
		return "", fmt.Errorf("拉起支付失败")
	}
	return checkoutResp.CheckoutURL, nil
}

func buildSubscriptionCreemProduct(plan *model.SubscriptionPlan) *creemProduct {
	currency := "USD"
	switch operation_setting.GetGeneralSetting().QuotaDisplayType {
	case operation_setting.QuotaDisplayTypeCNY:
		currency = "CNY"
	case operation_setting.QuotaDisplayTypeUSD:
		currency = "USD"
	default:
		currency = "USD"
	}
	return &creemProduct{
		ProductID: plan.CreemProductId,
		Name:      plan.Title,
		Price:     plan.PriceAmount,
		Currency:  currency,
		Quota:     0,
	}
}

func ensureUserCanPurchaseSubscription(userID int, plan *model.SubscriptionPlan) error {
	if plan == nil {
		return fmt.Errorf("套餐不存在")
	}
	if !plan.Enabled {
		return fmt.Errorf("套餐未启用")
	}
	if plan.MaxPurchasePerUser <= 0 {
		return nil
	}
	count, err := model.CountUserSubscriptionsByPlan(userID, plan.Id)
	if err != nil {
		return err
	}
	if count >= int64(plan.MaxPurchasePerUser) {
		return fmt.Errorf("已达到该套餐购买上限")
	}
	return nil
}

func buildTopupTradeNo(userID int) string {
	tradeNo := fmt.Sprintf("%s%d", common.GetRandomString(6), time.Now().Unix())
	return fmt.Sprintf("USR%dNO%s", userID, tradeNo)
}

func buildSubscriptionEpayTradeNo(userID int) string {
	tradeNo := fmt.Sprintf("%s%d", common.GetRandomString(6), time.Now().Unix())
	return fmt.Sprintf("SUBUSR%dNO%s", userID, tradeNo)
}

func buildStripeTopupReference(userID int) string {
	reference := fmt.Sprintf("new-api-ref-%d-%d-%s", userID, time.Now().UnixMilli(), randstr.String(4))
	return "ref_" + common.Sha1([]byte(reference))
}

func buildStripeSubscriptionReference(userID int) string {
	reference := fmt.Sprintf("sub-stripe-ref-%d-%d-%s", userID, time.Now().UnixMilli(), randstr.String(4))
	return "sub_ref_" + common.Sha1([]byte(reference))
}

func buildCreemTopupReference(userID int) string {
	reference := fmt.Sprintf("creem-api-ref-%d-%d-%s", userID, time.Now().UnixMilli(), randstr.String(4))
	return "ref_" + common.Sha1([]byte(reference))
}

func buildCreemSubscriptionReference(user *model.User) string {
	reference := "sub-creem-ref-" + randstr.String(6)
	return "sub_ref_" + common.Sha1([]byte(reference+time.Now().String()+user.Username))
}

func buildTopupEpayReturnURL() (*url.URL, error) {
	return url.Parse(system_setting.ServerAddress + "/console/log")
}

func buildTopupEpayNotifyURL() (*url.URL, error) {
	return url.Parse(GetCallbackAddress() + "/api/user/epay/notify")
}

func buildSubscriptionEpayReturnURL() (*url.URL, error) {
	return url.Parse(GetCallbackAddress() + "/api/subscription/epay/return")
}

func buildSubscriptionEpayNotifyURL() (*url.URL, error) {
	return url.Parse(GetCallbackAddress() + "/api/subscription/epay/notify")
}

func createFormCheckoutResult(scene types.PaymentScene, provider types.PaymentProvider, formURL string, fields map[string]string) *types.CheckoutResult {
	return &types.CheckoutResult{
		Scene:      scene,
		Provider:   provider,
		ActionType: types.CheckoutActionTypeFormPost,
		Form: &types.CheckoutForm{
			URL:    formURL,
			Fields: fields,
		},
	}
}

func createRedirectCheckoutResult(scene types.PaymentScene, provider types.PaymentProvider, url string) *types.CheckoutResult {
	return &types.CheckoutResult{
		Scene:      scene,
		Provider:   provider,
		ActionType: types.CheckoutActionTypeRedirectURL,
		URL:        url,
	}
}

func stringifyMap(input map[string]string) map[string]string {
	if input == nil {
		return map[string]string{}
	}
	output := make(map[string]string, len(input))
	for k, v := range input {
		output[k] = v
	}
	return output
}

func buildMallTopupLink(amount int64) (string, error) {
	link, ok := operation_setting.GetPaymentSetting().MallLinks[int(amount)]
	if !ok || strings.TrimSpace(link) == "" {
		return "", fmt.Errorf("未找到该充值金额对应的商品链接")
	}
	parsed, err := url.Parse(link)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("商城跳转链接无效")
	}
	return link, nil
}

func buildTopupPurchaseArgs(paymentMethod, tradeNo string, amount int64, payMoney float64, notifyURL, returnURL *url.URL) *epay.PurchaseArgs {
	return &epay.PurchaseArgs{
		Type:           paymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("TUC%d", amount),
		Money:          strconv.FormatFloat(payMoney, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyURL,
		ReturnUrl:      returnURL,
	}
}

func buildSubscriptionPurchaseArgs(paymentMethod, tradeNo, title string, price float64, notifyURL, returnURL *url.URL) *epay.PurchaseArgs {
	return &epay.PurchaseArgs{
		Type:           paymentMethod,
		ServiceTradeNo: tradeNo,
		Name:           fmt.Sprintf("SUB:%s", title),
		Money:          strconv.FormatFloat(price, 'f', 2, 64),
		Device:         epay.PC,
		NotifyUrl:      notifyURL,
		ReturnUrl:      returnURL,
	}
}
