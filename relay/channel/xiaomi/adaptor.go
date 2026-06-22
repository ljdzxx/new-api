package xiaomi

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	openai.Adaptor
}

func shouldTraceXiaomiClaude(c *gin.Context, info *relaycommon.RelayInfo) bool {
	return (common.DebugEnabled || common.DebugTraceEnabledForContext(c)) && info != nil && info.RelayFormat == types.RelayFormatClaude
}

func logXiaomiClaudeTrace(c *gin.Context, format string, args ...any) {
	if c == nil {
		return
	}
	logger.LogInfo(c, "[xiaomi claude trace] "+fmt.Sprintf(format, args...))
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech:
		return fmt.Sprintf("%s/v1/chat/completions", info.ChannelBaseUrl), nil
	default:
		if info.RelayFormat == types.RelayFormatClaude {
			requestURL := fmt.Sprintf("%s/anthropic/v1/messages", info.ChannelBaseUrl)
			if !shouldAppendXiaomiClaudeBetaQuery(info) {
				return requestURL, nil
			}
			parsedURL, err := url.Parse(requestURL)
			if err != nil {
				return "", err
			}
			query := parsedURL.Query()
			query.Set("beta", "true")
			parsedURL.RawQuery = query.Encode()
			return parsedURL.String(), nil
		}
		return a.Adaptor.GetRequestURL(info)
	}
}

func shouldAppendXiaomiClaudeBetaQuery(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	if info.IsClaudeBetaQuery {
		return true
	}
	if info.ChannelOtherSettings.ClaudeBetaQuery {
		return true
	}
	return false
}

func copyXiaomiClaudeClientHeaders(c *gin.Context, req *http.Header) {
	if c == nil || c.Request == nil || req == nil {
		return
	}
	for _, headerName := range []string{
		"anthropic-dangerous-direct-browser-access",
		"x-app",
		"x-claude-code-session-id",
		"x-stainless-arch",
		"x-stainless-lang",
		"x-stainless-os",
		"x-stainless-package-version",
		"x-stainless-retry-count",
		"x-stainless-runtime",
		"x-stainless-runtime-version",
		"x-stainless-timeout",
		"user-agent",
	} {
		if value := c.Request.Header.Get(headerName); value != "" {
			req.Set(headerName, value)
		}
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	if info.RelayFormat == types.RelayFormatClaude &&
		info.RelayMode != relayconstant.RelayModeResponses &&
		info.RelayMode != relayconstant.RelayModeResponsesCompact {
		channel.SetupApiRequestHeader(info, c, req)
		req.Set("api-key", info.ApiKey)
		anthropicVersion := c.Request.Header.Get("anthropic-version")
		if anthropicVersion == "" {
			anthropicVersion = "2023-06-01"
		}
		req.Set("anthropic-version", anthropicVersion)
		claude.CommonClaudeHeadersOperation(c, req, info)
		copyXiaomiClaudeClientHeaders(c, req)
		if shouldTraceXiaomiClaude(c, info) {
			logXiaomiClaudeTrace(c, "xiaomi adaptor headers before override: api_key_present=%t x_api_key_present=%t authorization_present=%t anthropic_version=%q anthropic_beta=%q accept=%q content_type=%q user_agent=%q direct_browser=%q x_app=%q stainless_runtime=%q stainless_package_version=%q claude_session=%q",
				req.Get("api-key") != "",
				req.Get("x-api-key") != "",
				req.Get("Authorization") != "",
				req.Get("anthropic-version"),
				req.Get("anthropic-beta"),
				req.Get("Accept"),
				req.Get("Content-Type"),
				req.Get("User-Agent"),
				req.Get("anthropic-dangerous-direct-browser-access"),
				req.Get("x-app"),
				req.Get("x-stainless-runtime"),
				req.Get("x-stainless-package-version"),
				req.Get("x-claude-code-session-id"),
			)
		}
		return nil
	}

	if err := a.Adaptor.SetupRequestHeader(c, req, info); err != nil {
		return err
	}
	req.Set("api-key", info.ApiKey)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}
	return request, nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if info.RelayFormat != types.RelayFormatClaude {
		return a.Adaptor.ConvertClaudeRequest(c, info, request)
	}
	beforeToolsSummary := summarizeClaudeToolsForLog(request.Tools)
	if err := normalizeClaudeToolsForMimo(request); err != nil {
		return nil, err
	}
	if shouldTraceXiaomiClaude(c, info) {
		toolChoiceSummary := "<nil>"
		if request.ToolChoice != nil {
			if encoded, err := common.Marshal(request.ToolChoice); err == nil {
				toolChoiceSummary = string(encoded)
			} else {
				toolChoiceSummary = fmt.Sprintf("<marshal_error:%v>", err)
			}
		}
		logXiaomiClaudeTrace(c,
			"normalized request tools: before={%s} after={%s} tool_choice=%s thinking_present=%t context_management_present=%t output_config_present=%t",
			beforeToolsSummary,
			summarizeClaudeToolsForLog(request.Tools),
			toolChoiceSummary,
			request.Thinking != nil,
			len(request.ContextManagement) > 0,
			len(request.OutputConfig) > 0,
		)
	}
	return request, nil
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	if info.RelayMode != relayconstant.RelayModeResponses && info.RelayMode != relayconstant.RelayModeResponsesCompact {
		return a.Adaptor.ConvertOpenAIResponsesRequest(c, info, request)
	}
	return nil, service.UnsupportedOpenAIResponsesProtocolError(info.ChannelId)
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	if info.RelayMode != relayconstant.RelayModeAudioSpeech {
		return a.Adaptor.ConvertAudioRequest(c, info, request)
	}
	return convertTTSRequest(c, request)
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if info.RelayMode == relayconstant.RelayModeAudioTranscription ||
		info.RelayMode == relayconstant.RelayModeAudioTranslation ||
		info.RelayMode == relayconstant.RelayModeImagesEdits {
		return channel.DoFormRequest(a, c, info, requestBody)
	}
	if info.RelayMode == relayconstant.RelayModeRealtime {
		return channel.DoWssRequest(a, c, info, requestBody)
	}
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if info.RelayMode == relayconstant.RelayModeAudioSpeech {
		return handleTTSResponse(c, resp, info)
	}
	if info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeResponsesCompact {
		if info.IsStream {
			return ChatCompletionsToResponsesStreamHandler(c, info, resp)
		}
		return ChatCompletionsToResponsesHandler(c, info, resp)
	}
	if info.RelayFormat == types.RelayFormatClaude {
		common.SetContextKey(c, constant.ContextKeyXiaomiClaudeDebug, true)
		adaptor := claude.Adaptor{}
		return adaptor.DoResponse(c, resp, info)
	}
	return a.Adaptor.DoResponse(c, resp, info)
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
