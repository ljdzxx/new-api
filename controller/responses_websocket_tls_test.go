package controller

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestResponsesWebSocketTLSAfterHTTP2Request(t *testing.T) {
	protocols := make(chan string, 1)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/warm" {
			_, _ = io.WriteString(w, r.Proto)
			return
		}
		protocols <- r.TLS.NegotiatedProtocol
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		_ = ws.WriteMessage(websocket.TextMessage, []byte("tls_ws_ok"))
	}))
	upstream.EnableHTTP2 = true
	upstream.TLS = &tls.Config{NextProtos: []string{"h2", "http/1.1"}}
	upstream.StartTLS()
	defer upstream.Close()
	transport := upstream.Client().Transport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = true
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	response, err := client.Get(upstream.URL + "/warm")
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 2, response.ProtoMajor, "ordinary HTTP must retain HTTP/2")
	require.Contains(t, transport.TLSClientConfig.NextProtos, "h2")
	originalProtos := append([]string(nil), transport.TLSClientConfig.NextProtos...)
	dialer := newResponsesWSDialer(client)
	ws, _, err := dialer.DialContext(context.Background(), "wss"+strings.TrimPrefix(upstream.URL, "https")+"/v1/responses", nil)
	require.NoError(t, err, "WS must not negotiate h2 from the shared HTTP transport")
	defer ws.Close()
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, message, err := ws.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "tls_ws_ok", string(message))
	require.Equal(t, "http/1.1", <-protocols)
	require.Equal(t, originalProtos, transport.TLSClientConfig.NextProtos, "shared transport must remain unchanged")
	require.NotSame(t, transport.TLSClientConfig, dialer.TLSClientConfig)
}

func TestResponsesWebSocketDialerPreservesProxyAndTLS(t *testing.T) {
	proxyURL, err := url.Parse("http://proxy.invalid:8080")
	require.NoError(t, err)
	dialCalled := false
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			dialCalled = true
			return nil, context.Canceled
		},
		TLSClientConfig: &tls.Config{ServerName: "upstream.example", MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1"}},
	}
	dialer := newResponsesWSDialer(&http.Client{Transport: transport})
	proxy, err := dialer.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}})
	require.NoError(t, err)
	require.Equal(t, proxyURL, proxy)
	_, err = dialer.NetDialContext(context.Background(), "tcp", "unused:443")
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, dialCalled)
	require.Equal(t, "upstream.example", dialer.TLSClientConfig.ServerName)
	require.Equal(t, uint16(tls.VersionTLS12), dialer.TLSClientConfig.MinVersion)
	require.Equal(t, []string{"http/1.1"}, dialer.TLSClientConfig.NextProtos)
	require.Equal(t, []string{"h2", "http/1.1"}, transport.TLSClientConfig.NextProtos)
	require.NotSame(t, transport.TLSClientConfig, dialer.TLSClientConfig)
	for _, client := range []*http.Client{nil, {}, {Transport: (*http.Transport)(nil)}, {Transport: &http.Transport{}}} {
		d := newResponsesWSDialer(client)
		require.NotNil(t, d.TLSClientConfig)
		require.Equal(t, []string{"http/1.1"}, d.TLSClientConfig.NextProtos)
	}
}
