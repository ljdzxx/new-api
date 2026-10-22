package advancedcustom

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const StreamStartedKey = "advanced_custom_stream_started"

// Reuse the shared HTTP transport (proxy, header overrides and timeouts).
// Responses streaming uses stream=true, not Chat's stream_options.include_usage;
// this channel is deliberately absent from streamSupportedChannels.
type Adaptor struct {
	openai.Adaptor
	route    dto.AdvancedCustomRoute
	resolved bool
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) { a.Adaptor.Init(info); a.resolved = false }
func (a *Adaptor) resolve(info *relaycommon.RelayInfo) error {
	if a.resolved {
		return nil
	}
	if info == nil || info.ChannelMeta == nil || info.RelayFormat != types.RelayFormatGemini {
		return fmt.Errorf("advanced custom channel requires Gemini generateContent")
	}
	config := info.ChannelOtherSettings.AdvancedCustom
	if err := config.Validate(); err != nil {
		return err
	}
	var ok bool
	a.route, ok = config.MatchPath(info.RequestURLPath)
	if !ok {
		return fmt.Errorf("advanced custom channel has no route for this request path")
	}
	a.resolved = true
	return nil
}
func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if err := a.resolve(info); err != nil {
		return "", err
	}
	target := a.route.UpstreamPath
	u, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	if !u.IsAbs() {
		base, err := url.Parse(strings.TrimRight(info.ChannelBaseUrl, "/"))
		if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
			return "", fmt.Errorf("advanced custom channel requires a valid HTTP(S) base URL")
		}
		// Like other adaptors, base URLs may include a gateway prefix.
		u, err = url.Parse(strings.TrimRight(base.String(), "/") + target)
		if err != nil {
			return "", err
		}
	}
	if auth := a.route.Auth; auth != nil && auth.Type == "query" {
		query := u.Query()
		query.Set(auth.Name, strings.ReplaceAll(auth.Value, "{key}", info.ApiKey))
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}
func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	if err := a.resolve(info); err != nil {
		return err
	}
	header.Set("Content-Type", "application/json")
	if info.IsStream {
		header.Set("Accept", "text/event-stream")
	} else {
		header.Set("Accept", "application/json")
	}
	auth := a.route.Auth
	if auth == nil {
		header.Set("Authorization", "Bearer "+info.ApiKey)
	} else if auth.Type == "header" {
		header.Set(auth.Name, strings.ReplaceAll(auth.Value, "{key}", info.ApiKey))
	}
	return nil
}
func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	if err := a.resolve(info); err != nil {
		return nil, err
	}
	return convertGeminiRequest(c, info, request)
}
func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	if err := a.resolve(info); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	return channel.DoApiRequest(a, c, info, body)
}
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	return responsesToGemini(c, info, resp)
}
func (a *Adaptor) GetModelList() []string { return []string{} }
func (a *Adaptor) GetChannelName() string { return "Advanced Custom" }

func raw(value any) []byte { data, _ := common.Marshal(value); return data }
