package aws

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	awsSDK "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAwsCancellationReachesUpstream(t *testing.T) {
	oldRelay, oldStream := common.RelayTimeout, constant.StreamingTimeout
	common.RelayTimeout, constant.StreamingTimeout = 1, 1
	service.InitHttpClient()
	t.Cleanup(func() {
		service.GetHttpClient().CloseIdleConnections()
		common.RelayTimeout, constant.StreamingTimeout = oldRelay, oldStream
		service.InitHttpClient()
	})
	for _, tc := range []struct {
		name                              string
		sync, headers, disconnect, events bool
	}{
		{name: "stream_header_timeout"},
		{name: "stream_idle_timeout", headers: true},
		{name: "stream_idle_timer_resets_and_preserves_usage", headers: true, events: true},
		{name: "stream_disconnect_before_headers", disconnect: true},
		{name: "stream_disconnect_during_body", headers: true, disconnect: true},
		{name: "sync_header_timeout", sync: true},
		{name: "sync_body_timeout", sync: true, headers: true},
		{name: "sync_disconnect", sync: true, disconnect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started, canceled := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
				if tc.sync {
					w.Header().Set("Content-Type", "application/json")
				}
				if tc.headers {
					w.(http.Flusher).Flush()
				}
				close(started)
				if tc.events {
					encoder := eventstream.NewEncoder()
					ticker := time.NewTicker(300 * time.Millisecond)
					defer ticker.Stop()
					for _, data := range []string{
						`{"type":"message_start","message":{"id":"msg_test","usage":{"input_tokens":12}}}`,
						`{"type":"ping"}`,
						`{"type":"ping"}`,
						`{"type":"ping"}`,
						`{"type":"message_delta","usage":{"output_tokens":3}}`,
					} {
						select {
						case <-r.Context().Done():
							close(canceled)
							return
						case <-ticker.C:
						}
						payload, _ := common.Marshal(struct {
							Bytes []byte `json:"bytes"`
						}{[]byte(data)})
						err := encoder.Encode(w, eventstream.Message{
							Headers: eventstream.Headers{
								{Name: ":message-type", Value: eventstream.StringValue("event")},
								{Name: ":event-type", Value: eventstream.StringValue("chunk")},
								{Name: ":content-type", Value: eventstream.StringValue("application/json")},
							},
							Payload: payload,
						})
						if err != nil {
							t.Errorf("encode SDK event: %v", err)
							return
						}
						w.(http.Flusher).Flush()
					}
				}
				<-r.Context().Done()
				close(canceled)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
			client, err := service.GetRelayHttpClient("", !tc.sync)
			require.NoError(t, err)
			a := &Adaptor{AwsClient: bedrockruntime.New(bedrockruntime.Options{
				Region: "us-east-1", BaseEndpoint: awsSDK.String(server.URL),
				Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
				HTTPClient:  client,
			})}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, IsStream: !tc.sync, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}
			if tc.sync {
				a.AwsReq = &bedrockruntime.InvokeModelInput{ModelId: awsSDK.String("test-model"), Body: []byte("{}")}
			} else {
				a.AwsReq = &bedrockruntime.InvokeModelWithResponseStreamInput{ModelId: awsSDK.String("test-model"), Body: []byte("{}")}
			}
			type result struct {
				err   *types.NewAPIError
				usage *dto.Usage
			}
			done := make(chan result, 1)
			go func() {
				if tc.sync {
					err, usage := awsHandler(c, info, a)
					done <- result{err, usage}
				} else {
					err, usage := awsStreamHandler(c, info, a)
					done <- result{err, usage}
				}
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("SDK did not call upstream")
			}
			if tc.disconnect {
				cancel()
			}
			select {
			case result := <-done:
				if tc.sync {
					require.NotNil(t, result.err)
				} else {
					reason := relaycommon.StreamEndReasonTimeout
					if tc.disconnect {
						reason = relaycommon.StreamEndReasonClientGone
					}
					require.Equal(t, reason, info.StreamStatus.EndReason)
					if !tc.headers {
						require.NotNil(t, result.err)
					}
					if tc.events {
						require.Nil(t, result.err)
						require.NotNil(t, result.usage)
						require.Equal(t, 12, result.usage.PromptTokens)
						require.Equal(t, 3, result.usage.CompletionTokens)
						require.Equal(t, 5, info.ReceivedResponseCount)
					}
				}
			case <-time.After(5 * time.Second):
				cancel()
				t.Fatal("AWS handler did not stop")
			}
			select {
			case <-canceled:
			case <-time.After(2 * time.Second):
				t.Fatal("AWS upstream did not observe cancellation")
			}
		})
	}
}
