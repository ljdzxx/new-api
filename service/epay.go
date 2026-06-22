package service

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func GetCallbackAddress() string {
	if operation_setting.CustomCallbackAddress == "" {
		return system_setting.ServerAddress
	}
	return operation_setting.CustomCallbackAddress
}

func BuildPaymentReturnURL(r *http.Request, returnPath string) (string, error) {
	if !strings.HasPrefix(returnPath, "/") {
		return "", fmt.Errorf("payment return path must start with /")
	}
	origin, err := getPaymentRequestOrigin(r)
	if err != nil {
		return "", err
	}
	return origin + returnPath, nil
}

func getPaymentRequestOrigin(r *http.Request) (string, error) {
	if r == nil {
		return "", fmt.Errorf("payment request is nil")
	}
	if origin := trustedHeaderOrigin(r, r.Header.Get("Origin")); origin != "" {
		return origin, nil
	}
	if referer := r.Header.Get("Referer"); referer != "" {
		if parsed, err := url.Parse(referer); err == nil {
			if origin := trustedHeaderOrigin(r, parsed.Scheme+"://"+parsed.Host); origin != "" {
				return origin, nil
			}
		}
	}

	host := firstForwardedValue(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return "", fmt.Errorf("payment request host is empty")
	}
	scheme := detectPaymentRequestScheme(r)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("payment request scheme is invalid")
	}
	return scheme + "://" + host, nil
}

func trustedHeaderOrigin(r *http.Request, rawOrigin string) string {
	rawOrigin = strings.TrimSpace(rawOrigin)
	if rawOrigin == "" || rawOrigin == "null" {
		return ""
	}
	parsed, err := url.Parse(rawOrigin)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}

	requestHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host"))
	if requestHost == "" {
		requestHost = strings.TrimSpace(r.Host)
	}
	if sameHostname(parsed.Host, requestHost) {
		return parsed.Scheme + "://" + parsed.Host
	}
	if common.ValidateRedirectURL(parsed.Scheme+"://"+parsed.Host) == nil {
		return parsed.Scheme + "://" + parsed.Host
	}
	return ""
}

func sameHostname(a string, b string) bool {
	a = strings.ToLower(stripPort(strings.TrimSpace(a)))
	b = strings.ToLower(stripPort(strings.TrimSpace(b)))
	return a != "" && a == b
}

func stripPort(host string) string {
	if strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			return h
		}
	}
	return host
}

func firstForwardedValue(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ",")
	return strings.TrimSpace(parts[0])
}

func detectPaymentRequestScheme(r *http.Request) string {
	if proto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		return strings.ToLower(proto)
	}
	if proto := firstForwardedValue(r.Header.Get("X-Forwarded-Protocol")); proto != "" {
		return strings.ToLower(proto)
	}
	if r.TLS != nil {
		return "https"
	}
	if r.URL != nil && r.URL.Scheme != "" {
		return strings.ToLower(r.URL.Scheme)
	}
	return "http"
}
