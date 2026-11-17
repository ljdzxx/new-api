package service

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
)

type EpayOrderInfo struct {
	QueryAPI   string
	Code       int
	Msg        string
	TradeNo    string
	OutTradeNo string
	Type       string
	Pid        string
	Money      string
	Status     string
}

func (info *EpayOrderInfo) IsSuccess() bool {
	if info == nil {
		return false
	}
	switch info.QueryAPI {
	case "", types.EpayQueryAPISigned:
		return info.Code == 0
	case types.EpayQueryAPIPHP:
		return info.Code == 1
	default:
		return false
	}
}

func normalizeEpayQueryAPI(queryAPI string) (string, error) {
	switch queryAPI {
	case "", types.EpayQueryAPISigned:
		return types.EpayQueryAPISigned, nil
	case types.EpayQueryAPIPHP:
		return types.EpayQueryAPIPHP, nil
	default:
		return "", fmt.Errorf("查单接口类型无效")
	}
}

func QueryEpayOrder(ctx context.Context, revision *model.EpayChannelRevision, outTradeNo string, tradeNo string) (*EpayOrderInfo, error) {
	if revision == nil || revision.PayAddress == "" || revision.MerchantId == "" || revision.MerchantKey == "" {
		return nil, fmt.Errorf("epay is not configured")
	}
	if strings.TrimSpace(outTradeNo) == "" && strings.TrimSpace(tradeNo) == "" {
		return nil, fmt.Errorf("epay order id is empty")
	}

	queryAPI, err := normalizeEpayQueryAPI(revision.QueryAPI)
	if err != nil {
		return nil, err
	}
	apiURL, err := buildEpayQueryURL(revision.PayAddress, queryAPI)
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("pid", revision.MerchantId)
	if strings.TrimSpace(outTradeNo) != "" {
		form.Set("out_trade_no", strings.TrimSpace(outTradeNo))
	}
	if strings.TrimSpace(tradeNo) != "" {
		form.Set("trade_no", strings.TrimSpace(tradeNo))
	}
	method := http.MethodPost
	var body io.Reader
	if queryAPI == types.EpayQueryAPIPHP {
		method = http.MethodGet
		form.Set("act", "order")
		form.Set("key", revision.MerchantKey)
		if form.Get("trade_no") != "" {
			form.Del("out_trade_no")
		}
		apiURL += "?" + form.Encode()
	} else {
		form.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
		form.Set("sign_type", "MD5")
		form.Set("sign", makeEpayMD5Sign(form, revision.MerchantKey))
		body = strings.NewReader(form.Encode())
	}

	reqCtx := ctx
	if reqCtx == nil {
		reqCtx = context.Background()
	}
	var cancel context.CancelFunc
	reqCtx, cancel = context.WithTimeout(reqCtx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, method, apiURL, body)
	if err != nil {
		return nil, epayQueryError(err, revision.MerchantKey)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := *http.DefaultClient
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, epayQueryError(err, revision.MerchantKey)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("epay order query returned http %d", resp.StatusCode)
	}

	var raw map[string]any
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1048577))
	if err != nil {
		return nil, epayQueryError(err, revision.MerchantKey)
	}
	if len(payload) > 1048576 {
		return nil, fmt.Errorf("epay order query response is too large")
	}
	if err := common.UnmarshalUseNumber(payload, &raw); err != nil {
		return nil, epayQueryError(err, revision.MerchantKey)
	}
	code, err := strconv.Atoi(stringFromAny(raw["code"]))
	if err != nil {
		return nil, fmt.Errorf("epay order query response has an invalid code")
	}
	message := stringFromAny(raw["msg"])
	if message == "" {
		message = stringFromAny(raw["message"])
	}
	info := &EpayOrderInfo{
		QueryAPI:   queryAPI,
		Code:       code,
		Msg:        redactEpayQuerySecret(message, revision.MerchantKey),
		TradeNo:    stringFromAny(raw["trade_no"]),
		OutTradeNo: stringFromAny(raw["out_trade_no"]),
		Type:       stringFromAny(raw["type"]),
		Pid:        stringFromAny(raw["pid"]),
		Money:      stringFromAny(raw["money"]),
		Status:     stringFromAny(raw["status"]),
	}
	return info, nil
}

func buildEpayQueryURL(payAddress, queryAPI string) (string, error) {
	queryAPI, err := normalizeEpayQueryAPI(queryAPI)
	if err != nil {
		return "", err
	}
	baseURL, err := url.Parse(strings.TrimSpace(payAddress))
	if err != nil {
		return "", err
	}
	if baseURL.Host == "" || (baseURL.Scheme != "https" && baseURL.Scheme != "http") || baseURL.User != nil {
		return "", fmt.Errorf("epay query address must be an HTTP(S) base URL without credentials")
	}
	path := "/api/pay/query"
	if queryAPI == types.EpayQueryAPIPHP {
		path = "/api.php"
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + path
	baseURL.RawPath = ""
	baseURL.RawQuery = ""
	baseURL.ForceQuery = false
	baseURL.Fragment = ""
	return baseURL.String(), nil
}

func epayQueryError(err error, merchantKey string) error {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		err = urlError.Err
	}
	return errors.New(redactEpayQuerySecret(err.Error(), merchantKey))
}

func redactEpayQuerySecret(value, merchantKey string) string {
	if merchantKey == "" {
		return value
	}
	for _, secret := range []string{url.QueryEscape(merchantKey), url.PathEscape(merchantKey), merchantKey} {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}

func makeEpayMD5Sign(values url.Values, key string) string {
	names := make([]string, 0, len(values))
	for name := range values {
		if name == "sign" || name == "sign_type" || strings.TrimSpace(values.Get(name)) == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+values.Get(name))
	}
	hash := md5.Sum([]byte(strings.Join(parts, "&") + key))
	return fmt.Sprintf("%x", hash)
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}
