package relay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func ResponsesHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	// Error logs are per channel attempt. A later successful channel must not
	// inherit the previous attempt's encrypted-input failure badges.
	common.SetContextKey(c, appconstant.ContextKeyResponsesLogBadges, []string(nil))
	info.InitChannelMeta(c)
	logger.LogInfo(c, fmt.Sprintf(
		"responses relay selected channel: request_path=%q relay_mode=%d channel_id=%d channel_type=%d api_type=%d origin_model=%q upstream_model=%q pass_through=%t channel_pass_through=%t",
		c.Request.URL.Path,
		info.RelayMode,
		info.ChannelId,
		info.ChannelType,
		info.ApiType,
		info.OriginModelName,
		info.UpstreamModelName,
		model_setting.GetGlobalSettings().PassThroughRequestEnabled,
		info.ChannelSetting.PassThroughBodyEnabled,
	))
	if info.ChannelType == appconstant.ChannelTypeXiaomi || info.ApiType == appconstant.APITypeXiaomi {
		logger.LogWarn(c, fmt.Sprintf(
			"responses relay rejected unsupported xiaomi channel: channel_id=%d channel_type=%d api_type=%d origin_model=%q upstream_model=%q",
			info.ChannelId,
			info.ChannelType,
			info.ApiType,
			info.OriginModelName,
			info.UpstreamModelName,
		))
		return types.NewErrorWithStatusCode(
			service.UnsupportedOpenAIResponsesProtocolError(info.ChannelId),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		switch info.ApiType {
		case appconstant.APITypeOpenAI, appconstant.APITypeCodex:
		default:
			return types.NewErrorWithStatusCode(
				fmt.Errorf("unsupported endpoint %q for api type %d", "/v1/responses/compact", info.ApiType),
				types.ErrorCodeInvalidRequest,
				http.StatusBadRequest,
				types.ErrOptionWithSkipRetry(),
			)
		}
	}

	var responsesReq *dto.OpenAIResponsesRequest
	switch req := info.Request.(type) {
	case *dto.OpenAIResponsesRequest:
		responsesReq = req
	case *dto.OpenAIResponsesCompactionRequest:
		responsesReq = openAIResponsesRequestFromCompaction(req)
	default:
		return types.NewErrorWithStatusCode(
			fmt.Errorf("invalid request type, expected dto.OpenAIResponsesRequest or dto.OpenAIResponsesCompactionRequest, got %T", info.Request),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	request, err := common.DeepCopy(responsesReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to GeneralOpenAIRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	// The compaction endpoint does not forward reasoning to the upstream API,
	// but the client-provided effort is still needed for conditional model
	// mapping (for example, "model,xhigh"). Keep it through mapping and strip
	// it before conversion so the upstream compaction payload remains whitelisted.
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		request.Reasoning = nil
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	var requestBody io.Reader
	var outboundRequestBody []byte
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		outboundRequestBody, err = storage.Bytes()
		if err != nil {
			return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		mappedBody, mapErr := helper.ApplyModelMappingToPassthroughBody(outboundRequestBody, info, request)
		if mapErr != nil {
			return types.NewError(mapErr, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		bodyChanged := !bytes.Equal(mappedBody, outboundRequestBody)
		outboundRequestBody = mappedBody
		removedImageTools := 0
		if relaycommon.ShouldStripImageGeneration(info.RelayMode) {
			outboundRequestBody, removedImageTools, err = relaycommon.StripImageGenerationTool(outboundRequestBody)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
		}
		sanitizedBody, removedIDs, err := relaycommon.SanitizeInvalidResponsesItemIDs(outboundRequestBody)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		if removedIDs == 0 && removedImageTools == 0 && !bodyChanged {
			info.UpstreamRequestBodySize = storage.Size()
			requestBody = common.ReaderOnly(storage)
		} else {
			outboundRequestBody = sanitizedBody
			body, size, closer, bodyErr := relaycommon.NewOutboundJSONBody(sanitizedBody)
			if bodyErr != nil {
				return types.NewError(bodyErr, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
			defer closer.Close()
			info.UpstreamRequestBodySize = size
			requestBody = body
			if removedIDs > 0 {
				logger.LogWarn(c, fmt.Sprintf(
					"[responses item_id sanitize] removed=%d passthrough=true request_path=%q relay_mode=%d channel_id=%d channel_type=%d api_type=%d origin_model=%q upstream_model=%q",
					removedIDs,
					c.Request.URL.Path,
					info.RelayMode,
					info.ChannelId,
					info.ChannelType,
					info.ApiType,
					info.OriginModelName,
					info.UpstreamModelName,
				))
			}
		}
	} else {
		convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// remove disabled fields for OpenAI Responses API
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// apply param override
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return newAPIErrorFromParamOverride(err)
			}
		}
		if relaycommon.ShouldStripImageGeneration(info.RelayMode) {
			jsonData, _, err = relaycommon.StripImageGenerationTool(jsonData)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
		}

		originalJSONSize := len(jsonData)
		jsonData, removed, err := relaycommon.SanitizeInvalidResponsesEncryptedContent(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		if removed > 0 {
			logger.LogError(c, fmt.Sprintf(
				"[responses encrypted_content sanitize] fixed=%d request_path=%q relay_mode=%d channel_id=%d channel_type=%d api_type=%d origin_model=%q upstream_model=%q before_bytes=%d after_bytes=%d",
				removed,
				c.Request.URL.Path,
				info.RelayMode,
				info.ChannelId,
				info.ChannelType,
				info.ApiType,
				info.OriginModelName,
				info.UpstreamModelName,
				originalJSONSize,
				len(jsonData),
			))
		}
		jsonData, removedIDs, err := relaycommon.SanitizeInvalidResponsesItemIDs(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		if removedIDs > 0 {
			logger.LogWarn(c, fmt.Sprintf(
				"[responses item_id sanitize] removed=%d passthrough=false request_path=%q relay_mode=%d channel_id=%d channel_type=%d api_type=%d origin_model=%q upstream_model=%q",
				removedIDs,
				c.Request.URL.Path,
				info.RelayMode,
				info.ChannelId,
				info.ChannelType,
				info.ApiType,
				info.OriginModelName,
				info.UpstreamModelName,
			))
		}

		if common.DebugEnabled {
			println("requestBody: ", string(jsonData))
		}
		outboundRequestBody = jsonData
		body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		defer closer.Close()
		jsonData = nil
		info.UpstreamRequestBodySize = size
		requestBody = body
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")
	usage, newAPIError := doResponsesRequestWithReasoningRecovery(c, info, adaptor, requestBody, outboundRequestBody)
	if newAPIError != nil {
		if newAPIError.GetErrorCode() != types.ErrorCodeClientDisconnected {
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		}
		relaycommon.MarkResponsesEncryptedFailure(c, responsesReq.Input, newAPIError)
		if isEncryptedContentRelayError(newAPIError) {
			logger.LogError(c, fmt.Sprintf("responses encrypted_content upstream_error: channel_id=%d status_code=%d error=%q request_body_bytes=%d", info.ChannelId, newAPIError.StatusCode, newAPIError.Error(), len(outboundRequestBody)))
		}
		// Preserve partial-stream settlement. The outer error handler's Refund
		// is idempotent after BillingSession.Settle; no new attempt may follow.
		if info.IsStream && helper.ResponsesStreamStarted(c) {
			types.ErrOptionWithSkipRetry()(newAPIError)
			if partialUsage, ok := usage.(*dto.Usage); ok && partialUsage != nil {
				service.PostTextConsumeQuota(c, info, partialUsage, nil)
			}
		}
		return newAPIError
	}

	usageDto := usage.(*dto.Usage)
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		originModelName := info.OriginModelName
		originPriceData := info.PriceData

		_, err := helper.ModelPriceHelper(c, info, info.GetEstimatePromptTokens(), &types.TokenCountMeta{})
		if err != nil {
			info.OriginModelName = originModelName
			info.PriceData = originPriceData
			return types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry())
		}
		service.PostTextConsumeQuota(c, info, usageDto, nil)

		info.OriginModelName = originModelName
		info.PriceData = originPriceData
		return nil
	}

	if strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
		service.PostAudioConsumeQuota(c, info, usageDto, "")
	} else {
		service.PostTextConsumeQuota(c, info, usageDto, nil)
	}
	return nil
}

func openAIResponsesRequestFromCompaction(req *dto.OpenAIResponsesCompactionRequest) *dto.OpenAIResponsesRequest {
	// Only fields supported by POST /v1/responses/compact are forwarded.
	// Codex-parity fields such as tools, reasoning, and text remain accepted by
	// the client DTO for compatibility, but are intentionally not sent upstream.
	var reasoning *dto.Reasoning
	if req.Reasoning != nil {
		// Do not alias the client request: model mapping may override the effort
		// on this internal copy before the field is stripped for upstream.
		reasoningValue := *req.Reasoning
		reasoning = &reasoningValue
	}
	return &dto.OpenAIResponsesRequest{
		Model:                req.Model,
		Input:                req.Input,
		Instructions:         req.Instructions,
		PreviousResponseID:   req.PreviousResponseID,
		ParallelToolCalls:    req.ParallelToolCalls,
		ServiceTier:          req.ServiceTier,
		PromptCacheKey:       req.PromptCacheKey,
		PromptCacheOptions:   req.PromptCacheOptions,
		PromptCacheRetention: req.PromptCacheRetention,
		Reasoning:            reasoning,
	}
}

func isEncryptedContentRelayError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "encrypted_content") ||
		strings.Contains(msg, "encrypted content") ||
		strings.Contains(msg, "could not be decrypted")
}
