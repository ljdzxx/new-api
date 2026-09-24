package relay

import (
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// The recovery stays inside one relay attempt so neither credentials nor the
// billing session are replaced. An encrypted-content rejection or HTTP 502/503
// with encrypted reasoning can be replayed before real output, at most once.
func doResponsesRequestWithReasoningRecovery(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, requestBody io.Reader, outboundBody []byte) (usage any, apiErr *types.NewAPIError) {
	recoveryAttempted := false
	defer func() {
		if common.GetContextKeyBool(c, constant.ContextKeyResponsesRecoveryNoRetry) && apiErr != nil {
			types.ErrOptionWithSkipRetry()(apiErr)
		}
		if recoveryAttempted {
			logger.LogWarn(c, fmt.Sprintf("responses reasoning recovery finished: channel_id=%d success=%t", info.ChannelId, apiErr == nil))
		}
	}()
	for {
		if recoveryAttempted {
			common.SetContextKey(c, constant.ContextKeyResponsesLogBadges, []string{"R1"})
		}
		resp, err := adaptor.DoRequest(c, info, requestBody)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		}
		var httpResp *http.Response
		if resp != nil {
			httpResp = resp.(*http.Response)
		}
		if httpResp != nil && httpResp.StatusCode != http.StatusOK {
			usage = nil
			apiErr = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		} else {
			usage, apiErr = adaptor.DoResponse(c, httpResp, info)
		}
		if !shouldRecoverResponsesReasoning(apiErr, httpResp, outboundBody) {
			return usage, apiErr
		}

		alreadyHandled := common.GetContextKeyBool(c, constant.ContextKeyResponsesRecoveryNoRetry)
		common.SetContextKey(c, constant.ContextKeyResponsesRecoveryNoRetry, true)
		if alreadyHandled || recoveryAttempted || info.RelayMode != relayconstant.RelayModeResponses ||
			helper.ResponsesStreamStarted(c) || c.Request.Context().Err() != nil || types.IsSkipRetryError(apiErr) ||
			common.GetContextKeyBool(c, constant.ContextKeyResponsesBillableStreamOutput) || responsesRecoveryHasUsage(usage) {
			return usage, apiErr
		}
		rebuilt, removed, rebuildErr := relaycommon.RebuildResponsesInputWithoutReasoning(outboundBody)
		if rebuildErr != nil || removed == 0 {
			logger.LogWarn(c, fmt.Sprintf("responses reasoning recovery skipped: channel_id=%d removed=%d reason=%v", info.ChannelId, removed, rebuildErr))
			return usage, apiErr
		}
		body, size, closer, bodyErr := relaycommon.NewOutboundJSONBody(rebuilt)
		if bodyErr != nil {
			logger.LogWarn(c, fmt.Sprintf("responses reasoning recovery body failed: channel_id=%d err=%v", info.ChannelId, bodyErr))
			return usage, apiErr
		}
		defer closer.Close()
		logger.LogWarn(c, fmt.Sprintf("responses reasoning recovery retry: channel_id=%d removed_reasoning=%d before_bytes=%d after_bytes=%d", info.ChannelId, removed, len(outboundBody), size))
		info.UpstreamRequestBodySize = size
		requestBody = body
		recoveryAttempted = true
	}
}

func shouldRecoverResponsesReasoning(apiErr *types.NewAPIError, resp *http.Response, body []byte) bool {
	if apiErr == nil {
		return false
	}
	if apiErr.GetErrorCode() == types.ErrorCodeInvalidEncryptedContent {
		return true
	}
	// Use the actual upstream HTTP status, not a mapped status or a locally
	// generated 502 from parsing a failed/malformed HTTP 200 stream.
	if resp == nil || (resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusServiceUnavailable) {
		return false
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return false
	}
	hasEncryptedReasoning := false
	input.ForEach(func(_, item gjson.Result) bool {
		ciphertext := item.Get("encrypted_content")
		hasEncryptedReasoning = item.Get("type").String() == "reasoning" && ciphertext.Type == gjson.String && ciphertext.Str != ""
		return !hasEncryptedReasoning
	})
	return hasEncryptedReasoning
}

func responsesRecoveryHasUsage(usage any) bool {
	u, ok := usage.(*dto.Usage)
	return ok && dto.HasOpenAIUsageTokens(u)
}
