package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIPassthroughServiceTier(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	original := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = original })
	service.InitHttpClient()
	t.Cleanup(func() { service.GetHttpClient().CloseIdleConnections() })

	type capturedRequest struct {
		body          []byte
		path          string
		contentLength int64
		err           error
	}
	captured := make(chan capturedRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		captured <- capturedRequest{body, r.URL.Path, r.ContentLength, err}
		// Stop after capturing the actual outbound body, before billing.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"captured service tier request","type":"invalid_request_error"}}`)
	}))
	defer upstream.Close()

	for _, endpoint := range []struct {
		path string
		mode int
	}{
		{"/v1/chat/completions", relayconstant.RelayModeChatCompletions},
		{"/v1/responses", relayconstant.RelayModeResponses},
		{"/v1/responses/compact", relayconstant.RelayModeResponsesCompact},
	} {
		for _, mode := range []struct {
			name    string
			global  bool
			channel bool
		}{
			{"channel", false, true},
			{"global", true, false},
			{"both", true, true},
		} {
			for _, allow := range []bool{false, true} {
				for _, mapped := range []bool{false, true} {
					for _, stream := range []bool{false, true} {
						if endpoint.mode == relayconstant.RelayModeResponsesCompact && stream {
							continue
						}
						t.Run(fmt.Sprintf("%s/%s/allow=%t/mapped=%t/stream=%t", endpoint.path, mode.name, allow, mapped, stream), func(t *testing.T) {
							settings.PassThroughRequestEnabled = mode.global
							body := fmt.Sprintf(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}],"input":"hello","stream":%t,"service_tier":"priority","store":false,"safety_identifier":"user-123","extension":{"large":18446744073709551615,"zero":0,"enabled":false}}`, stream)
							ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
							requestCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							ctx.Request = httptest.NewRequest(http.MethodPost, endpoint.path, strings.NewReader(body)).WithContext(requestCtx)
							ctx.Request.Header.Set("Content-Type", "application/json")
							defer common.CleanupBodyStorage(ctx)
							common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
							common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
							common.SetContextKey(ctx, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: mode.channel})
							common.SetContextKey(ctx, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AllowServiceTier: allow, DisableStore: true})
							if mapped {
								ctx.Set("model_mapping", `{"gpt-4o-mini":"gpt-4o"}`)
							}

							var request dto.Request
							switch endpoint.mode {
							case relayconstant.RelayModeChatCompletions:
								request = &dto.GeneralOpenAIRequest{}
							case relayconstant.RelayModeResponses:
								request = &dto.OpenAIResponsesRequest{}
							case relayconstant.RelayModeResponsesCompact:
								request = &dto.OpenAIResponsesCompactionRequest{}
							}
							require.NoError(t, common.Unmarshal([]byte(body), request))
							info := &relaycommon.RelayInfo{
								Request: request, OriginModelName: "gpt-4o-mini", RelayMode: endpoint.mode,
								RequestURLPath: endpoint.path, IsStream: stream, DisablePing: true,
								UpstreamRequestBodySize: int64(len(body)),
							}
							var apiErr *types.NewAPIError
							if endpoint.mode == relayconstant.RelayModeChatCompletions {
								info.RelayFormat = types.RelayFormatOpenAI
								apiErr = TextHelper(ctx, info)
							} else {
								info.RelayFormat = types.RelayFormatOpenAIResponses
								apiErr = ResponsesHelper(ctx, info)
							}
							require.NotNil(t, apiErr)
							require.Equal(t, http.StatusBadRequest, apiErr.StatusCode, apiErr.Error())
							require.Contains(t, apiErr.Error(), "captured service tier request")
							var got capturedRequest
							select {
							case got = <-captured:
							default:
								t.Fatal("request did not reach upstream")
							}
							require.NoError(t, got.err)
							require.Equal(t, endpoint.path, got.path)
							require.Equal(t, int64(len(got.body)), got.contentLength)
							var wantFields, gotFields map[string]json.RawMessage
							require.NoError(t, common.Unmarshal([]byte(body), &wantFields))
							require.NoError(t, common.Unmarshal(got.body, &gotFields))
							if !allow {
								delete(wantFields, "service_tier")
							}
							if mapped {
								wantFields["model"] = json.RawMessage(`"gpt-4o"`)
							}
							require.Equal(t, wantFields, gotFields)
							if allow && !mapped {
								require.Equal(t, body, string(got.body))
							}
							storage, err := common.GetBodyStorage(ctx)
							require.NoError(t, err)
							cachedBody, err := storage.Bytes()
							require.NoError(t, err)
							require.Equal(t, body, string(cachedBody))
						})
					}
				}
			}
		}
	}
}
