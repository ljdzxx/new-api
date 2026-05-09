package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func logClaudeMessagesDebug(c *gin.Context, format string, args ...any) {
	if !common.DebugEnabled && !common.DebugTraceEnabled {
		return
	}
	logger.LogInfo(c, "[claude messages] "+fmt.Sprintf(format, args...))
}

func logXiaomiClaudeTrace(c *gin.Context, format string, args ...any) {
	if c == nil || (!common.DebugEnabled && !common.DebugTraceEnabled) {
		return
	}
	logger.LogInfo(c, "[xiaomi claude trace] "+fmt.Sprintf(format, args...))
}

func shouldTraceXiaomiClaude(info *relaycommon.RelayInfo) bool {
	if !common.DebugEnabled && !common.DebugTraceEnabled {
		return false
	}
	return info != nil &&
		info.ChannelType == constant.ChannelTypeXiaomi &&
		info.RelayFormat == types.RelayFormatClaude
}

func logClaudeMessagesError(c *gin.Context, format string, args ...any) {
	logger.LogError(c, "[claude messages] "+fmt.Sprintf(format, args...))
}

func claudeHeaderForLog(c *gin.Context, key string) string {
	if c == nil || c.Request == nil {
		return ""
	}
	value := strings.TrimSpace(c.Request.Header.Get(key))
	if len(value) > 160 {
		return value[:160] + "...(truncated)"
	}
	return value
}

func summarizeClaudeClientHeadersForLog(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return "headers=<nil>"
	}
	return fmt.Sprintf(
		"content_type=%q content_length=%d user_agent=%q anthropic_version=%q anthropic_beta=%q x_stainless_lang=%q x_stainless_package_version=%q originator=%q authorization_present=%t x_api_key_present=%t",
		claudeHeaderForLog(c, "Content-Type"),
		c.Request.ContentLength,
		claudeHeaderForLog(c, "User-Agent"),
		claudeHeaderForLog(c, "anthropic-version"),
		claudeHeaderForLog(c, "anthropic-beta"),
		claudeHeaderForLog(c, "x-stainless-lang"),
		claudeHeaderForLog(c, "x-stainless-package-version"),
		claudeHeaderForLog(c, "originator"),
		claudeHeaderForLog(c, "Authorization") != "",
		claudeHeaderForLog(c, "x-api-key") != "",
	)
}

func claudeOptionalUintForLog(value *uint) string {
	if value == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%d", *value)
}

func claudeOptionalFloatForLog(value *float64) string {
	if value == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%g", *value)
}

func claudeOptionalIntForLog(value *int) string {
	if value == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%d", *value)
}

func claudeOptionalBoolForLog(value *bool) string {
	if value == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%t", *value)
}

func claudeThinkingForLog(thinking *dto.Thinking) string {
	if thinking == nil {
		return "<nil>"
	}
	return fmt.Sprintf("type=%q budget_tokens=%s display=%q", thinking.Type, claudeOptionalIntForLog(thinking.BudgetTokens), thinking.Display)
}

func summarizeClaudeMessagesForLog(messages []dto.ClaudeMessage) string {
	if len(messages) == 0 {
		return "messages=0"
	}
	roleCounts := map[string]int{}
	stringCount := 0
	emptyStringCount := 0
	mediaBlockCount := 0
	unknownContentCount := 0
	mediaTypeCounts := map[string]int{}
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		if role == "" {
			role = "<empty>"
		}
		roleCounts[role]++
		switch content := message.Content.(type) {
		case string:
			stringCount++
			if content == "" {
				emptyStringCount++
			}
		case []dto.ClaudeMediaMessage:
			mediaBlockCount += len(content)
			for _, item := range content {
				mediaTypeCounts[item.Type]++
			}
		case []any:
			mediaBlockCount += len(content)
			for _, item := range content {
				itemMap, ok := item.(map[string]any)
				if !ok {
					mediaTypeCounts["<unknown>"]++
					continue
				}
				itemType, _ := itemMap["type"].(string)
				if itemType == "" {
					itemType = "<empty>"
				}
				mediaTypeCounts[itemType]++
			}
		case nil:
			unknownContentCount++
		default:
			unknownContentCount++
		}
	}
	return fmt.Sprintf(
		"messages=%d roles=%v string_messages=%d empty_string_messages=%d media_blocks=%d media_types=%v unknown_contents=%d",
		len(messages),
		roleCounts,
		stringCount,
		emptyStringCount,
		mediaBlockCount,
		mediaTypeCounts,
		unknownContentCount,
	)
}

func summarizeClaudeRequestForLog(request *dto.ClaudeRequest) string {
	if request == nil {
		return "request=<nil>"
	}
	systemKind := "<nil>"
	switch request.System.(type) {
	case string:
		systemKind = "string"
	case []dto.ClaudeMediaMessage:
		systemKind = "media_array"
	case []any:
		systemKind = "array"
	default:
		if request.System != nil {
			systemKind = fmt.Sprintf("%T", request.System)
		}
	}
	return fmt.Sprintf(
		"model=%q stream=%s max_tokens=%s temperature=%s top_p=%s top_k=%s system=%s tools_present=%t tool_choice_present=%t thinking={%s} output_config_present=%t output_format_present=%t context_management_present=%t container_present=%t mcp_servers_present=%t metadata_present=%t service_tier=%q inference_geo=%q speed_present=%t cache_control_present=%t %s",
		request.Model,
		claudeOptionalBoolForLog(request.Stream),
		claudeOptionalUintForLog(request.MaxTokens),
		claudeOptionalFloatForLog(request.Temperature),
		claudeOptionalFloatForLog(request.TopP),
		claudeOptionalIntForLog(request.TopK),
		systemKind,
		request.Tools != nil,
		request.ToolChoice != nil,
		claudeThinkingForLog(request.Thinking),
		len(request.OutputConfig) > 0,
		len(request.OutputFormat) > 0,
		len(request.ContextManagement) > 0,
		len(request.Container) > 0,
		len(request.McpServers) > 0,
		len(request.Metadata) > 0,
		request.ServiceTier,
		request.InferenceGeo,
		len(request.Speed) > 0,
		len(request.CacheControl) > 0,
		summarizeClaudeMessagesForLog(request.Messages),
	)
}

func summarizeClaudeChannelForLog(info *relaycommon.RelayInfo) string {
	if info == nil || info.ChannelMeta == nil {
		return "channel=<nil>"
	}
	return fmt.Sprintf(
		"channel_id=%d channel_type=%d api_type=%d base_url=%q upstream_model=%q origin_model=%q mapped=%t retry=%d pass_through_global=%t pass_through_channel=%t beta_query=%t allow_service_tier=%t allow_inference_geo=%t allow_speed=%t param_override=%d header_override=%d",
		info.ChannelId,
		info.ChannelType,
		info.ApiType,
		info.ChannelBaseUrl,
		info.UpstreamModelName,
		info.OriginModelName,
		info.IsModelMapped,
		info.RetryIndex,
		model_setting.GetGlobalSettings().PassThroughRequestEnabled,
		info.ChannelSetting.PassThroughBodyEnabled,
		info.IsClaudeBetaQuery || info.ChannelOtherSettings.ClaudeBetaQuery,
		info.ChannelOtherSettings.AllowServiceTier,
		info.ChannelOtherSettings.AllowInferenceGeo,
		info.ChannelOtherSettings.AllowSpeed,
		len(info.ParamOverride),
		len(info.HeadersOverride),
	)
}

func summarizeClaudeJSONBodyForLog(jsonData []byte) string {
	summary := fmt.Sprintf("bytes=%d", len(jsonData))
	if len(jsonData) == 0 {
		return summary
	}
	var data map[string]any
	if err := common.Unmarshal(jsonData, &data); err != nil {
		return summary + fmt.Sprintf(" json_parse_error=%q", err.Error())
	}
	messageCount := 0
	if messages, ok := data["messages"].([]any); ok {
		messageCount = len(messages)
	}
	model, _ := data["model"].(string)
	return summary + fmt.Sprintf(
		" model=%q messages=%d stream=%v max_tokens_present=%t thinking_present=%t tools_present=%t tool_choice_present=%t system_present=%t service_tier_present=%t inference_geo_present=%t speed_present=%t cache_control_present=%t output_config_present=%t mcp_servers_present=%t",
		model,
		messageCount,
		data["stream"],
		data["max_tokens"] != nil,
		data["thinking"] != nil,
		data["tools"] != nil,
		data["tool_choice"] != nil,
		data["system"] != nil,
		data["service_tier"] != nil,
		data["inference_geo"] != nil,
		data["speed"] != nil,
		data["cache_control"] != nil,
		data["output_config"] != nil,
		data["mcp_servers"] != nil,
	)
}

func summarizeClaudeJSONBodyDetailsForLog(jsonData []byte) string {
	var data map[string]any
	if err := common.Unmarshal(jsonData, &data); err != nil {
		return fmt.Sprintf("json_parse_error=%q", err.Error())
	}

	toolSummary := "tools=<absent>"
	if tools, ok := data["tools"]; ok {
		toolSummary = summarizeClaudeToolsAnyForLog(tools)
	}

	toolChoiceSummary := "tool_choice=<absent>"
	if toolChoice, ok := data["tool_choice"]; ok {
		toolChoiceSummary = "tool_choice=" + compactAnyForLog(toolChoice, 2000)
	}

	thinkingSummary := "thinking=<absent>"
	if thinking, ok := data["thinking"]; ok {
		thinkingSummary = "thinking=" + compactAnyForLog(thinking, 2000)
	}

	contextSummary := "context_management=<absent>"
	if contextManagement, ok := data["context_management"]; ok {
		contextSummary = "context_management=" + compactAnyForLog(contextManagement, 2000)
	}

	outputConfigSummary := "output_config=<absent>"
	if outputConfig, ok := data["output_config"]; ok {
		outputConfigSummary = "output_config=" + compactAnyForLog(outputConfig, 2000)
	}

	return fmt.Sprintf("%s %s %s %s %s", toolSummary, toolChoiceSummary, thinkingSummary, contextSummary, outputConfigSummary)
}

func summarizeClaudeToolsAnyForLog(tools any) string {
	rawTools, ok := tools.([]any)
	if !ok {
		encoded, err := common.Marshal(tools)
		if err != nil {
			return fmt.Sprintf("tools_unmarshal_failed type=%T marshal_error=%q", tools, err.Error())
		}
		if err := common.Unmarshal(encoded, &rawTools); err != nil {
			return fmt.Sprintf("tools_unmarshal_failed type=%T unmarshal_error=%q raw=%s", tools, err.Error(), truncateForLog(string(encoded), 4000))
		}
	}

	parts := make([]string, 0, len(rawTools))
	for i, tool := range rawTools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			parts = append(parts, fmt.Sprintf("#%d type=%T raw=%s", i, tool, compactAnyForLog(tool, 1000)))
			continue
		}
		parts = append(parts, fmt.Sprintf(
			"#%d type=%q name=%q max_uses=%v input_schema_present=%t description_present=%t raw=%s",
			i,
			strings.TrimSpace(common.Interface2String(toolMap["type"])),
			strings.TrimSpace(common.Interface2String(toolMap["name"])),
			toolMap["max_uses"],
			toolMap["input_schema"] != nil,
			strings.TrimSpace(common.Interface2String(toolMap["description"])) != "",
			compactAnyForLog(toolMap, 2000),
		))
	}
	return fmt.Sprintf("tools=%d [%s]", len(rawTools), strings.Join(parts, "; "))
}

func compactAnyForLog(value any, limit int) string {
	encoded, err := common.Marshal(value)
	if err != nil {
		return fmt.Sprintf("<marshal_error:%s>", err.Error())
	}
	return truncateForLog(string(encoded), limit)
}

func truncateForLog(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + fmt.Sprintf("...(truncated %d bytes)", len(value)-limit)
}

func summarizeClaudeUsageForLog(usage any) string {
	typedUsage, ok := usage.(*dto.Usage)
	if !ok || typedUsage == nil {
		return fmt.Sprintf("usage_type=%T", usage)
	}
	return fmt.Sprintf(
		"prompt=%d completion=%d total=%d cache_hit=%d claude_cache_5m=%d claude_cache_1h=%d semantic=%q source=%q",
		typedUsage.PromptTokens,
		typedUsage.CompletionTokens,
		typedUsage.TotalTokens,
		typedUsage.PromptCacheHitTokens,
		typedUsage.ClaudeCacheCreation5mTokens,
		typedUsage.ClaudeCacheCreation1hTokens,
		typedUsage.UsageSemantic,
		typedUsage.UsageSource,
	)
}

func ClaudeHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {

	info.InitChannelMeta(c)
	if info.ChannelType == constant.ChannelTypeXiaomi && info.RelayFormat == types.RelayFormatClaude {
		common.SetContextKey(c, constant.ContextKeyXiaomiClaudeDebug, true)
		logXiaomiClaudeTrace(c, "trace enabled immediately after InitChannelMeta: %s", summarizeClaudeChannelForLog(info))
	}

	claudeReq, ok := info.Request.(*dto.ClaudeRequest)

	if !ok {
		logClaudeMessagesError(c, "invalid request type: expected=*dto.ClaudeRequest actual=%T %s", info.Request, summarizeClaudeChannelForLog(info))
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected *dto.ClaudeRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	logClaudeMessagesDebug(c, "request received: %s %s %s", summarizeClaudeRequestForLog(claudeReq), summarizeClaudeChannelForLog(info), summarizeClaudeClientHeadersForLog(c))

	request, err := common.DeepCopy(claudeReq)
	if err != nil {
		logClaudeMessagesError(c, "deep copy request failed: %s %s", err.Error(), summarizeClaudeChannelForLog(info))
		return types.NewError(fmt.Errorf("failed to copy request to ClaudeRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		logClaudeMessagesError(c, "model mapping failed: %s request=%s %s", err.Error(), summarizeClaudeRequestForLog(request), summarizeClaudeChannelForLog(info))
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	logClaudeMessagesDebug(c, "model mapping finished: request=%s %s", summarizeClaudeRequestForLog(request), summarizeClaudeChannelForLog(info))

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		logClaudeMessagesError(c, "invalid adaptor: api_type=%d %s", info.ApiType, summarizeClaudeChannelForLog(info))
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	logClaudeMessagesDebug(c, "adaptor initialized: adaptor=%T %s", adaptor, summarizeClaudeChannelForLog(info))

	if request.MaxTokens == nil || *request.MaxTokens == 0 {
		defaultMaxTokens := uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(request.Model))
		request.MaxTokens = &defaultMaxTokens
		logClaudeMessagesDebug(c, "max_tokens defaulted: model=%q max_tokens=%d", request.Model, defaultMaxTokens)
	}

	if baseModel, effortLevel, ok := reasoning.TrimEffortSuffix(request.Model); ok && effortLevel != "" &&
		(strings.HasPrefix(request.Model, "claude-opus-4-6") || strings.HasPrefix(request.Model, "claude-opus-4-7")) {
		request.Model = baseModel
		request.Thinking = &dto.Thinking{
			Type: "adaptive",
		}
		request.OutputConfig = json.RawMessage(fmt.Sprintf(`{"effort":"%s"}`, effortLevel))
		if strings.HasPrefix(request.Model, "claude-opus-4-7") {
			// Opus 4.7 rejects non-default temperature/top_p/top_k with 400
			// and defaults display to "omitted"; restore the 4.6 visible summary.
			request.Thinking.Display = "summarized"
			request.Temperature = nil
			request.TopP = nil
			request.TopK = nil
		} else {
			request.Temperature = common.GetPointer[float64](1.0)
		}
		info.UpstreamModelName = request.Model
	} else if model_setting.GetClaudeSettings().ThinkingAdapterEnabled &&
		strings.HasSuffix(request.Model, "-thinking") {
		if request.Thinking == nil {
			baseModel := strings.TrimSuffix(request.Model, "-thinking")
			if strings.HasPrefix(baseModel, "claude-opus-4-7") {
				// Opus 4.7 rejects thinking.type="enabled"; use adaptive at high effort.
				request.Thinking = &dto.Thinking{Type: "adaptive", Display: "summarized"}
				request.OutputConfig = json.RawMessage(`{"effort":"high"}`)
				request.Temperature = nil
				request.TopP = nil
				request.TopK = nil
			} else {
				// 因为BudgetTokens 必须大于1024
				if request.MaxTokens == nil || *request.MaxTokens < 1280 {
					request.MaxTokens = common.GetPointer[uint](1280)
				}

				// BudgetTokens 为 max_tokens 的 80%
				request.Thinking = &dto.Thinking{
					Type:         "enabled",
					BudgetTokens: common.GetPointer[int](int(float64(*request.MaxTokens) * model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage)),
				}
				// TODO: 临时处理
				// https://docs.anthropic.com/en/docs/build-with-claude/extended-thinking#important-considerations-when-using-extended-thinking
				request.Temperature = common.GetPointer[float64](1.0)
			}
		}
		if !model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) {
			request.Model = strings.TrimSuffix(request.Model, "-thinking")
		}
		info.UpstreamModelName = request.Model
	}
	logClaudeMessagesDebug(c, "request normalized before system/pass-through handling: %s %s", summarizeClaudeRequestForLog(request), summarizeClaudeChannelForLog(info))

	if info.ChannelSetting.SystemPrompt != "" {
		if request.System == nil {
			request.SetStringSystem(info.ChannelSetting.SystemPrompt)
			logClaudeMessagesDebug(c, "channel system prompt injected into empty system")
		} else if info.ChannelSetting.SystemPromptOverride {
			common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
			if request.IsStringSystem() {
				existing := strings.TrimSpace(request.GetStringSystem())
				if existing == "" {
					request.SetStringSystem(info.ChannelSetting.SystemPrompt)
				} else {
					request.SetStringSystem(info.ChannelSetting.SystemPrompt + "\n" + existing)
				}
			} else {
				systemContents := request.ParseSystem()
				newSystem := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
				newSystem.SetText(info.ChannelSetting.SystemPrompt)
				if len(systemContents) == 0 {
					request.System = []dto.ClaudeMediaMessage{newSystem}
				} else {
					request.System = append([]dto.ClaudeMediaMessage{newSystem}, systemContents...)
				}
			}
			logClaudeMessagesDebug(c, "channel system prompt override applied: system_kind_after=%s", summarizeClaudeRequestForLog(request))
		}
	}

	if !model_setting.GetGlobalSettings().PassThroughRequestEnabled &&
		!info.ChannelSetting.PassThroughBodyEnabled &&
		!(info.ChannelType == constant.ChannelTypeXiaomi && info.RelayFormat == types.RelayFormatClaude) &&
		service.ShouldChatCompletionsUseResponsesGlobal(info.ChannelId, info.ChannelType, info.OriginModelName) {
		logClaudeMessagesDebug(c, "routing Claude messages via Responses compatibility path: %s", summarizeClaudeChannelForLog(info))
		openAIRequest, convErr := service.ClaudeToOpenAIRequest(*request, info)
		if convErr != nil {
			logClaudeMessagesError(c, "Claude to OpenAI request conversion failed: %s request=%s %s", convErr.Error(), summarizeClaudeRequestForLog(request), summarizeClaudeChannelForLog(info))
			return types.NewError(convErr, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		usage, newApiErr := chatCompletionsViaResponses(c, info, adaptor, openAIRequest)
		if newApiErr != nil {
			logClaudeMessagesError(c, "Responses compatibility relay failed: status=%d err=%s %s", newApiErr.StatusCode, newApiErr.Error(), summarizeClaudeChannelForLog(info))
			return newApiErr
		}

		logClaudeMessagesDebug(c, "Responses compatibility relay succeeded: usage=%s %s", summarizeClaudeUsageForLog(usage), summarizeClaudeChannelForLog(info))
		service.PostTextConsumeQuota(c, info, usage, nil)
		return nil
	}

	var requestBody io.Reader
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			logClaudeMessagesError(c, "read pass-through request body failed: %s %s", err.Error(), summarizeClaudeChannelForLog(info))
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		logClaudeMessagesDebug(c, "using pass-through request body: bytes=%d disk=%t %s", storage.Size(), storage.IsDisk(), summarizeClaudeChannelForLog(info))
		if shouldTraceXiaomiClaude(info) {
			bodyBytes, bodyErr := storage.Bytes()
			if bodyErr != nil {
				logXiaomiClaudeTrace(c, "pass-through upstream request body read failed: %s bytes=%d disk=%t", bodyErr.Error(), storage.Size(), storage.IsDisk())
			} else {
				logXiaomiClaudeTrace(c, "FINAL upstream request body source=pass-through %s details=%s", summarizeClaudeJSONBodyForLog(bodyBytes), summarizeClaudeJSONBodyDetailsForLog(bodyBytes))
				logXiaomiClaudeTrace(c, "FINAL upstream request body raw bytes=%d:\n%s", len(bodyBytes), string(bodyBytes))
			}
		}
		requestBody = common.ReaderOnly(storage)
	} else {
		convertedRequest, err := adaptor.ConvertClaudeRequest(c, info, request)
		if err != nil {
			logClaudeMessagesError(c, "convert Claude request failed: %s request=%s %s", err.Error(), summarizeClaudeRequestForLog(request), summarizeClaudeChannelForLog(info))
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			logClaudeMessagesError(c, "marshal converted Claude request failed: %s converted_type=%T %s", err.Error(), convertedRequest, summarizeClaudeChannelForLog(info))
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		logClaudeMessagesDebug(c, "converted Claude request built: converted_type=%T %s", convertedRequest, summarizeClaudeJSONBodyForLog(jsonData))

		// remove disabled fields for Claude API
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			logClaudeMessagesError(c, "remove disabled Claude fields failed: %s body=%s %s", err.Error(), summarizeClaudeJSONBodyForLog(jsonData), summarizeClaudeChannelForLog(info))
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		logClaudeMessagesDebug(c, "disabled Claude fields filtered: %s", summarizeClaudeJSONBodyForLog(jsonData))

		// apply param override
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				logClaudeMessagesError(c, "apply param override failed: %s audit=%v body=%s %s", err.Error(), info.ParamOverrideAudit, summarizeClaudeJSONBodyForLog(jsonData), summarizeClaudeChannelForLog(info))
				return newAPIErrorFromParamOverride(err)
			}
			logClaudeMessagesDebug(c, "param override applied: audit=%v body=%s", info.ParamOverrideAudit, summarizeClaudeJSONBodyForLog(jsonData))
		}

		if common.DebugEnabled || common.DebugTraceEnabled {
			logClaudeMessagesDebug(c, "sanitized upstream request body summary: %s", summarizeClaudeJSONBodyForLog(jsonData))
			logger.LogInfo(c, fmt.Sprintf("[claude messages raw] upstream request body bytes=%d:\n%s", len(jsonData), string(jsonData)))
		}
		if shouldTraceXiaomiClaude(info) {
			logXiaomiClaudeTrace(c, "FINAL upstream request body source=converted %s details=%s", summarizeClaudeJSONBodyForLog(jsonData), summarizeClaudeJSONBodyDetailsForLog(jsonData))
			logXiaomiClaudeTrace(c, "FINAL upstream request body raw bytes=%d:\n%s", len(jsonData), string(jsonData))
		}
		requestBody = bytes.NewBuffer(jsonData)
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")
	var httpResp *http.Response
	if common.DebugEnabled || common.DebugTraceEnabled {
		if requestURL, urlErr := adaptor.GetRequestURL(info); urlErr != nil {
			logClaudeMessagesError(c, "build upstream request url failed before dispatch: %s %s", urlErr.Error(), summarizeClaudeChannelForLog(info))
		} else {
			logClaudeMessagesDebug(c, "dispatching upstream request: method=%s url=%q stream=%t %s", c.Request.Method, requestURL, info.IsStream, summarizeClaudeChannelForLog(info))
		}
	}
	if shouldTraceXiaomiClaude(info) {
		if requestURL, urlErr := adaptor.GetRequestURL(info); urlErr != nil {
			logXiaomiClaudeTrace(c, "FINAL upstream request url build failed before dispatch: %s", urlErr.Error())
		} else {
			logXiaomiClaudeTrace(c, "FINAL upstream dispatch: method=%s url=%q stream=%t %s", c.Request.Method, requestURL, info.IsStream, summarizeClaudeChannelForLog(info))
		}
	}
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		logClaudeMessagesError(c, "upstream request failed: %s %s", err.Error(), summarizeClaudeChannelForLog(info))
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		logClaudeMessagesDebug(c, "upstream response received: status=%d content_type=%q request_id=%q stream=%t %s", httpResp.StatusCode, httpResp.Header.Get("Content-Type"), httpResp.Header.Get("request-id"), info.IsStream, summarizeClaudeChannelForLog(info))
		if httpResp.StatusCode != http.StatusOK {
			newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
			// reset status code 重置状态码
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			logClaudeMessagesError(c, "upstream returned non-200: upstream_status=%d mapped_status=%d err=%s %s", httpResp.StatusCode, newAPIError.StatusCode, newAPIError.Error(), summarizeClaudeChannelForLog(info))
			return newAPIError
		}
	} else {
		logClaudeMessagesError(c, "upstream response is nil without request error: %s", summarizeClaudeChannelForLog(info))
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	//log.Printf("usage: %v", usage)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		logClaudeMessagesError(c, "Claude response handling failed: status=%d err=%s %s", newAPIError.StatusCode, newAPIError.Error(), summarizeClaudeChannelForLog(info))
		return newAPIError
	}

	logClaudeMessagesDebug(c, "Claude messages relay succeeded: usage=%s %s", summarizeClaudeUsageForLog(usage), summarizeClaudeChannelForLog(info))
	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
	return nil
}
