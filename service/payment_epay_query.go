package service

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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

func QueryEpayOrder(ctx context.Context, outTradeNo string, tradeNo string) (*EpayOrderInfo, error) {
	if strings.TrimSpace(operation_setting.PayAddress) == "" ||
		strings.TrimSpace(operation_setting.EpayId) == "" ||
		strings.TrimSpace(operation_setting.EpayKey) == "" {
		return nil, fmt.Errorf("epay is not configured")
	}
	if strings.TrimSpace(outTradeNo) == "" && strings.TrimSpace(tradeNo) == "" {
		return nil, fmt.Errorf("epay order id is empty")
	}

	apiURL, err := buildEpayQueryURL(operation_setting.PayAddress)
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("pid", operation_setting.EpayId)
	if strings.TrimSpace(outTradeNo) != "" {
		form.Set("out_trade_no", strings.TrimSpace(outTradeNo))
	}
	if strings.TrimSpace(tradeNo) != "" {
		form.Set("trade_no", strings.TrimSpace(tradeNo))
	}
	form.Set("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	form.Set("sign_type", "MD5")
	form.Set("sign", makeEpayMD5Sign(form, operation_setting.EpayKey))

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
	if err := common.DecodeJson(resp.Body, &raw); err != nil {
		return nil, err
	}
	info := &EpayOrderInfo{
		Code:       intFromAny(raw["code"]),
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
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", v), "0"), ".")
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		var n int
		_, _ = fmt.Sscanf(strings.TrimSpace(v), "%d", &n)
		return n
	default:
		return 0
	}
}
