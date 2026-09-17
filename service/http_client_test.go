package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestGetRelayHttpClientPreservesNonStreamTimeout(t *testing.T) {
	oldTimeout, oldClient := common.RelayTimeout, httpClient
	common.RelayTimeout = 7
	InitHttpClient()
	ResetProxyClientCache()
	t.Cleanup(func() {
		ResetProxyClientCache()
		httpClient.CloseIdleConnections()
		common.RelayTimeout, httpClient = oldTimeout, oldClient
	})

	for _, proxyURL := range []string{"", "http://localhost:8080", "https://localhost:8080", "socks5://localhost:1080", "socks5h://localhost:1080"} {
		t.Run(proxyURL, func(t *testing.T) {
			syncClient, err := GetRelayHttpClient(proxyURL, false)
			require.NoError(t, err)
			streamClient, err := GetRelayHttpClient(proxyURL, true)
			require.NoError(t, err)
			require.Zero(t, streamClient.Timeout)
			require.Same(t, syncClient.Transport, streamClient.Transport)
			require.NotNil(t, streamClient.CheckRedirect)
			// Selecting a streaming client must not disable the cached timeout.
			nextClient, err := GetRelayHttpClient(proxyURL, false)
			require.NoError(t, err)
			require.Same(t, syncClient, nextClient)
			require.Equal(t, 7*time.Second, nextClient.Timeout)
		})
	}

	_, err := GetRelayHttpClient("unsupported://localhost", true)
	require.Error(t, err)

	common.RelayTimeout = 0
	InitHttpClient()
	client, err := GetRelayHttpClient("", false)
	require.NoError(t, err)
	require.Zero(t, client.Timeout)
}
