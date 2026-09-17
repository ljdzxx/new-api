package service

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// DoRelayHTTPRequest keeps upstream cancellation alive until the response body is
// consumed or closed. Both the downstream and any existing request context apply.
func DoRelayHTTPRequest(downstream context.Context, client *http.Client, req *http.Request, isStream bool) (*http.Response, error) {
	ctx, cancel := context.WithCancelCause(req.Context())
	stopDownstream := context.AfterFunc(downstream, func() { cancel(context.Cause(downstream)) })
	if downstream.Err() != nil {
		cancel(context.Cause(downstream))
	}
	cleanup := func() {
		stopDownstream()
		cancel(context.Canceled)
	}

	var headerTimer *time.Timer
	if isStream {
		headerTimer = time.AfterFunc(relaycommon.StreamingTimeout(), func() { cancel(context.DeadlineExceeded) })
		defer headerTimer.Stop()
	}
	resp, err := client.Do(req.WithContext(ctx))
	if headerTimer != nil {
		headerTimer.Stop()
	}
	if err == nil && ctx.Err() != nil {
		err = context.Cause(ctx)
	}
	if err != nil {
		cleanup()
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	}
	resp.Body = &relayResponseBody{ReadCloser: resp.Body, cancel: cleanup}
	return resp, nil
}

type relayResponseBody struct {
	io.ReadCloser
	cancel    func()
	closeOnce sync.Once
	closeErr  error
}

func (b *relayResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.cancel()
	}
	return n, err
}

func (b *relayResponseBody) Close() error {
	b.closeOnce.Do(func() {
		// Cancel before Close so blocked reads and HTTP/2 streams stop promptly.
		b.cancel()
		b.closeErr = b.ReadCloser.Close()
	})
	return b.closeErr
}
