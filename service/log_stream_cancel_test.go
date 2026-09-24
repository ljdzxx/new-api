package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGenerateTextOtherInfoMarksDownstreamStreamCancellation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		stream        bool
		reason        relaycommon.StreamEndReason
		missingStatus bool
		cancelContext bool
		wantCanceled  bool
	}{
		{name: "downstream cancellation", stream: true, reason: relaycommon.StreamEndReasonClientGone, cancelContext: true, wantCanceled: true},
		{name: "downstream write failure", stream: true, reason: relaycommon.StreamEndReasonClientGone, wantCanceled: true},
		{name: "completed stream", stream: true, reason: relaycommon.StreamEndReasonDone},
		{name: "context canceled after completion", stream: true, reason: relaycommon.StreamEndReasonDone, cancelContext: true},
		{name: "upstream timeout", stream: true, reason: relaycommon.StreamEndReasonTimeout},
		{name: "upstream read failure", stream: true, reason: relaycommon.StreamEndReasonScannerErr},
		{name: "upstream EOF", stream: true, reason: relaycommon.StreamEndReasonEOF},
		{name: "handler stopped", stream: true, reason: relaycommon.StreamEndReasonHandlerStop},
		{name: "ping failure", stream: true, reason: relaycommon.StreamEndReasonPingFail},
		{name: "missing stream status", stream: true, missingStatus: true},
		{name: "non-streaming cancellation", reason: relaycommon.StreamEndReasonClientGone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			requestContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestContext)
			if tc.cancelContext {
				cancel()
			}
			common.SetContextKey(ctx, constant.ContextKeyResponsesLogBadges, []string{"R1"})
			info := &relaycommon.RelayInfo{
				IsStream:    tc.stream,
				ChannelMeta: &relaycommon.ChannelMeta{},
			}
			if !tc.missingStatus {
				info.StreamStatus = relaycommon.NewStreamStatus()
				info.StreamStatus.SetEndReason(tc.reason, nil)
			}

			other := GenerateTextOtherInfo(ctx, info, 1, 1, 1, 0, 1, 0, 1)

			if tc.wantCanceled {
				require.Equal(t, true, other["stream_canceled_by_downstream"])
			} else {
				require.NotContains(t, other, "stream_canceled_by_downstream")
			}
			require.Equal(t, []string{"R1"}, other["responses_badges"])
		})
	}
}
