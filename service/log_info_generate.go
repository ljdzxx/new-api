package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func scaleLogTokens(tokens int, relayInfo *relaycommon.RelayInfo) int {
	if relayInfo == nil {
		return tokens
	}
	return relayhelper.ScaleTokensByGlobalModelRatio(tokens, relayInfo.PriceData.GlobalModelRatio)
}

func attachGroupRatioBreakdown(other map[string]interface{}, info types.GroupRatioInfo) {
	if other == nil {
		return
	}
	other["group_ratio"] = info.GroupRatio
	other["base_group_ratio"] = info.BaseGroupRatio
	other["user_level_ratio"] = info.UserLevelRatio
	other["user_level_id"] = info.UserLevelID
	other["effective_group_ratio"] = info.GroupRatio
	other["group_ratio_source"] = "group"
	if info.HasSpecialRatio {
		other["group_ratio_source"] = "user_group_special"
		other["user_group_ratio"] = info.GroupRatio
		other["user_group_base_ratio"] = info.GroupSpecialRatio
	}
}

// attachQuotaSaturationToOther nests a quota saturation marker under
// other.admin_info.quota_saturation. Nesting under admin_info makes it
// admin-only for free, since model.formatUserLogs strips the whole admin_info
// object for non-admin viewers. Creates admin_info if absent. No-op when the
// clamp is nil (the common case: no saturation happened).
func attachQuotaSaturationToOther(other map[string]interface{}, clamp *common.QuotaClamp) {
	if clamp == nil || other == nil {
		return
	}
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok || adminInfo == nil {
		adminInfo = map[string]interface{}{}
		other["admin_info"] = adminInfo
	}
	adminInfo["quota_saturation"] = clamp.AuditMap()
}

// attachQuotaSaturation records the request's quota clamp (if any) onto the
// consume log's other.admin_info and emits a request-correlated backend audit
// line. Called right before RecordConsumeLog on the text/audio/wss paths.
func attachQuotaSaturation(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil {
		return
	}
	clamp := relayInfo.QuotaClamp
	if clamp == nil {
		return
	}
	attachQuotaSaturationToOther(other, clamp)
	logger.LogWarn(ctx, fmt.Sprintf("quota saturation on consume log: op=%s kind=%s original=%g clamped=%d user=%d model=%s",
		clamp.Op, clamp.Kind, clamp.Original, clamp.Clamped, relayInfo.UserId, relayInfo.OriginModelName))
}

func appendRequestPath(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if other == nil {
		return
	}
	if ctx != nil && ctx.Request != nil && ctx.Request.URL != nil {
		if path := ctx.Request.URL.Path; path != "" {
			other["request_path"] = path
			return
		}
	}
	if relayInfo != nil && relayInfo.RequestURLPath != "" {
		path := relayInfo.RequestURLPath
		if idx := strings.Index(path, "?"); idx != -1 {
			path = path[:idx]
		}
		other["request_path"] = path
	}
}

func GenerateTextOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelRatio, groupRatio, completionRatio float64,
	cacheTokens int, cacheRatio float64, modelPrice float64, userGroupRatio float64) map[string]interface{} {
	other := make(map[string]interface{})
	other["model_ratio"] = modelRatio
	attachGroupRatioBreakdown(other, relayInfo.PriceData.GroupRatioInfo)
	other["system_global_model_ratio"] = relayInfo.PriceData.SystemGlobalModelRatio
	other["user_global_model_ratio"] = relayInfo.PriceData.UserGlobalModelRatio
	other["channel_model_ratio"] = relayInfo.PriceData.ChannelModelRatio
	other["global_model_ratio"] = relayInfo.PriceData.GlobalModelRatio
	other["completion_ratio"] = completionRatio
	other["cache_tokens"] = cacheTokens
	other["cache_ratio"] = cacheRatio
	other["model_price"] = modelPrice
	other["use_price"] = relayInfo.PriceData.UsePrice
	if relayInfo.PriceData.UsePrice {
		other["billing_mode"] = "per_request"
	} else {
		other["billing_mode"] = "per_token"
	}
	if !relayInfo.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = userGroupRatio
	}
	other["frt"] = float64(relayInfo.FirstResponseTime.UnixMilli() - relayInfo.StartTime.UnixMilli())
	if tier := ctx.GetString("billing_policy_tier"); tier != "" {
		other["billing_mode"] = "tiered"
		other["billing_tier"] = tier
	}
	if adjustments, exists := ctx.Get("billing_policy_adjustments"); exists {
		other["billing_policy_adjustments"] = adjustments
	}
	if relayInfo.ReasoningEffort != "" {
		other["reasoning_effort"] = relayInfo.ReasoningEffort
	}
	if relayInfo.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = relayInfo.UpstreamModelName
	}

	isSystemPromptOverwritten := common.GetContextKeyBool(ctx, constant.ContextKeySystemPromptOverride)
	if isSystemPromptOverwritten {
		other["is_system_prompt_overwritten"] = true
	}

	adminInfo := make(map[string]interface{})
	adminInfo["use_channel"] = ctx.GetStringSlice("use_channel")
	isMultiKey := common.GetContextKeyBool(ctx, constant.ContextKeyChannelIsMultiKey)
	if isMultiKey {
		adminInfo["is_multi_key"] = true
		adminInfo["multi_key_index"] = common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex)
	}

	isLocalCountTokens := common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens)
	if isLocalCountTokens {
		adminInfo["local_count_tokens"] = isLocalCountTokens
	}

	AppendChannelAffinityAdminInfo(ctx, adminInfo)

	other["admin_info"] = adminInfo
	appendRequestPath(ctx, relayInfo, other)
	appendRequestConversionChain(relayInfo, other)
	appendFinalRequestFormat(relayInfo, other)
	appendBillingInfo(relayInfo, other)
	appendParamOverrideInfo(relayInfo, other)
	return other
}

type billingPolicyLogSnapshot struct {
	SchemaVersion              int                               `json:"schema_version"`
	Revision                   int64                             `json:"revision"`
	Model                      string                            `json:"model"`
	Policy                     *billing_policy.Policy            `json:"policy,omitempty"`
	Calculation                billing_policy.BillingCalculation `json:"calculation"`
	ActualQuota                int                               `json:"actual_quota"`
	PreConsumedQuota           int                               `json:"pre_consumed_quota"`
	GroupRatio                 float64                           `json:"group_ratio"`
	GlobalModelRatio           float64                           `json:"global_model_ratio"`
	PolicyAdjustmentMultiplier float64                           `json:"policy_adjustment_multiplier"`
	OtherRatioMultiplier       float64                           `json:"other_ratio_multiplier"`
	OtherRatios                map[string]float64                `json:"other_ratios,omitempty"`
	AdditionalCharges          []billingPolicyAdditionalCharge   `json:"additional_charges"`
	AdditionalChargesUSD       string                            `json:"additional_charges_usd"`
	BillableSubtotalUSD        string                            `json:"billable_subtotal_usd"`
}

type billingPolicyAdditionalCharge struct {
	Field     string `json:"field"`
	Units     int64  `json:"units"`
	Unit      string `json:"unit"`
	UnitPrice string `json:"unit_price"`
	CostUSD   string `json:"cost_usd"`
}

func buildBillingPolicyAdditionalCharges(summary textQuotaSummary) ([]billingPolicyAdditionalCharge, decimal.Decimal) {
	charges := make([]billingPolicyAdditionalCharge, 0, 5)
	total := decimal.Zero
	appendCharge := func(field string, units int64, unit, unitPrice string, cost decimal.Decimal) {
		if units <= 0 || !cost.IsPositive() {
			return
		}
		charges = append(charges, billingPolicyAdditionalCharge{
			Field: field, Units: units, Unit: unit, UnitPrice: unitPrice, CostUSD: cost.String(),
		})
		total = total.Add(cost)
	}
	perThousand := decimal.NewFromInt(1000)
	perMillion := decimal.NewFromInt(1_000_000)
	if summary.WebSearchCallCount > 0 && summary.WebSearchPrice > 0 {
		price := decimal.NewFromFloat(summary.WebSearchPrice)
		appendCharge("web_search", int64(summary.WebSearchCallCount), "per_thousand_calls", price.String(),
			price.Mul(decimal.NewFromInt(int64(summary.WebSearchCallCount))).Div(perThousand))
	}
	if summary.ClaudeWebSearchCallCount > 0 && summary.ClaudeWebSearchPrice > 0 {
		price := decimal.NewFromFloat(summary.ClaudeWebSearchPrice)
		appendCharge("claude_web_search", int64(summary.ClaudeWebSearchCallCount), "per_thousand_calls", price.String(),
			price.Mul(decimal.NewFromInt(int64(summary.ClaudeWebSearchCallCount))).Div(perThousand))
	}
	if summary.FileSearchCallCount > 0 && summary.FileSearchPrice > 0 {
		price := decimal.NewFromFloat(summary.FileSearchPrice)
		appendCharge("file_search", int64(summary.FileSearchCallCount), "per_thousand_calls", price.String(),
			price.Mul(decimal.NewFromInt(int64(summary.FileSearchCallCount))).Div(perThousand))
	}
	if summary.AudioTokens > 0 && summary.AudioInputPrice > 0 {
		price := decimal.NewFromFloat(summary.AudioInputPrice)
		appendCharge("audio_input", int64(summary.AudioTokens), "per_million_tokens", price.String(),
			price.Mul(decimal.NewFromInt(int64(summary.AudioTokens))).Div(perMillion))
	}
	if summary.ImageGenerationCallPrice > 0 {
		price := decimal.NewFromFloat(summary.ImageGenerationCallPrice)
		appendCharge("image_generation", 1, "per_request", price.String(), price)
	}
	if len(charges) == 0 {
		return nil, decimal.Zero
	}
	return charges, total
}

func billingPolicyBusinessRatios(priceData types.PriceData) (map[string]float64, float64) {
	ratios := priceData.OtherRatios()
	if len(ratios) == 0 {
		return nil, 1
	}
	multiplier := 1.0
	for _, ratio := range ratios {
		multiplier *= ratio
	}
	return ratios, multiplier
}

func attachBillingPolicySnapshot(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, other map[string]interface{}) {
	if relayInfo == nil || other == nil || !billing_policy.IsActive() {
		return
	}
	policy, ok := billing_policy.Resolve(relayInfo.OriginModelName)
	if !ok {
		if common.DebugTraceEnabledForContext(ctx) {
			logger.LogWarn(ctx, "[billing-policy][settlement] active policy missing for model "+relayInfo.OriginModelName)
		}
		return
	}
	effectivePrices, _, effectivePricesErr := billing_policy.EffectivePricesForUsage(policy, billing_policy.Usage{
		InputTotalTokens: summary.PolicyInputTotalTokens, OutputTotalTokens: summary.PolicyOutputTotalTokens,
	})

	cacheWriteRemaining := summary.CacheCreationTokens - summary.CacheCreationTokens5m - summary.CacheCreationTokens1h
	if cacheWriteRemaining < 0 {
		cacheWriteRemaining = 0
	}
	inputTokens := summary.PromptTokens
	cacheReadTokens := summary.CacheTokens
	if !summary.IsClaudeUsageSemantic {
		inputTokens -= summary.CacheTokens + summary.CacheCreationTokens + summary.ImageTokens
		if summary.AudioInputPrice > 0 || (effectivePricesErr == nil && strings.TrimSpace(effectivePrices.AudioInput) != "") {
			inputTokens -= summary.AudioTokens
		}
		if inputTokens < 0 {
			inputTokens = 0
		}
		cacheWriteRemaining = summary.CacheCreationTokens
	}
	outputTokens := summary.CompletionTokens
	if effectivePricesErr == nil && strings.TrimSpace(effectivePrices.AudioOutput) != "" {
		outputTokens -= summary.AudioOutputTokens
		if outputTokens < 0 {
			outputTokens = 0
		}
	}

	calculation := billing_policy.BillingCalculation{}
	if summary.PolicyCalculation != nil {
		calculation = *summary.PolicyCalculation
	} else {
		var err error
		calculation, err = billing_policy.CalculateBilling(policy, billing_policy.BillingUsage{
			TierInputTotalTokens:  summary.PolicyInputTotalTokens,
			TierOutputTotalTokens: summary.PolicyOutputTotalTokens,
			InputTokens:           int64(inputTokens),
			OutputTokens:          int64(outputTokens),
			CacheReadTokens:       int64(cacheReadTokens),
			CacheWriteTokens:      int64(cacheWriteRemaining),
			CacheWrite5mTokens:    int64(summary.CacheCreationTokens5m),
			CacheWrite1hTokens:    int64(summary.CacheCreationTokens1h),
			ImageInputTokens:      int64(summary.ImageTokens),
			AudioInputTokens:      int64(summary.AudioTokens),
			AudioOutputTokens:     int64(summary.AudioOutputTokens),
		}, billingPolicyRequestContext(ctx))
		if err != nil {
			if common.DebugTraceEnabledForContext(ctx) {
				logger.LogWarn(ctx, "[billing-policy][settlement] calculation failed: "+err.Error())
			}
			return
		}
	}
	config := billing_policy.GetConfig()
	otherRatios, otherRatioMultiplier := billingPolicyBusinessRatios(relayInfo.PriceData)
	additionalCharges, additionalChargesUSD := buildBillingPolicyAdditionalCharges(summary)
	modelTotalUSD, err := decimal.NewFromString(calculation.TotalUSD)
	if err != nil {
		modelTotalUSD = decimal.Zero
	}
	snapshot := billingPolicyLogSnapshot{
		SchemaVersion:              config.SchemaVersion,
		Revision:                   config.Revision,
		Model:                      relayInfo.OriginModelName,
		Calculation:                calculation,
		ActualQuota:                summary.Quota,
		PreConsumedQuota:           relayInfo.FinalPreConsumedQuota,
		GroupRatio:                 summary.GroupRatio,
		GlobalModelRatio:           summary.GlobalModelRatio,
		PolicyAdjustmentMultiplier: relayInfo.PriceData.EffectivePolicyAdjustmentMultiplier(),
		OtherRatioMultiplier:       otherRatioMultiplier,
		OtherRatios:                otherRatios,
		AdditionalCharges:          additionalCharges,
		AdditionalChargesUSD:       additionalChargesUSD.String(),
		BillableSubtotalUSD:        modelTotalUSD.Add(additionalChargesUSD).String(),
	}
	if common.DebugTraceEnabledForContext(ctx) {
		snapshot.Policy = &policy
	}
	other["billing_policy"] = snapshot
	other["billing_mode"] = policy.Mode
	if calculation.TierID != "" {
		other["billing_tier"] = calculation.TierID
	}
	if common.DebugTraceEnabledForContext(ctx) {
		logger.LogInfo(ctx, "[billing-policy][settlement] snapshot="+common.GetJsonString(snapshot))
	}
}

func billingPolicyRequestContext(ctx *gin.Context) billing_policy.RequestContext {
	requestContext := billing_policy.RequestContext{}
	if ctx == nil || ctx.Request == nil {
		return requestContext
	}
	requestContext.Headers = make(map[string]string, len(ctx.Request.Header))
	for key, values := range ctx.Request.Header {
		if len(values) > 0 {
			requestContext.Headers[strings.ToLower(key)] = values[0]
		}
	}
	if storage, err := common.GetBodyStorage(ctx); err == nil && storage != nil {
		requestContext.Body, _ = storage.Bytes()
	}
	return requestContext
}

func attachAudioBillingPolicySnapshot(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, inputText, outputText, inputAudio, outputAudio, tierInputTotal, tierOutputTotal, quota int, other map[string]interface{}) {
	if relayInfo == nil {
		return
	}
	globalRatio := relayInfo.PriceData.GlobalModelRatio
	summary := textQuotaSummary{
		PromptTokens:            relayhelper.ScaleTokensByGlobalModelRatio(inputText, globalRatio),
		CompletionTokens:        relayhelper.ScaleTokensByGlobalModelRatio(outputText, globalRatio),
		AudioTokens:             relayhelper.ScaleTokensByGlobalModelRatio(inputAudio, globalRatio),
		AudioOutputTokens:       relayhelper.ScaleTokensByGlobalModelRatio(outputAudio, globalRatio),
		PolicyInputTotalTokens:  int64(tierInputTotal),
		PolicyOutputTotalTokens: int64(tierOutputTotal),
		ModelName:               relayInfo.OriginModelName,
		ModelRatio:              relayInfo.PriceData.ModelRatio,
		GroupRatio:              relayInfo.PriceData.GroupRatioInfo.GroupRatio,
		GlobalModelRatio:        globalRatio,
		Quota:                   quota,
	}
	attachBillingPolicySnapshot(ctx, relayInfo, summary, other)
}

func attachPerRequestBillingPolicySnapshot(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, quota int, other map[string]interface{}) {
	if relayInfo == nil || other == nil || !billing_policy.IsActive() {
		return
	}
	policy, ok := billing_policy.Resolve(relayInfo.OriginModelName)
	if !ok || policy.Mode != "per_request" {
		return
	}
	calculation, err := billing_policy.CalculateBilling(policy, billing_policy.BillingUsage{}, billingPolicyRequestContext(ctx))
	if err != nil {
		if common.DebugTraceEnabledForContext(ctx) {
			logger.LogWarn(ctx, "[billing-policy][per-request] calculation failed: "+err.Error())
		}
		return
	}
	config := billing_policy.GetConfig()
	otherRatios, otherRatioMultiplier := billingPolicyBusinessRatios(relayInfo.PriceData)
	snapshot := billingPolicyLogSnapshot{
		SchemaVersion:              config.SchemaVersion,
		Revision:                   config.Revision,
		Model:                      relayInfo.OriginModelName,
		Calculation:                calculation,
		ActualQuota:                quota,
		PreConsumedQuota:           relayInfo.FinalPreConsumedQuota,
		GroupRatio:                 relayInfo.PriceData.GroupRatioInfo.GroupRatio,
		GlobalModelRatio:           relayInfo.PriceData.GlobalModelRatio,
		PolicyAdjustmentMultiplier: relayInfo.PriceData.EffectivePolicyAdjustmentMultiplier(),
		OtherRatioMultiplier:       otherRatioMultiplier,
		OtherRatios:                otherRatios,
		AdditionalChargesUSD:       "0",
		BillableSubtotalUSD:        calculation.TotalUSD,
	}
	if common.DebugTraceEnabledForContext(ctx) {
		snapshot.Policy = &policy
	}
	other["billing_policy"] = snapshot
	other["billing_mode"] = policy.Mode
	other["use_price"] = true
	if common.DebugTraceEnabledForContext(ctx) {
		logger.LogInfo(ctx, "[billing-policy][per-request] snapshot="+common.GetJsonString(snapshot))
	}
}

func appendParamOverrideInfo(relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil || other == nil || len(relayInfo.ParamOverrideAudit) == 0 {
		return
	}
	other["po"] = relayInfo.ParamOverrideAudit
}

func appendBillingInfo(relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil || other == nil {
		return
	}
	// billing_source: "wallet" or "subscription"
	if relayInfo.BillingSource != "" {
		other["billing_source"] = relayInfo.BillingSource
	}
	if relayInfo.UserSetting.BillingPreference != "" {
		other["billing_preference"] = relayInfo.UserSetting.BillingPreference
	}
	if relayInfo.BillingSource == "subscription" {
		if relayInfo.SubscriptionId != 0 {
			other["subscription_id"] = relayInfo.SubscriptionId
		}
		if relayInfo.SubscriptionPreConsumed > 0 {
			other["subscription_pre_consumed"] = relayInfo.SubscriptionPreConsumed
		}
		// post_delta: settlement delta applied after actual usage is known (can be negative for refund)
		if relayInfo.SubscriptionPostDelta != 0 {
			other["subscription_post_delta"] = relayInfo.SubscriptionPostDelta
		}
		if relayInfo.SubscriptionPlanId != 0 {
			other["subscription_plan_id"] = relayInfo.SubscriptionPlanId
		}
		if relayInfo.SubscriptionPlanTitle != "" {
			other["subscription_plan_title"] = relayInfo.SubscriptionPlanTitle
		}
		// Compute "this request" subscription consumed + remaining
		consumed := relayInfo.SubscriptionPreConsumed + relayInfo.SubscriptionPostDelta
		usedFinal := relayInfo.SubscriptionAmountUsedAfterPreConsume + relayInfo.SubscriptionPostDelta
		if consumed < 0 {
			consumed = 0
		}
		if usedFinal < 0 {
			usedFinal = 0
		}
		if relayInfo.SubscriptionAmountTotal > 0 {
			remain := relayInfo.SubscriptionAmountTotal - usedFinal
			if remain < 0 {
				remain = 0
			}
			other["subscription_total"] = relayInfo.SubscriptionAmountTotal
			other["subscription_used"] = usedFinal
			other["subscription_remain"] = remain
		}
		if consumed > 0 {
			other["subscription_consumed"] = consumed
		}
		// Wallet quota is not deducted when billed from subscription.
		other["wallet_quota_deducted"] = 0
	}
}

func appendRequestConversionChain(relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil || other == nil {
		return
	}
	if len(relayInfo.RequestConversionChain) == 0 {
		return
	}
	chain := make([]string, 0, len(relayInfo.RequestConversionChain))
	for _, f := range relayInfo.RequestConversionChain {
		switch f {
		case types.RelayFormatOpenAI:
			chain = append(chain, "OpenAI Compatible")
		case types.RelayFormatClaude:
			chain = append(chain, "Claude Messages")
		case types.RelayFormatGemini:
			chain = append(chain, "Google Gemini")
		case types.RelayFormatOpenAIResponses:
			chain = append(chain, "OpenAI Responses")
		default:
			chain = append(chain, string(f))
		}
	}
	if len(chain) == 0 {
		return
	}
	other["request_conversion"] = chain
}

func appendFinalRequestFormat(relayInfo *relaycommon.RelayInfo, other map[string]interface{}) {
	if relayInfo == nil || other == nil {
		return
	}
	if relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		// claude indicates the final upstream request format is Claude Messages.
		// Frontend log rendering uses this to keep the original Claude input display.
		other["claude"] = true
	}
}

func GenerateWssOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.RealtimeUsage, modelRatio, groupRatio, completionRatio, audioRatio, audioCompletionRatio, modelPrice, userGroupRatio float64) map[string]interface{} {
	info := GenerateTextOtherInfo(ctx, relayInfo, modelRatio, groupRatio, completionRatio, 0, 0.0, modelPrice, userGroupRatio)
	info["ws"] = true
	info["audio_input"] = scaleLogTokens(usage.InputTokenDetails.AudioTokens, relayInfo)
	info["audio_output"] = scaleLogTokens(usage.OutputTokenDetails.AudioTokens, relayInfo)
	info["text_input"] = scaleLogTokens(usage.InputTokenDetails.TextTokens, relayInfo)
	info["text_output"] = scaleLogTokens(usage.OutputTokenDetails.TextTokens, relayInfo)
	info["audio_ratio"] = audioRatio
	info["audio_completion_ratio"] = audioCompletionRatio
	return info
}

func GenerateAudioOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, modelRatio, groupRatio, completionRatio, audioRatio, audioCompletionRatio, modelPrice, userGroupRatio float64) map[string]interface{} {
	info := GenerateTextOtherInfo(ctx, relayInfo, modelRatio, groupRatio, completionRatio, 0, 0.0, modelPrice, userGroupRatio)
	info["audio"] = true
	info["audio_input"] = scaleLogTokens(usage.PromptTokensDetails.AudioTokens, relayInfo)
	info["audio_output"] = scaleLogTokens(usage.CompletionTokenDetails.AudioTokens, relayInfo)
	info["text_input"] = scaleLogTokens(usage.PromptTokensDetails.TextTokens, relayInfo)
	info["text_output"] = scaleLogTokens(usage.CompletionTokenDetails.TextTokens, relayInfo)
	info["audio_ratio"] = audioRatio
	info["audio_completion_ratio"] = audioCompletionRatio
	return info
}

func GenerateClaudeOtherInfo(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelRatio, groupRatio, completionRatio float64,
	cacheTokens int, cacheRatio float64,
	cacheCreationTokens int, cacheCreationRatio float64,
	cacheCreationTokens5m int, cacheCreationRatio5m float64,
	cacheCreationTokens1h int, cacheCreationRatio1h float64,
	modelPrice float64, userGroupRatio float64) map[string]interface{} {
	info := GenerateTextOtherInfo(ctx, relayInfo, modelRatio, groupRatio, completionRatio, cacheTokens, cacheRatio, modelPrice, userGroupRatio)
	info["claude"] = true
	info["cache_creation_tokens"] = cacheCreationTokens
	info["cache_creation_ratio"] = cacheCreationRatio
	if cacheCreationTokens5m != 0 {
		info["cache_creation_tokens_5m"] = cacheCreationTokens5m
		info["cache_creation_ratio_5m"] = cacheCreationRatio5m
	}
	if cacheCreationTokens1h != 0 {
		info["cache_creation_tokens_1h"] = cacheCreationTokens1h
		info["cache_creation_ratio_1h"] = cacheCreationRatio1h
	}
	return info
}

func GenerateMjOtherInfo(relayInfo *relaycommon.RelayInfo, priceData types.PriceData) map[string]interface{} {
	other := make(map[string]interface{})
	other["model_price"] = priceData.ModelPrice
	attachGroupRatioBreakdown(other, priceData.GroupRatioInfo)
	other["system_global_model_ratio"] = priceData.SystemGlobalModelRatio
	other["user_global_model_ratio"] = priceData.UserGlobalModelRatio
	other["channel_model_ratio"] = priceData.ChannelModelRatio
	other["global_model_ratio"] = priceData.GlobalModelRatio
	appendRequestPath(nil, relayInfo, other)
	return other
}
