package service

import (
	"context"
	"crypto/md5"
	"encoding/json"
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
)

type EpayOrderInfo struct {
	Code       int
	Msg        string
	TradeNo    string
	OutTradeNo string
	Type       string
	Pid        string
	Money      string
	Status     string
}

func QueryEpayOrder(ctx context.Context, revision *model.EpayChannelRevision, outTradeNo string, tradeNo string) (*EpayOrderInfo, error) {
	if revision == nil || revision.PayAddress == "" || revision.MerchantId == "" || revision.MerchantKey == "" {
		return nil, fmt.Errorf("epay is not configured")
	}
	if strings.TrimSpace(outTradeNo) == "" && strings.TrimSpace(tradeNo) == "" {
		return nil, fmt.Errorf("epay order id is empty")
	}

	apiURL, err := buildEpayQueryURL(revision.PayAddress)
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
	form.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	form.Set("sign_type", "MD5")
	form.Set("sign", makeEpayMD5Sign(form, revision.MerchantKey))

	reqCtx := ctx
	if reqCtx == nil {
		reqCtx = context.Background()
	}
	var cancel context.CancelFunc
	reqCtx, cancel = context.WithTimeout(reqCtx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("epay order query returned http %d", resp.StatusCode)
	}

	var raw map[string]any
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1048577))
	if err != nil {
		return nil, err
	}
	if len(payload) > 1048576 {
		return nil, fmt.Errorf("epay order query response is too large")
	}
	if err := common.UnmarshalUseNumber(payload, &raw); err != nil {
		return nil, err
	}
	code, err := strconv.Atoi(stringFromAny(raw["code"]))
	if err != nil {
		return nil, fmt.Errorf("epay order query response has an invalid code")
	}
	info := &EpayOrderInfo{
		Code:       code,
		Msg:        stringFromAny(raw["msg"]),
		TradeNo:    stringFromAny(raw["trade_no"]),
		OutTradeNo: stringFromAny(raw["out_trade_no"]),
		Type:       stringFromAny(raw["type"]),
		Pid:        stringFromAny(raw["pid"]),
		Money:      stringFromAny(raw["money"]),
		Status:     stringFromAny(raw["status"]),
	}
	return info, nil
}

func buildEpayQueryURL(payAddress string) (string, error) {
	baseURL, err := url.Parse(strings.TrimSpace(payAddress))
	if err != nil {
		return "", err
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/api/pay/query"
	baseURL.RawQuery = ""
	baseURL.Fragment = ""
	return baseURL.String(), nil
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
