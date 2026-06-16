package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func logClaudeRelayDebug(c *gin.Context, format string, args ...any) {
	if !common.DebugEnabled && !common.DebugTraceEnabled {
		return
	}
	logger.LogInfo(c, "[claude messages] "+fmt.Sprintf(format, args...))
}

func logClaudeRelayError(c *gin.Context, format string, args ...any) {
	logger.LogError(c, "[claude messages] "+fmt.Sprintf(format, args...))
}

func logClaudeRelayRawRequest(c *gin.Context) {
	if c == nil || c.Request == nil || (!common.DebugEnabled && !common.DebugTraceEnabled) {
		return
	}
	dump, err := httputil.DumpRequest(c.Request, false)
	if err != nil {
		logClaudeRelayError(c, "raw request dump failed: %s", err.Error())
		return
	}
	logger.LogInfo(c, "[claude messages raw] relay request metadata and headers:\n"+string(dump))
}

func claudeRelayHeaderPresent(c *gin.Context, key string) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return strings.TrimSpace(c.Request.Header.Get(key)) != ""
}

func summarizeClaudeRelayHTTPForLog(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return "http=<nil>"
	}
	path := ""
	queryPresent := false
	if c.Request.URL != nil {
		path = c.Request.URL.Path
		queryPresent = c.Request.URL.RawQuery != ""
	}
	return fmt.Sprintf(
		"method=%s path=%q query_present=%t content_type=%q content_length=%d user_agent=%q anthropic_version_present=%t anthropic_beta_present=%t authorization_present=%t x_api_key_present=%t",
		c.Request.Method,
		path,
		queryPresent,
		c.Request.Header.Get("Content-Type"),
		c.Request.ContentLength,
		c.Request.UserAgent(),
		claudeRelayHeaderPresent(c, "anthropic-version"),
		claudeRelayHeaderPresent(c, "anthropic-beta"),
		claudeRelayHeaderPresent(c, "Authorization"),
		claudeRelayHeaderPresent(c, "x-api-key"),
	)
}

func summarizeClaudeRelayRequestForLog(request dto.Request) string {
	claudeRequest, ok := request.(*dto.ClaudeRequest)
	if !ok || claudeRequest == nil {
		return fmt.Sprintf("request_type=%T", request)
	}
	roleCounts := map[string]int{}
	for _, message := range claudeRequest.Messages {
		role := strings.TrimSpace(message.Role)
		if role == "" {
			role = "<empty>"
		}
		roleCounts[role]++
	}
	stream := "<nil>"
	if claudeRequest.Stream != nil {
		stream = fmt.Sprintf("%t", *claudeRequest.Stream)
	}
	return fmt.Sprintf(
		"model=%q stream=%s messages=%d roles=%v system_present=%t tools_present=%t tool_choice_present=%t thinking_present=%t",
		claudeRequest.Model,
		stream,
		len(claudeRequest.Messages),
		roleCounts,
		claudeRequest.System != nil,
		claudeRequest.Tools != nil,
		claudeRequest.ToolChoice != nil,
		claudeRequest.Thinking != nil,
	)
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)
	if relayFormat == types.RelayFormatClaude {
		logClaudeRelayRawRequest(c)
		logClaudeRelayDebug(c, "relay entry: request_id=%s %s", requestId, summarizeClaudeRelayHTTPForLog(c))
	}

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			if relayFormat == types.RelayFormatClaude {
				logClaudeRelayError(c, "relay finished with error: request_id=%s status=%d err=%s %s", requestId, newAPIError.StatusCode, newAPIError.Error(), summarizeClaudeRelayHTTPForLog(c))
			}
			logger.LogError(c, fmt.Sprintf("relay error: %s", newAPIError.Error()))
			if types.IsSkipRetryError(newAPIError) || newAPIError.StatusCode < http.StatusInternalServerError {
				c.Header("x-should-retry", "false")
			}
			if !applyChannelErrorInterceptIfNeeded(c, newAPIError, requestId) {
				if !c.GetBool(contextKeyFixedErrorMessage) {
					newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
				}
			}
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				c.JSON(newAPIError.StatusCode, gin.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				c.JSON(newAPIError.StatusCode, gin.H{
					"error": newAPIError.ToOpenAIError(),
				})
			}
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		if relayFormat == types.RelayFormatClaude {
			logClaudeRelayError(c, "request validation failed: request_id=%s err=%s %s", requestId, err.Error(), summarizeClaudeRelayHTTPForLog(c))
		}
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		return
	}
	if relayFormat == types.RelayFormatClaude {
		logClaudeRelayDebug(c, "request validated: request_id=%s %s", requestId, summarizeClaudeRelayRequestForLog(request))
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		if relayFormat == types.RelayFormatClaude {
			logClaudeRelayError(c, "relay info generation failed: request_id=%s err=%s request=%s", requestId, err.Error(), summarizeClaudeRelayRequestForLog(request))
		}
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}
	if relayFormat == types.RelayFormatClaude {
		logClaudeRelayDebug(c, "relay info generated: request_id=%s origin_model=%q relay_mode=%d token_group=%q using_group=%q", requestId, relayInfo.OriginModelName, relayInfo.RelayMode, relayInfo.TokenGroup, relayInfo.UsingGroup)
	}

	if (relayInfo.RelayMode == relayconstant.RelayModeResponses ||
		relayInfo.RelayMode == relayconstant.RelayModeResponsesCompact) &&
		common.GetContextKeyInt(c, constant.ContextKeyChannelType) == constant.ChannelTypeXiaomi {
		channelID := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		channelName := common.GetContextKeyString(c, constant.ContextKeyChannelName)
		logger.LogWarn(c, fmt.Sprintf(
			"responses relay rejected unsupported xiaomi channel before billing: request_id=%s channel_id=%d channel_name=%q origin_model=%q path=%q stream=%t",
			requestId,
			channelID,
			channelName,
			relayInfo.OriginModelName,
			c.Request.URL.Path,
			relayInfo.IsStream,
		))
		newAPIError = types.NewErrorWithStatusCode(
			service.UnsupportedOpenAIResponsesProtocolError(channelID),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
		return
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck && meta != nil {
		contains, words := service.CheckSensitiveText(meta.CombineText)
		if contains {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = types.NewErrorWithStatusCode(errors.New("sensitive words detected"), types.ErrorCodeSensitiveWordsDetected, http.StatusForbidden, types.ErrOptionWithSkipRetry())
			return
		}
	}

	tokens, err := service.EstimateRequestToken(c, meta, relayInfo)
	if err != nil {
		if relayFormat == types.RelayFormatClaude {
			logClaudeRelayError(c, "token estimate failed: request_id=%s err=%s origin_model=%q", requestId, err.Error(), relayInfo.OriginModelName)
		}
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}

	relayInfo.SetEstimatePromptTokens(tokens)
	if relayFormat == types.RelayFormatClaude {
		logClaudeRelayDebug(c, "token estimate finished: request_id=%s origin_model=%q prompt_tokens=%d", requestId, relayInfo.OriginModelName, tokens)
	}

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, meta)
	if err != nil {
		if relayFormat == types.RelayFormatClaude {
			logClaudeRelayError(c, "model price failed: request_id=%s err=%s origin_model=%q prompt_tokens=%d", requestId, err.Error(), relayInfo.OriginModelName, tokens)
		}
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError)
		return
	}
	if relayFormat == types.RelayFormatClaude {
		logClaudeRelayDebug(c, "model price finished: request_id=%s origin_model=%q free=%t use_price=%t quota_to_pre_consume=%d", requestId, relayInfo.OriginModelName, priceData.FreeModel, priceData.UsePrice, priceData.QuotaToPreConsume)
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			if relayFormat == types.RelayFormatClaude {
				logClaudeRelayError(c, "pre-consume billing failed: request_id=%s status=%d err=%s origin_model=%q quota=%d", requestId, newAPIError.StatusCode, newAPIError.Error(), relayInfo.OriginModelName, priceData.QuotaToPreConsume)
			}
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	retryLoopLimit := relayRetryLoopLimit()
	for ; retryParam.GetRetry() <= retryLoopLimit; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			if relayFormat == types.RelayFormatClaude {
				logClaudeRelayError(c, "channel selection failed: request_id=%s retry=%d status=%d err=%s origin_model=%q", requestId, relayInfo.RetryIndex, channelErr.StatusCode, channelErr.Error(), relayInfo.OriginModelName)
			}
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			break
		}
		if relayFormat == types.RelayFormatClaude {
			logClaudeRelayDebug(c, "channel selected: request_id=%s retry=%d channel_id=%d channel_type=%d channel_name=%q origin_model=%q", requestId, relayInfo.RetryIndex, channel.Id, channel.Type, channel.Name, relayInfo.OriginModelName)
		}
		if (relayInfo.RelayMode == relayconstant.RelayModeResponses ||
			relayInfo.RelayMode == relayconstant.RelayModeResponsesCompact) &&
			channel.Type == constant.ChannelTypeXiaomi {
			logger.LogWarn(c, fmt.Sprintf(
				"responses relay rejected unsupported xiaomi channel before upstream call: request_id=%s retry=%d channel_id=%d channel_type=%d channel_name=%q origin_model=%q path=%q stream=%t",
				requestId,
				relayInfo.RetryIndex,
				channel.Id,
				channel.Type,
				channel.Name,
				relayInfo.OriginModelName,
				c.Request.URL.Path,
				relayInfo.IsStream,
			))
			newAPIError = types.NewErrorWithStatusCode(
				service.UnsupportedOpenAIResponsesProtocolError(channel.Id),
				types.ErrorCodeInvalidRequest,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
			break
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if relayFormat == types.RelayFormatClaude {
				logClaudeRelayError(c, "request body storage failed: request_id=%s retry=%d channel_id=%d err=%s", requestId, relayInfo.RetryIndex, channel.Id, bodyErr.Error())
			}
			// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
			} else {
				newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		switch relayFormat {
		case types.RelayFormatOpenAIRealtime:
			newAPIError = relay.WssHelper(c, relayInfo)
		case types.RelayFormatClaude:
			newAPIError = relay.ClaudeHelper(c, relayInfo)
		case types.RelayFormatGemini:
			newAPIError = geminiRelayHandler(c, relayInfo)
		default:
			newAPIError = relayHandler(c, relayInfo)
		}

		if newAPIError == nil {
			relayInfo.LastError = nil
			if relayFormat == types.RelayFormatClaude {
				logClaudeRelayDebug(c, "relay succeeded: request_id=%s retry=%d channel_id=%d origin_model=%q upstream_model=%q", requestId, relayInfo.RetryIndex, channel.Id, relayInfo.OriginModelName, relayInfo.UpstreamModelName)
			}
			return
		}

		newAPIError = service.NormalizeViolationFeeError(newAPIError)
		if c.GetBool(contextKeyUpstreamCallStarted) && newAPIError.StatusCode > 0 && c.GetInt(contextKeyUpstreamResponseCode) == 0 {
			c.Set(contextKeyUpstreamResponseCode, newAPIError.StatusCode)
		}
		relayInfo.LastError = newAPIError
		if relayFormat == types.RelayFormatClaude {
			logClaudeRelayError(c, "relay attempt failed: request_id=%s retry=%d channel_id=%d status=%d err=%s origin_model=%q upstream_model=%q", requestId, relayInfo.RetryIndex, channel.Id, newAPIError.StatusCode, newAPIError.Error(), relayInfo.OriginModelName, relayInfo.UpstreamModelName)
		}

		processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)
		retryParam.MarkChannelFailed(channel.Id)

		if forceRetry, finalErr := shouldForceRetryForGlobalQuotaInsufficient(c, channel.Id, newAPIError); finalErr != nil {
			newAPIError = finalErr
			break
		} else if forceRetry {
			continue
		}

		if !shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

const (
	contextKeyUpstreamCallStarted  = "upstream_call_started"
	contextKeyUpstreamResponseCode = "upstream_response_code"
	contextKeyFixedErrorMessage    = "fixed_error_message"
	maxGlobalQuotaInsufficientHits = 3
)

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func newNoAvailableChannelError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(
		fmt.Errorf("当前无可用渠道"),
		types.ErrorCodeGetChannelFailed,
		http.StatusServiceUnavailable,
		types.ErrOptionWithSkipRetry(),
	)
}

func taskNoAvailableChannelError() *dto.TaskError {
	return service.TaskErrorWrapperLocal(fmt.Errorf("当前无可用渠道"), string(types.ErrorCodeGetChannelFailed), http.StatusServiceUnavailable)
}

func isSpecificChannelRequest(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return true
	}
	_, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId)
	return ok
}

func recordGlobalQuotaInsufficientHit(c *gin.Context, channelID int, errText string) int {
	hits := c.GetInt("global_quota_insufficient_hits") + 1
	c.Set("global_quota_insufficient_hits", hits)
	logger.LogInfo(c, fmt.Sprintf("global quota insufficient keyword matched: hit=%d/%d channel_id=%d err=%s", hits, maxGlobalQuotaInsufficientHits, channelID, errText))
	return hits
}

func shouldForceRetryForGlobalQuotaInsufficient(c *gin.Context, channelID int, err *types.NewAPIError) (bool, *types.NewAPIError) {
	if err == nil || !service.ShouldMatchGlobalQuotaInsufficientKeyword(err) {
		return false, nil
	}
	hits := recordGlobalQuotaInsufficientHit(c, channelID, err.ErrorWithStatusCode())
	if hits >= maxGlobalQuotaInsufficientHits {
		return false, newNoAvailableChannelError()
	}
	if isSpecificChannelRequest(c) {
		return false, nil
	}
	return true, nil
}

func shouldForceRetryTaskForGlobalQuotaInsufficient(c *gin.Context, channelID int, taskErr *dto.TaskError) (bool, *dto.TaskError) {
	if taskErr == nil {
		return false, nil
	}
	err := taskErr.Error
	if err == nil {
		err = errors.New(taskErr.Message)
	}
	apiErr := types.NewOpenAIError(err, types.ErrorCode(taskErr.Code), taskErr.StatusCode)
	if !service.ShouldMatchGlobalQuotaInsufficientKeyword(apiErr) {
		return false, nil
	}
	hits := recordGlobalQuotaInsufficientHit(c, channelID, taskErr.Message)
	if hits >= maxGlobalQuotaInsufficientHits {
		return false, taskNoAvailableChannelError()
	}
	if isSpecificChannelRequest(c) {
		return false, nil
	}
	return true, nil
}

func relayRetryLoopLimit() int {
	limit := common.RetryTimes
	if minLimit := maxGlobalQuotaInsufficientHits - 1; limit < minLimit {
		return minLimit
	}
	return limit
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannelForwardSetting(c *gin.Context, channel *model.Channel) dto.ChannelSettings {
	if channel != nil && channel.Setting != nil {
		return channel.GetSetting()
	}
	if setting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting); ok {
		return setting
	}
	return dto.ChannelSettings{}
}

func getSelectedChannelSetting(c *gin.Context) dto.ChannelSettings {
	if setting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting); ok {
		return setting
	}
	return dto.ChannelSettings{}
}

func applyChannelErrorInterceptIfNeeded(c *gin.Context, err *types.NewAPIError, requestId string) bool {
	if c == nil || err == nil || !c.GetBool(contextKeyUpstreamCallStarted) {
		return false
	}
	channelSetting := getSelectedChannelSetting(c)
	if !channelSetting.ErrorInterceptEnabled {
		return false
	}
	template := strings.TrimSpace(channelSetting.ErrorInterceptMessage)
	if template == "" {
		return false
	}
	message := template
	responseCode := err.StatusCode
	upstreamCode := err.UpstreamStatusCode
	if upstreamCode == 0 {
		upstreamCode = c.GetInt(contextKeyUpstreamResponseCode)
		if upstreamCode == 0 {
			upstreamCode = responseCode
		}
	}
	message = strings.ReplaceAll(message, "{request_id}", requestId)
	message = strings.ReplaceAll(message, "{response_code}", strconv.Itoa(responseCode))
	message = strings.ReplaceAll(message, "{error_code}", strconv.Itoa(upstreamCode))
	err.SetMessage(message)
	return true
}

func hasBuiltInToolCall(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ResponsesUsageInfo == nil || len(info.ResponsesUsageInfo.BuiltInTools) == 0 {
		return false
	}
	for toolType := range info.ResponsesUsageInfo.BuiltInTools {
		if strings.TrimSpace(toolType) != "function" {
			return true
		}
	}
	return false
}

func cloneRequestHeadersForForwardPrecheck(source http.Header) http.Header {
	cloned := make(http.Header, len(source))
	for key, values := range source {
		for _, value := range values {
			cloned.Add(key, value)
		}
	}
	cloned.Set("Content-Type", "application/json")
	cloned.Set("Accept", "application/json")
	return cloned
}

func copyForwardPrecheckContextValues(source *gin.Context, target *gin.Context) {
	if source == nil || target == nil {
		return
	}
	for _, key := range []string{
		common.RequestIdKey,
		string(constant.ContextKeyUserId),
		string(constant.ContextKeyUsingGroup),
		string(constant.ContextKeyUserGroup),
		string(constant.ContextKeyUserQuota),
		string(constant.ContextKeyUserEmail),
		string(constant.ContextKeyTokenId),
		string(constant.ContextKeyTokenKey),
		string(constant.ContextKeyTokenUnlimited),
		string(constant.ContextKeyTokenGroup),
		string(constant.ContextKeyUserSetting),
		string(constant.ContextKeyRequestStartTime),
	} {
		if value, ok := source.Get(key); ok {
			target.Set(key, value)
		}
	}
}

func runChannelForwardPrecheck(c *gin.Context, sourceInfo *relaycommon.RelayInfo, sourceChannel *model.Channel, channelSetting dto.ChannelSettings, newMessage string) (map[string]float64, *dto.Usage, *types.NewAPIError) {
	if c == nil || sourceInfo == nil || sourceChannel == nil {
		return nil, nil, types.NewErrorWithStatusCode(fmt.Errorf("invalid forward precheck context"), types.ErrorCodeBadRequestBody, http.StatusNetworkAuthenticationRequired, types.ErrOptionWithSkipRetry())
	}
	precheckModel := strings.TrimSpace(channelSetting.ForwardPrecheckModel)
	if precheckModel == "" || strings.TrimSpace(channelSetting.ForwardPrecheckPrompt) == "" {
		return nil, nil, types.NewErrorWithStatusCode(fmt.Errorf("forward precheck config is incomplete"), types.ErrorCodeBadRequestBody, http.StatusNetworkAuthenticationRequired, types.ErrOptionWithSkipRetry())
	}

	recorder := httptest.NewRecorder()
	precheckContext, _ := gin.CreateTestContext(recorder)
	precheckContext.Request = c.Request.Clone(c.Request.Context())
	precheckContext.Request.Method = http.MethodPost
	precheckContext.Request.URL = &url.URL{Path: "/v1/chat/completions"}
	precheckContext.Request.Header = cloneRequestHeadersForForwardPrecheck(c.Request.Header)
	precheckContext.Set(common.RequestIdKey, c.GetString(common.RequestIdKey))
	common.SetContextKey(precheckContext, constant.ContextKeyChannelForwardedApplied, true)
	precheckContext.Set("channel_forward_precheck", true)
	copyForwardPrecheckContextValues(c, precheckContext)

	request := &dto.GeneralOpenAIRequest{
		Model: precheckModel,
		Messages: []dto.Message{
			{Role: "system", Content: channelSetting.ForwardPrecheckPrompt},
			{Role: "user", Content: newMessage},
		},
		Stream:    common.GetPointer(false),
		MaxTokens: common.GetPointer[uint](256),
		ResponseFormat: &dto.ResponseFormat{
			Type: "json_object",
		},
	}
	body, err := common.Marshal(request)
	if err != nil {
		return nil, nil, types.NewErrorWithStatusCode(err, types.ErrorCodeJsonMarshalFailed, http.StatusNetworkAuthenticationRequired, types.ErrOptionWithSkipRetry())
	}
	precheckContext.Request.Body = io.NopCloser(bytes.NewReader(body))
	precheckContext.Request.ContentLength = int64(len(body))
	storage, err := common.CreateBodyStorage(body)
	if err != nil {
		return nil, nil, types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusNetworkAuthenticationRequired, types.ErrOptionWithSkipRetry())
	}
	defer storage.Close()
	precheckContext.Set(common.KeyBodyStorage, storage)

	setupErr := middleware.SetupContextForSelectedChannel(precheckContext, sourceChannel, precheckModel)
	if setupErr != nil {
		return nil, nil, fixedForwardPrecheckError(setupErr)
	}
	precheckKey := common.GetContextKeyString(precheckContext, constant.ContextKeyChannelKey)
	precheckKeyHash := ""
	if precheckKey != "" {
		precheckKeyHash = common.Sha1([]byte(precheckKey))
		if len(precheckKeyHash) > 12 {
			precheckKeyHash = precheckKeyHash[:12]
		}
	}
	if common.DebugEnabled || common.DebugTraceEnabled {
		logger.LogInfo(c, fmt.Sprintf("channel forward precheck selected channel: source_channel=%d channel_type=%d base_url=%q key_present=%t key_len=%d key_sha1=%s multi_key=%t multi_key_index=%d",
			sourceChannel.Id,
			sourceChannel.Type,
			common.GetContextKeyString(precheckContext, constant.ContextKeyChannelBaseUrl),
			precheckKey != "",
			len(precheckKey),
			precheckKeyHash,
			common.GetContextKeyBool(precheckContext, constant.ContextKeyChannelIsMultiKey),
			common.GetContextKeyInt(precheckContext, constant.ContextKeyChannelMultiKeyIndex),
		))
	}

	precheckInfo, err := relaycommon.GenRelayInfo(precheckContext, types.RelayFormatOpenAI, request, nil)
	if err != nil {
		return nil, nil, fixedForwardPrecheckError(types.NewError(err, types.ErrorCodeGenRelayInfoFailed, types.ErrOptionWithSkipRetry()))
	}
	precheckInfo.RelayMode = relayconstant.RelayModeChatCompletions
	precheckInfo.RequestURLPath = "/v1/chat/completions"
	precheckInfo.InitChannelMeta(precheckContext)
	adaptor := relay.GetAdaptor(precheckInfo.ApiType)
	if adaptor == nil {
		return nil, nil, fixedForwardPrecheckError(types.NewError(fmt.Errorf("invalid api type: %d", precheckInfo.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry()))
	}
	if err := helper.ModelMappedHelper(precheckContext, precheckInfo, request); err != nil {
		return nil, nil, fixedForwardPrecheckError(types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry()))
	}
	adaptor.Init(precheckInfo)
	convertedRequest, err := adaptor.ConvertOpenAIRequest(precheckContext, precheckInfo, request)
	if err != nil {
		return nil, nil, fixedForwardPrecheckError(types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry()))
	}
	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, nil, fixedForwardPrecheckError(types.NewError(err, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry()))
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, precheckInfo.ChannelOtherSettings, precheckInfo.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, nil, fixedForwardPrecheckError(types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry()))
	}
	if len(precheckInfo.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, precheckInfo)
		if err != nil {
			if fixedErr, ok := relaycommon.AsParamOverrideReturnError(err); ok {
				return nil, nil, fixedForwardPrecheckError(relaycommon.NewAPIErrorFromParamOverride(fixedErr))
			}
			return nil, nil, fixedForwardPrecheckError(types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid, types.ErrOptionWithSkipRetry()))
		}
	}

	precheckURL := "<unknown>"
	if requestURL, urlErr := adaptor.GetRequestURL(precheckInfo); urlErr == nil {
		precheckURL = requestURL
	}
	if common.DebugEnabled || common.DebugTraceEnabled {
		logger.LogInfo(c, fmt.Sprintf("channel forward precheck request: source_channel=%d model=%s relay_mode=%d request_url=%s message_chars=%d", sourceChannel.Id, precheckInfo.UpstreamModelName, precheckInfo.RelayMode, precheckURL, len([]rune(newMessage))))
	}
	startTime := time.Now()
	respAny, err := adaptor.DoRequest(precheckContext, precheckInfo, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, nil, fixedForwardPrecheckError(types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError, types.ErrOptionWithSkipRetry()))
	}
	httpResp, ok := respAny.(*http.Response)
	if !ok || httpResp == nil {
		return nil, nil, fixedForwardPrecheckError(types.NewOpenAIError(fmt.Errorf("forward precheck response is nil"), types.ErrorCodeBadResponse, http.StatusInternalServerError, types.ErrOptionWithSkipRetry()))
	}
	defer service.CloseResponseBodyGracefully(httpResp)
	if httpResp.StatusCode != http.StatusOK {
		return nil, nil, fixedForwardPrecheckError(service.RelayErrorHandler(precheckContext.Request.Context(), httpResp, false))
	}
	responseBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, nil, fixedForwardPrecheckError(types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError, types.ErrOptionWithSkipRetry()))
	}
	var response dto.OpenAITextResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, nil, fixedForwardPrecheckError(types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError, types.ErrOptionWithSkipRetry()))
	}
	if oaiError := response.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, nil, fixedForwardPrecheckError(types.WithOpenAIError(*oaiError, httpResp.StatusCode, types.ErrOptionWithSkipRetry()))
	}
	resultText := ""
	if len(response.Choices) > 0 {
		resultText = strings.TrimSpace(response.Choices[0].Message.StringContent())
	}
	metrics, err := parseChannelForwardPrecheckMetrics(resultText)
	if err != nil {
		return nil, nil, fixedForwardPrecheckError(types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusNetworkAuthenticationRequired, types.ErrOptionWithSkipRetry()))
	}
	usage := response.Usage
	recordChannelForwardPrecheckUsage(precheckContext, sourceInfo, precheckInfo, sourceChannel, &usage, int(time.Since(startTime).Seconds()))
	if common.DebugEnabled || common.DebugTraceEnabled {
		logger.LogInfo(c, fmt.Sprintf("channel forward precheck result: source_channel=%d model=%s metrics=%v prompt_tokens=%d completion_tokens=%d", sourceChannel.Id, precheckInfo.UpstreamModelName, metrics, usage.PromptTokens, usage.CompletionTokens))
	}
	return metrics, &usage, nil
}

func fixedForwardPrecheckError(err *types.NewAPIError) *types.NewAPIError {
	if err == nil {
		return types.NewErrorWithStatusCode(fmt.Errorf("预请求失败，请重试。"), types.ErrorCodeBadResponse, http.StatusNetworkAuthenticationRequired, types.ErrOptionWithSkipRetry())
	}
	common.SysError(fmt.Sprintf("channel forward precheck failed: status=%d err=%s", err.StatusCode, err.Error()))
	fixedErr := types.NewErrorWithStatusCode(fmt.Errorf("预请求失败，请重试。"), err.GetErrorCode(), http.StatusNetworkAuthenticationRequired, types.ErrOptionWithSkipRetry())
	return fixedErr
}

func recordChannelForwardPrecheckUsage(c *gin.Context, sourceInfo *relaycommon.RelayInfo, precheckInfo *relaycommon.RelayInfo, sourceChannel *model.Channel, usage *dto.Usage, useTimeSeconds int) {
	if c == nil || sourceInfo == nil || precheckInfo == nil || sourceChannel == nil || usage == nil {
		return
	}
	userId := sourceInfo.UserId
	if userId == 0 {
		userId = c.GetInt("id")
	}
	other := map[string]interface{}{
		"channel_forward_precheck": true,
		"source_channel_id":        sourceChannel.Id,
		"source_model":             sourceInfo.OriginModelName,
		"upstream_model":           precheckInfo.UpstreamModelName,
		"note":                     "system side precheck usage, quota is not charged to user",
	}
	model.RecordConsumeLog(c, userId, model.RecordConsumeLogParams{
		ChannelId:        sourceChannel.Id,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		ModelName:        precheckInfo.UpstreamModelName,
		TokenName:        c.GetString("token_name"),
		Quota:            0,
		Content:          "渠道转发预检",
		TokenId:          sourceInfo.TokenId,
		UseTimeSeconds:   useTimeSeconds,
		IsStream:         false,
		Group:            sourceInfo.UsingGroup,
		Other:            other,
	})
}

func parseChannelForwardPrecheckMetrics(text string) (map[string]float64, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	var raw map[string]any
	if err := common.Unmarshal([]byte(text), &raw); err != nil {
		return nil, err
	}
	metrics := make(map[string]float64, len(raw))
	for key, value := range raw {
		switch v := value.(type) {
		case float64:
			metrics[key] = v
		case int:
			metrics[key] = float64(v)
		case int64:
			metrics[key] = float64(v)
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return nil, fmt.Errorf("metric %s is not numeric", key)
			}
			metrics[key] = parsed
		case json.Number:
			parsed, err := v.Float64()
			if err != nil {
				return nil, fmt.Errorf("metric %s is not numeric", key)
			}
			metrics[key] = parsed
		default:
			return nil, fmt.Errorf("metric %s is not numeric", key)
		}
	}
	return metrics, nil
}

func logChannelForwardApplied(c *gin.Context, sourceChannel *model.Channel, targetChannel *model.Channel, relayInfo *relaycommon.RelayInfo, matchInfo *service.ChannelForwardMatchInfo) {
	if c == nil || sourceChannel == nil || targetChannel == nil {
		return
	}
	requestPath := ""
	if c.Request != nil && c.Request.URL != nil {
		requestPath = c.Request.URL.Path
	}
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	modelName := ""
	if relayInfo != nil {
		modelName = relayInfo.OriginModelName
	}
	source := ""
	snippet := ""
	matchedText := ""
	metricLogic := ""
	targetModel := ""
	metrics := map[string]float64(nil)
	if matchInfo != nil {
		source = matchInfo.Source
		snippet = matchInfo.TextSnippet
		matchedText = matchInfo.MatchedText
		metricLogic = matchInfo.MetricLogic
		targetModel = matchInfo.TargetModel
		metrics = matchInfo.Metrics
	}
	logger.LogInfo(c, fmt.Sprintf(
		"channel forward applied: from #%d(%s) to #%d(%s), group=%s, original_model=%s, target_model_pattern=%s, path=%s, logic=%s, source=%s, matched=%q, metrics=%v, snippet=%q",
		sourceChannel.Id,
		sourceChannel.Name,
		targetChannel.Id,
		targetChannel.Name,
		usingGroup,
		modelName,
		targetModel,
		requestPath,
		metricLogic,
		source,
		matchedText,
		metrics,
		snippet,
	))
}

func maybeApplyChannelForward(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) (*model.Channel, *types.NewAPIError) {
	if channel == nil || info == nil || info.Request == nil || common.GetContextKeyBool(c, constant.ContextKeyChannelForwardedApplied) {
		return channel, nil
	}

	channelSetting := getChannelForwardSetting(c, channel)
	var (
		shouldForward bool
		matchInfo     *service.ChannelForwardMatchInfo
		err           error
	)
	if hasBuiltInToolCall(info) {
		targetChannelID, targetModel, ok := service.MatchChannelForwardTarget(info.OriginModelName, channelSetting)
		shouldForward = channelSetting.ForwardEnabled && ok
		if shouldForward {
			matchInfo = &service.ChannelForwardMatchInfo{
				Source:          "built_in_tools",
				MatchedText:     "built-in tool call",
				TargetModel:     targetModel,
				TargetChannelID: targetChannelID,
			}
		}
	} else {
		if !channelSetting.ForwardEnabled {
			return channel, nil
		}
		newMessage := service.ExtractChannelForwardNewMessage(info.Request, info.RelayFormat, info.RelayMode)
		if newMessage == "" {
			return channel, nil
		}
		if service.IsChannelForwardMessageTooLong(newMessage, channelSetting) {
			if common.DebugEnabled || common.DebugTraceEnabled {
				logger.LogInfo(c, fmt.Sprintf("channel forward skipped: channel_id=%d original_model=%s reason=message_too_long chars=%d max=%d", channel.Id, info.OriginModelName, len([]rune(newMessage)), channelSetting.ForwardMaxMessageChars))
			}
			return channel, nil
		}
		metrics, _, precheckErr := runChannelForwardPrecheck(c, info, channel, channelSetting, newMessage)
		if precheckErr != nil {
			c.Set(contextKeyFixedErrorMessage, true)
			return nil, precheckErr
		}
		shouldForward, matchInfo, err = service.EvaluateChannelForwardPrecheck(metrics, channelSetting, info.OriginModelName, newMessage)
		if err != nil {
			common.SysError(fmt.Sprintf("channel forward precheck config failed: %s", err.Error()))
			c.Set(contextKeyFixedErrorMessage, true)
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("预请求失败，请重试。"), types.ErrorCodeGetChannelFailed, http.StatusNetworkAuthenticationRequired, types.ErrOptionWithSkipRetry())
		}
	}
	if !shouldForward {
		if matchInfo != nil && matchInfo.SkipReason != "" {
			if common.DebugEnabled || common.DebugTraceEnabled {
				logger.LogInfo(c, fmt.Sprintf("channel forward skipped: channel_id=%d original_model=%s reason=%s metrics=%v snippet=%q", channel.Id, info.OriginModelName, matchInfo.SkipReason, matchInfo.Metrics, matchInfo.TextSnippet))
			}
		}
		return channel, nil
	}

	targetChannelID := 0
	if matchInfo != nil {
		targetChannelID = matchInfo.TargetChannelID
	}
	if targetChannelID <= 0 {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("forward target channel is empty"), types.ErrorCodeGetChannelFailed, http.StatusNetworkAuthenticationRequired, types.ErrOptionWithSkipRetry())
	}

	targetChannel, err := model.CacheGetChannel(targetChannelID)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if targetChannel.Status != common.ChannelStatusEnabled {
		return nil, types.NewError(fmt.Errorf("channel #%d is disabled", targetChannel.Id), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	logChannelForwardApplied(c, channel, targetChannel, info, matchInfo)

	setupErr := middleware.SetupContextForSelectedChannel(c, targetChannel, info.OriginModelName)
	if setupErr != nil {
		return nil, setupErr
	}
	common.SetContextKey(c, constant.ContextKeyChannelForwardedApplied, true)
	common.SetContextKey(c, constant.ContextKeyChannelForwardLockedId, targetChannel.Id)
	return targetChannel, nil
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if lockedChannelID := common.GetContextKeyInt(c, constant.ContextKeyChannelForwardLockedId); lockedChannelID > 0 {
		targetChannel, err := model.CacheGetChannel(lockedChannelID)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if targetChannel.Status != common.ChannelStatusEnabled {
			return nil, types.NewError(fmt.Errorf("channel #%d is disabled", targetChannel.Id), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
		}
		if setupErr := middleware.SetupContextForSelectedChannel(c, targetChannel, info.OriginModelName); setupErr != nil {
			return nil, setupErr
		}
		return targetChannel, nil
	}

	if info.ChannelMeta == nil {
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		channel := &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}
		if channel.Id > 0 {
			if fullChannel, err := model.CacheGetChannel(channel.Id); err == nil && fullChannel != nil {
				channel = fullChannel
			}
		}
		return maybeApplyChannelForward(c, info, channel)
	}
	channel, selectGroup, err := service.GetNextRetryChannel(retryParam)

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return maybeApplyChannelForward(c, info, channel)
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, err.Error()))
	if service.ShouldMarkChannelQuotaInsufficient(err) {
		if markErr := service.MarkChannelQuotaInsufficientDaily(channelError.ChannelId, err.ErrorWithStatusCode()); markErr != nil {
			logger.LogError(c, fmt.Sprintf("mark channel daily quota-insufficient failed (channel #%d): %s", channelError.ChannelId, markErr.Error()))
		}
	}
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if service.ShouldDisableChannel(channelError.ChannelType, err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		useTimeSeconds := int(time.Since(startTime).Seconds())
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, false, userGroup, other)
	}

}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *dto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *dto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}

	retryLoopLimit := relayRetryLoopLimit()
	for ; retryParam.GetRetry() <= retryLoopLimit; retryParam.IncreaseRetry() {
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			break
		}

		if !taskErr.LocalError {
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode))
		}
		retryParam.MarkChannelFailed(channel.Id)

		if forceRetry, finalErr := shouldForceRetryTaskForGlobalQuotaInsufficient(c, channel.Id, taskErr); finalErr != nil {
			taskErr = finalErr
			break
		} else if forceRetry {
			continue
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：结算 + 日志 + 插入任务 ──
	if taskErr == nil {
		if settleErr := service.SettleBilling(c, relayInfo, result.Quota); settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
		}
		service.LogTaskConsumption(c, relayInfo)

		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:             relayInfo.PriceData.ModelPrice,
			GroupRatio:             relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:             relayInfo.PriceData.ModelRatio,
			SystemGlobalModelRatio: relayInfo.PriceData.SystemGlobalModelRatio,
			UserGlobalModelRatio:   relayInfo.PriceData.UserGlobalModelRatio,
			ChannelModelRatio:      relayInfo.PriceData.ChannelModelRatio,
			GlobalModelRatio:       relayInfo.PriceData.GlobalModelRatio,
			OtherRatios:            relayInfo.PriceData.OtherRatios,
			OriginModelName:        relayInfo.OriginModelName,
			PerCallBilling:         common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName),
		}
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if insertErr := task.Insert(); insertErr != nil {
			common.SysError("insert task error: " + insertErr.Error())
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr.StatusCode == http.StatusTooManyRequests {
		taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *dto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
