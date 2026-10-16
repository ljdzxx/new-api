package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestGeminiSearchBillingSelectsOneUnit 验证查询数与 grounded prompt 不会叠加收费。
func TestGeminiSearchBillingSelectsOneUnit(t *testing.T) {
	for _, tc := range []struct {
		name, model, key string
		count, quota     int
	}{
		{"gemini3_query", "gemini-3-flash-preview", billing_policy.ToolGeminiGoogleSearch, 2, 14000},
		{"gemini25_prompt", "gemini-2.5-pro", billing_policy.ToolGeminiGroundedPrompt, 1, 17500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				OriginModelName: tc.model,
				StartTime:       time.Now(),
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: tc.model},
				PriceData:       types.PriceData{UsePrice: true, ModelPrice: 0, GlobalModelRatio: 1, GlobalRatioConfigSnapshot: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
			}
			info.SetBillableToolCount(relaycommon.GeminiGoogleSearchQueryTool, 2)
			info.SetBillableToolCount(relaycommon.GeminiGoogleSearchGroundedPromptTool, 1)
			summary := calculateTextQuotaSummary(ctx, info, &dto.Usage{})
			require.Equal(t, tc.key, summary.GeminiSearchBillingKey)
			require.Equal(t, tc.count, summary.GeminiSearchCallCount)
			require.Equal(t, tc.quota, summary.Quota)
			require.Equal(t, 2, summary.GeminiSearchQueryCount)
			require.Equal(t, 1, summary.GeminiGroundedPromptCount)
			usage := billingPolicyToolUsage(ctx, summary)
			require.Equal(t, int64(tc.count), usage[tc.key])
			require.Len(t, usage, 1)
		})
	}
}

// TestGeminiSearchBillingPolicyPrice 验证激活策略覆盖默认工具价。
func TestGeminiSearchBillingPolicyPrice(t *testing.T) {
	policy := fullTextBillingPolicy()
	policy.Prices = billing_policy.Prices{Input: "0", Output: "0"}
	policy.Tools = map[string]billing_policy.ToolPrice{
		billing_policy.ToolGeminiGoogleSearch:   {Unit: "per_thousand_calls", Price: "20"},
		billing_policy.ToolGeminiGroundedPrompt: {Unit: "per_thousand_calls", Price: "35"},
	}
	installActiveBillingPolicyForTest(t, "gemini-search-policy", policy)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-search-policy", StartTime: time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-3-flash-preview"},
		PriceData:   types.PriceData{GlobalModelRatio: 1, GlobalRatioConfigSnapshot: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
	}
	info.SetBillableToolCount(relaycommon.GeminiGoogleSearchQueryTool, 2)
	info.SetBillableToolCount(relaycommon.GeminiGoogleSearchGroundedPromptTool, 1)
	summary := calculateTextQuotaSummary(ctx, info, &dto.Usage{})
	require.NoError(t, summary.PolicyError)
	require.NotNil(t, summary.PolicyCalculation)
	require.Equal(t, 20000, summary.Quota)
	require.Equal(t, 20.0, summary.GeminiSearchPrice)
	require.Equal(t, int64(2), summary.PolicyCalculation.Usage.ToolUsage[billing_policy.ToolGeminiGoogleSearch])
	require.NotContains(t, summary.PolicyCalculation.Usage.ToolUsage, billing_policy.ToolGeminiGroundedPrompt)
}

// TestGeminiSearchCountIsBounded 验证上游异常计数不能放大费用或溢出。
func TestGeminiSearchCountIsBounded(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	info.SetBillableToolCount(relaycommon.GeminiGoogleSearchQueryTool, relaycommon.MaxGeminiSearchCount+1)
	require.Equal(t, relaycommon.MaxGeminiSearchCount, info.ResponsesUsageInfo.BuiltInTools[relaycommon.GeminiGoogleSearchQueryTool].CallCount)
	info.SetBillableToolCount(relaycommon.GeminiGoogleSearchQueryTool, -1)
	require.Equal(t, relaycommon.MaxGeminiSearchCount, info.ResponsesUsageInfo.BuiltInTools[relaycommon.GeminiGoogleSearchQueryTool].CallCount)
	info.SetBillableToolCount(relaycommon.GeminiGoogleSearchQueryTool, 2)
	require.Equal(t, 2, info.ResponsesUsageInfo.BuiltInTools[relaycommon.GeminiGoogleSearchQueryTool].CallCount)
}
