package xiaomi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestXiaomiGetRequestURLUsesAnthropicMessagesForClaude(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.xiaomimimo.com",
		},
	}

	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.xiaomimimo.com/anthropic/v1/messages", requestURL)
}

func TestXiaomiGetRequestURLAppendsBetaQueryForClaude(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayFormat:       types.RelayFormatClaude,
		IsClaudeBetaQuery: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.xiaomimimo.com",
		},
	}

	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.xiaomimimo.com/anthropic/v1/messages?beta=true", requestURL)
}

func TestXiaomiGetRequestURLDoesNotSpecialCaseResponses(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses,
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.xiaomimimo.com",
		},
	}

	requestURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	require.NotEqual(t, "https://api.xiaomimimo.com/v1/chat/completions", requestURL)
}

func TestXiaomiSetupRequestHeaderForClaude(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx.Request.Header.Set("anthropic-version", "2023-06-01")
	ctx.Request.Header.Set("anthropic-beta", "code-tools-2025-01-01")
	ctx.Request.Header.Set("anthropic-dangerous-direct-browser-access", "true")
	ctx.Request.Header.Set("x-app", "cli")
	ctx.Request.Header.Set("x-claude-code-session-id", "session-1")
	ctx.Request.Header.Set("x-stainless-runtime", "node")
	ctx.Request.Header.Set("x-stainless-package-version", "0.81.0")
	ctx.Request.Header.Set("User-Agent", "claude-cli/2.1.133")
	ctx.Request.Header.Set("Authorization", "Bearer local-user-token")
	ctx.Request.Header.Set("x-api-key", "local-user-key")

	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: "mimo-key",
		},
	}

	headers := http.Header{}
	err := (&Adaptor{}).SetupRequestHeader(ctx, &headers, info)
	require.NoError(t, err)
	require.Equal(t, "mimo-key", headers.Get("api-key"))
	require.Equal(t, "2023-06-01", headers.Get("anthropic-version"))
	require.Equal(t, "code-tools-2025-01-01", headers.Get("anthropic-beta"))
	require.Equal(t, "true", headers.Get("anthropic-dangerous-direct-browser-access"))
	require.Equal(t, "cli", headers.Get("x-app"))
	require.Equal(t, "session-1", headers.Get("x-claude-code-session-id"))
	require.Equal(t, "node", headers.Get("x-stainless-runtime"))
	require.Equal(t, "0.81.0", headers.Get("x-stainless-package-version"))
	require.Equal(t, "claude-cli/2.1.133", headers.Get("User-Agent"))
	require.Empty(t, headers.Get("x-api-key"))
	require.Empty(t, headers.Get("Authorization"))
}

func TestXiaomiConvertClaudeRequestDefaultsToolTypeToCustom(t *testing.T) {
	t.Parallel()

	req := &dto.ClaudeRequest{
		Tools: []dto.Tool{
			{
				Name: "get_weather",
				InputSchema: map[string]any{
					"type": "object",
				},
			},
		},
	}
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude}

	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, info, req)
	require.NoError(t, err)

	claudeReq, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok)
	tools, ok := claudeReq.Tools.([]dto.Tool)
	require.True(t, ok)
	require.Len(t, tools, 1)
	require.Equal(t, "custom", tools[0].Type)
}

func TestXiaomiConvertClaudeRequestPreservesExplicitToolType(t *testing.T) {
	t.Parallel()

	req := &dto.ClaudeRequest{
		Tools: []map[string]any{
			{
				"name": "web_search",
				"type": "web_search_20250305",
			},
		},
	}
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude}

	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, info, req)
	require.NoError(t, err)

	claudeReq, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok)
	tools, ok := claudeReq.Tools.([]map[string]any)
	require.True(t, ok)
	require.Len(t, tools, 1)
	require.Equal(t, "web_search_20250305", tools[0]["type"])
}

func TestSummarizeClaudeToolsForLogIncludesWebSearchShape(t *testing.T) {
	t.Parallel()

	summary := summarizeClaudeToolsForLog([]map[string]any{
		{
			"type":     "web_search_20250305",
			"name":     "web_search",
			"max_uses": 8,
		},
	})

	require.Contains(t, summary, "web_search_20250305")
	require.Contains(t, summary, "web_search")
	require.Contains(t, summary, "8")
}

func TestXiaomiConvertClaudeRequestDefaultsToolTypeForAnySliceValue(t *testing.T) {
	t.Parallel()

	req := &dto.ClaudeRequest{
		Tools: []any{
			dto.Tool{
				Name: "get_weather",
				InputSchema: map[string]any{
					"type": "object",
				},
			},
		},
	}
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude}

	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, info, req)
	require.NoError(t, err)

	claudeReq, ok := converted.(*dto.ClaudeRequest)
	require.True(t, ok)
	tools, ok := claudeReq.Tools.([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)

	tool, ok := tools[0].(dto.Tool)
	require.True(t, ok)
	require.Equal(t, "custom", tool.Type)
}

func TestXiaomiConvertOpenAIResponsesRequestRejectsResponsesProtocol(t *testing.T) {
	t.Parallel()

	request := dto.OpenAIResponsesRequest{
		Tools: jsonMustRaw(t, `[{"type":"function","name":"get_weather"}]`),
	}
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 3,
		},
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, info, request)
	require.Nil(t, converted)
	require.Error(t, err)
	require.Equal(t, "渠道 #3 暂不支持走OpenAI /v1/responses 协议", err.Error())
}

func jsonMustRaw(t *testing.T, s string) json.RawMessage {
	t.Helper()
	return json.RawMessage(s)
}
