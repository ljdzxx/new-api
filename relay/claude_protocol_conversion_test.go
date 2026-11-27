package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	"github.com/tidwall/gjson"
)

type claudeCaptureTransport func(*http.Request) (*http.Response, error)

func (f claudeCaptureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClaudeProtocolConversionBypassesRawPassthrough(t *testing.T) {
	service.InitHttpClient()
	client := service.GetHttpClient()
	transport := client.Transport
	settings := model_setting.GetGlobalSettings()
	original := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		client.Transport = transport
		settings.PassThroughRequestEnabled = original
		client.CloseIdleConnections()
	})
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
		for _, global := range []bool{false, true} {
			t.Run(string(format)+"/global="+map[bool]string{false: "false", true: "true"}[global], func(t *testing.T) {
				settings.PassThroughRequestEnabled = global
				var captured []byte
				var path string
				client.Transport = claudeCaptureTransport(func(req *http.Request) (*http.Response, error) {
					var err error
					captured, err = io.ReadAll(req.Body)
					if err != nil {
						return nil, err
					}
					path = req.URL.Path
					return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"captured converted body"}}`)), Request: req}, nil
				})
				body := `{"model":"claude-test","input":"hello","max_output_tokens":1024,"stream":true}`
				url := "/v1/responses"
				mode := relayconstant.RelayModeResponses
				var request dto.Request = &dto.OpenAIResponsesRequest{}
				if format == types.RelayFormatGemini {
					body = `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":1024}}`
					url = "/v1beta/models/claude-test:streamGenerateContent"
					mode = relayconstant.RelayModeChatCompletions
					request = &dto.GeminiChatRequest{}
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				defer common.CleanupBodyStorage(c)
				common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAnthropic)
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://claude.invalid")
				common.SetContextKey(c, constant.ContextKeyOriginalModel, "claude-test")
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: !global})
				require.NoError(t, common.UnmarshalJsonStr(body, request))
				info := &relaycommon.RelayInfo{Request: request, RelayFormat: format, RelayMode: mode, OriginModelName: "claude-test", RequestURLPath: url, IsStream: true, DisablePing: true}
				var err *types.NewAPIError
				if format == types.RelayFormatGemini {
					err = GeminiHelper(c, info)
				} else {
					err = ResponsesHelper(c, info)
				}
				require.NotNil(t, err)
				require.Contains(t, err.Error(), "captured converted body")
				require.Equal(t, "/v1/messages", path)
				require.Equal(t, "claude-test", gjson.GetBytes(captured, "model").String())
				require.Equal(t, "hello", gjson.GetBytes(captured, "messages.0.content.0.text").String())
				require.Equal(t, int64(1024), gjson.GetBytes(captured, "max_tokens").Int())
				require.True(t, gjson.GetBytes(captured, "stream").Bool())
				require.False(t, gjson.GetBytes(captured, "input").Exists())
				require.False(t, gjson.GetBytes(captured, "contents").Exists())
			})
		}
	}
}
