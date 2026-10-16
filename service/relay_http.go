package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// UpstreamResponseHeaderTimeout distinguishes an upstream that never sent
// headers from a downstream disconnect or a failure while reading SSE frames.
type UpstreamResponseHeaderTimeout struct{ After time.Duration }

func (e *UpstreamResponseHeaderTimeout) Error() string {
	return fmt.Sprintf("upstream response headers timed out after %s", e.After)
}

func (e *UpstreamResponseHeaderTimeout) Unwrap() error { return context.DeadlineExceeded }

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
		timeout := relaycommon.StreamingTimeout()
		headerTimer = time.AfterFunc(timeout, func() { cancel(&UpstreamResponseHeaderTimeout{After: timeout}) })
		defer headerTimer.Stop()
	}
	common.ObserveUpstreamStatus(downstream, 0)
	resp, err := client.Do(req.WithContext(ctx))
	if resp != nil && err == nil {
		common.ObserveUpstreamStatus(downstream, resp.StatusCode)
	}
	if headerTimer != nil {
		headerTimer.Stop()
	}
	if err == nil && ctx.Err() != nil {
		err = context.Cause(ctx)
	}
	if err != nil {
		// HTTP/2 may return context.Canceled instead of the cancellation cause.
		// Preserve our header deadline classification before cleanup cancels ctx.
		var headerTimeout *UpstreamResponseHeaderTimeout
		if errors.As(context.Cause(ctx), &headerTimeout) {
			err = headerTimeout
		}
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
