package common

import (
	"context"
	"sync/atomic"
)

type upstreamObservationKey struct{}

// One observer per channel attempt. Only actual HTTP responses may populate it;
// local errors, mapped statuses and HTTP-200 stream errors are not HTTP failures.
func WithUpstreamObservation(ctx context.Context) (context.Context, *atomic.Int32) {
	status := &atomic.Int32{}
	return context.WithValue(ctx, upstreamObservationKey{}, status), status
}

func ObserveUpstreamStatus(ctx context.Context, status int) {
	if value, ok := ctx.Value(upstreamObservationKey{}).(*atomic.Int32); ok {
		value.Store(int32(status))
	}
}
