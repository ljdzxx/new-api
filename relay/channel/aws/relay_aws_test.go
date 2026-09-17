package aws

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAwsRelayTimeoutOnlyNonStream(t *testing.T) {
	oldTimeout := common.RelayTimeout
	common.RelayTimeout = 7
	service.InitHttpClient()
	t.Cleanup(func() {
		service.GetHttpClient().CloseIdleConnections()
		common.RelayTimeout = oldTimeout
		service.InitHttpClient()
	})

	for _, isStream := range []bool{true, false} {
		ctx, cancel := newAwsInvokeContext(context.Background(), isStream)
		_, hasDeadline := ctx.Deadline()
		cancel()
		require.Equal(t, !isStream, hasDeadline)

		info := &relaycommon.RelayInfo{
			IsStream: isStream,
			ChannelMeta: &relaycommon.ChannelMeta{
				ApiKey:            "access-key|secret-key|us-east-1",
				UpstreamModelName: "claude-3-5-sonnet-20240620",
			},
		}
		client, err := newAwsClient(nil, info)
		require.NoError(t, err)
		httpClient, ok := client.Options().HTTPClient.(*http.Client)
		require.True(t, ok)
		require.Equal(t, isStream, httpClient.Timeout == 0)
	}

	common.RelayTimeout = 0
	ctx, cancel := newAwsInvokeContext(context.Background(), false)
	defer cancel()
	_, hasDeadline := ctx.Deadline()
	require.False(t, hasDeadline)
}

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}
