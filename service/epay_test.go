package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPaymentReturnURLUsesRequestOrigin(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://pay.example.com/api/payment/topup/checkout", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "https://pay.example.com")

	returnURL, err := BuildPaymentReturnURL(req, "/console/log")
	require.NoError(t, err)
	require.Equal(t, "https://pay.example.com/console/log", returnURL)
}

func TestBuildPaymentReturnURLFallsBackToForwardedHost(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://internal/api/payment/topup/checkout", nil)
	require.NoError(t, err)
	req.Host = "internal"
	req.Header.Set("X-Forwarded-Host", "a.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	returnURL, err := BuildPaymentReturnURL(req, "/console/log")
	require.NoError(t, err)
	require.Equal(t, "https://a.example.com/console/log", returnURL)
}
