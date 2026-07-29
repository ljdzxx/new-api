package helper

import (
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// https://docs.claude.com/en/docs/build-with-claude/prompt-caching#1-hour-cache-duration
const claudeCacheCreation1hMultiplier = 6 / 3.75

// defaultBillingPolicyPreConsumeOutputTokens protects paid per-token policies
// from treating an omitted max_tokens as a zero-cost output during pre-consume.
const defaultBillingPolicyPreConsumeOutputTokens = 8192

func ScaleTokensByGlobalModelRatio(tokens int, globalModelRatio float64) int {
	if tokens <= 0 {
		return tokens
	}
	if math.IsNaN(globalModelRatio) || math.IsInf(globalModelRatio, 0) {
		globalModelRatio = ratio_setting.DefaultGlobalModelRatio
	}
	if globalModelRatio < 0 {
		globalModelRatio = 0
	}
	return common.QuotaFromFloat(float64(tokens) * globalModelRatio)
}

func scaleTokensByGlobalModelRatioStrict(tokens int, globalModelRatio float64) (int, error) {
	if tokens <= 0 {
		return tokens, nil
	}
	if math.IsNaN(globalModelRatio) || math.IsInf(globalModelRatio, 0) {
		globalModelRatio = ratio_setting.DefaultGlobalModelRatio
	}
	if globalModelRatio < 0 {
		globalModelRatio = 0
	}
	return common.QuotaFromFloatStrict(float64(tokens) * globalModelRatio)
}

func globalModelRatioApplies(inputTokens int64, threshold int64) bool {
	return threshold <= 0 || inputTokens >= threshold
}

type globalModelRatioConfig struct {
	systemRatio      float64
	systemThreshold  int64
	userRatio        float64
	userThreshold    int64
	channelRatio     float64
	channelThreshold int64
}

func captureGlobalModelRatioConfig(userID int, channelRatio float64, channelThreshold int64) globalModelRatioConfig {
	if math.IsNaN(channelRatio) || math.IsInf(channelRatio, 0) {
		channelRatio = ratio_setting.DefaultGlobalModelRatio
	}
	if channelRatio < 0 {
		channelRatio = 0
	}
	config := globalModelRatioConfig{
		systemRatio:      ratio_setting.GetGlobalModelRatio(),
		systemThreshold:  ratio_setting.GetGlobalModelRatioInputTokenThreshold(),
		userRatio:        ratio_setting.DefaultGlobalModelRatio,
		channelRatio:     channelRatio,
		channelThreshold: channelThreshold,
	}
	if userID <= 0 {
		return config
	}
	userRatio, inputTokenThreshold, err := model.GetUserGlobalModelRatioConfig(userID, false)
	if err != nil {
		common.SysError(fmt.Sprintf("failed to get user global model ratio, user_id=%d: %s", userID, err.Error()))
		return config
	}
	config.userRatio = userRatio
	config.userThreshold = inputTokenThreshold
	return config
}

func (config globalModelRatioConfig) effective(inputTokens int64) (float64, float64, float64, float64) {
	systemRatio := config.systemRatio
	userRatio := config.userRatio
	channelRatio := config.channelRatio
	if !globalModelRatioApplies(inputTokens, config.systemThreshold) {
		systemRatio = ratio_setting.DefaultGlobalModelRatio
	}
	if !globalModelRatioApplies(inputTokens, config.userThreshold) {
		userRatio = ratio_setting.DefaultGlobalModelRatio
	}
	if !globalModelRatioApplies(inputTokens, config.channelThreshold) {
		channelRatio = ratio_setting.DefaultGlobalModelRatio
	}
	return systemRatio, userRatio, channelRatio, systemRatio * userRatio * channelRatio
}

func getEffectiveGlobalModelRatio(userID int, channelRatio float64, channelThreshold int64, inputTokens int64) (float64, float64, float64, float64) {
	return captureGlobalModelRatioConfig(userID, channelRatio, channelThreshold).effective(inputTokens)
}

// ReevaluateGlobalModelRatioForActualInput rechecks thresholded global model
// ratios against the upstream-reported raw input token total. Ratios without a
// threshold keep the request-time snapshot so a settings change during an
// in-flight request cannot alter its settlement.
func ReevaluateGlobalModelRatioForActualInput(info *relaycommon.RelayInfo, inputTokens int64) float64 {
	if info == nil {
		return ratio_setting.DefaultGlobalModelRatio
	}

	if !info.PriceData.GlobalRatioConfigSnapshot {
		config := captureGlobalModelRatioConfig(info.UserId, getChannelModelRatio(info), getChannelModelRatioInputTokenThreshold(info))
		info.PriceData.ConfiguredSystemGlobalModelRatio = config.systemRatio
		info.PriceData.ConfiguredUserGlobalModelRatio = config.userRatio
		info.PriceData.ConfiguredChannelModelRatio = config.channelRatio
		info.PriceData.SystemGlobalRatioThreshold = config.systemThreshold
		info.PriceData.UserGlobalRatioThreshold = config.userThreshold
		info.PriceData.ChannelGlobalRatioThreshold = config.channelThreshold
		info.PriceData.GlobalRatioConfigSnapshot = true
	}
	if info.PriceData.SystemGlobalRatioThreshold <= 0 && info.PriceData.UserGlobalRatioThreshold <= 0 && info.PriceData.ChannelGlobalRatioThreshold <= 0 {
		return info.PriceData.GlobalModelRatio
	}
	config := globalModelRatioConfig{
		systemRatio: info.PriceData.ConfiguredSystemGlobalModelRatio, systemThreshold: info.PriceData.SystemGlobalRatioThreshold,
		userRatio: info.PriceData.ConfiguredUserGlobalModelRatio, userThreshold: info.PriceData.UserGlobalRatioThreshold,
		channelRatio: info.PriceData.ConfiguredChannelModelRatio, channelThreshold: info.PriceData.ChannelGlobalRatioThreshold,
	}
	systemRatio, userRatio, channelRatio, globalRatio := config.effective(inputTokens)
	info.PriceData.SystemGlobalModelRatio = systemRatio
	info.PriceData.UserGlobalModelRatio = userRatio
	info.PriceData.ChannelModelRatio = channelRatio
	info.PriceData.GlobalModelRatio = globalRatio
	return globalRatio
}

func getChannelModelRatio(info *relaycommon.RelayInfo) float64 {
	if info == nil || info.ChannelMeta == nil {
		return ratio_setting.DefaultGlobalModelRatio
	}
	return info.ChannelMeta.ChannelModelRatio
}

func getChannelModelRatioInputTokenThreshold(info *relaycommon.RelayInfo) int64 {
	if info == nil || info.ChannelMeta == nil {
		return 0
	}
	return info.ChannelMeta.RatioThreshold
}

// BindChannelModelRatio updates relay metadata before price calculation so the
// selected channel participates in both token scaling and the billing snapshot.
func BindChannelModelRatio(info *relaycommon.RelayInfo, channelRatio float64) {
	BindChannelModelRatioConfig(info, channelRatio, 0)
}

func BindChannelModelRatioConfig(info *relaycommon.RelayInfo, channelRatio float64, inputTokenThreshold int64) {
	if info == nil {
		return
	}
	if math.IsNaN(channelRatio) || math.IsInf(channelRatio, 0) || channelRatio < 0 {
		channelRatio = ratio_setting.DefaultGlobalModelRatio
	}
	if info.ChannelMeta == nil {
		info.ChannelMeta = &relaycommon.ChannelMeta{}
	}
	info.ChannelMeta.ChannelModelRatio = channelRatio
	if inputTokenThreshold < 0 {
		inputTokenThreshold = 0
	}
	info.ChannelMeta.RatioThreshold = inputTokenThreshold
}

// HandleGroupRatio checks for "auto_group" in the context and updates the group ratio and relayInfo.UsingGroup if present
func HandleGroupRatio(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) types.GroupRatioInfo {
	groupRatioInfo := types.GroupRatioInfo{
		GroupRatio:        1.0, // default ratio
		BaseGroupRatio:    1.0,
		UserLevelRatio:    1.0,
		GroupSpecialRatio: -1,
	}

	// check auto group
	autoGroup, exists := ctx.Get("auto_group")
	if exists {
		logger.LogDebug(ctx, fmt.Sprintf("final group: %s", autoGroup))
		relayInfo.UsingGroup = autoGroup.(string)
	}

	// check user group special ratio
	userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup)
	if ok {
		// user group special ratio
		groupRatioInfo.GroupSpecialRatio = userGroupRatio
		groupRatioInfo.GroupRatio = userGroupRatio
		groupRatioInfo.HasSpecialRatio = true
	} else {
		// normal group ratio
		groupRatioInfo.GroupRatio = ratio_setting.GetGroupRatio(relayInfo.UsingGroup)
	}
	userLevelID := common.GetContextKeyInt(ctx, constant.ContextKeyUserLevelID)
	groupRatioInfo.BaseGroupRatio = groupRatioInfo.GroupRatio
	groupRatioInfo.UserLevelID = userLevelID
	groupRatioInfo.UserLevelRatio = setting.GetUserLevelDiscountMultiplierByID(userLevelID)
	groupRatioInfo.GroupRatio = groupRatioInfo.BaseGroupRatio * groupRatioInfo.UserLevelRatio

	return groupRatioInfo
}

func ModelPriceHelper(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) (types.PriceData, error) {
	modelPrice, usePrice := ratio_setting.GetModelPrice(info.OriginModelName, false)
	var activePolicyValues *billing_policy.LegacyValues
	var activePolicy *billing_policy.Policy
	activePolicyTier := ""
	if billing_policy.IsActive() {
		policy, ok := frozenBillingPolicy(c, info)
		if !ok {
			return types.PriceData{}, fmt.Errorf("模型 %s 未配置新版计费策略", info.OriginModelName)
		}
		values, tierID, err := billing_policy.ToLegacyValuesForUsage(policy, billing_policy.Usage{InputTotalTokens: int64(promptTokens)})
		if err != nil {
			return types.PriceData{}, fmt.Errorf("模型 %s 计费策略无效: %w", info.OriginModelName, err)
		}
		activePolicyValues = &values
		activePolicy = &policy
		activePolicyTier = tierID
		modelPrice, usePrice = values.ModelPrice, values.UsePrice
	}
	policyAdjustmentMultiplier := 1.0
	var policyAppliedAdjustments []billing_policy.AppliedAdjustment
	if billing_policy.IsActive() {
		if policy, ok := frozenBillingPolicy(c, info); ok {
			multiplier, applied := frozenPolicyAdjustments(info, policy)
			policyAdjustmentMultiplier, _ = multiplier.Float64()
			policyAppliedAdjustments = applied
			if len(applied) > 0 {
				c.Set("billing_policy_adjustments", applied)
			}
		}
	}
	globalRatioConfig := captureGlobalModelRatioConfig(info.UserId, getChannelModelRatio(info), getChannelModelRatioInputTokenThreshold(info))
	systemGlobalModelRatio, userGlobalModelRatio, channelModelRatio, globalModelRatio := globalRatioConfig.effective(int64(promptTokens))

	groupRatioInfo := HandleGroupRatio(c, info)

	var preConsumedQuota int
	var modelRatio float64
	var completionRatio float64
	var cacheRatio float64
	var imageRatio float64
	var cacheCreationRatio float64
	var cacheCreationRatio5m float64
	var cacheCreationRatio1h float64
	var audioRatio float64
	var audioCompletionRatio float64
	var freeModel bool
	policyPreConsumeCalculated := false
	requestBillingRatios := types.PriceData{}
	if usePrice && meta != nil {
		for name, ratio := range meta.BillingRatios {
			requestBillingRatios.AddOtherRatio(name, ratio)
		}
	}
	if !usePrice {
		preConsumedTokens := common.Max(promptTokens, common.PreConsumedQuota)
		if meta.MaxTokens != 0 {
			preConsumedTokens += meta.MaxTokens
		}
		var success bool
		var matchName string
		if activePolicyValues != nil {
			modelRatio, success, matchName = activePolicyValues.ModelRatio, true, info.OriginModelName
		} else {
			modelRatio, success, matchName = ratio_setting.GetModelRatio(info.OriginModelName)
		}
		if !success {
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !acceptUnsetRatio {
				return types.PriceData{}, fmt.Errorf("模型 %s 倍率或价格未配置，请联系管理员设置或开始自用模式；Model %s ratio or price not set, please set or start self-use mode", matchName, matchName)
			}
		}
		if activePolicyValues != nil {
			completionRatio = activePolicyValues.CompletionRatio
			cacheRatio = activePolicyValues.CacheRatio
			cacheCreationRatio = activePolicyValues.CacheCreationRatio
			cacheCreationRatio5m = activePolicyValues.CacheCreation5mRatio
			imageRatio = activePolicyValues.ImageRatio
			audioRatio = activePolicyValues.AudioRatio
			audioCompletionRatio = activePolicyValues.AudioCompletionRatio
		} else {
			completionRatio = ratio_setting.GetCompletionRatio(info.OriginModelName)
			cacheRatio, _ = ratio_setting.GetCacheRatio(info.OriginModelName)
			cacheCreationRatio, _ = ratio_setting.GetCreateCacheRatio(info.OriginModelName)
			imageRatio, _ = ratio_setting.GetImageRatio(info.OriginModelName)
			audioRatio = ratio_setting.GetAudioRatio(info.OriginModelName)
			audioCompletionRatio = ratio_setting.GetAudioCompletionRatio(info.OriginModelName)
		}
		if activePolicyValues == nil {
			cacheCreationRatio5m = cacheCreationRatio
		}
		// 固定1h和5min缓存写入价格的比例
		cacheCreationRatio1h = cacheCreationRatio * claudeCacheCreation1hMultiplier
		if activePolicyValues != nil {
			cacheCreationRatio1h = activePolicyValues.CacheCreation1hRatio
		}
		scaledPreConsumedTokens, err := scaleTokensByGlobalModelRatioStrict(preConsumedTokens, globalModelRatio)
		if err != nil {
			return types.PriceData{}, err
		}
		if activePolicy != nil {
			estimatedOutputTokens := meta.MaxTokens
			if estimatedOutputTokens == 0 && groupRatioInfo.GroupRatio != 0 && globalModelRatio != 0 {
				estimatedOutputTokens = defaultBillingPolicyPreConsumeOutputTokens
			}
			scaledInputTokens, err := scaleTokensByGlobalModelRatioStrict(common.Max(promptTokens, common.PreConsumedQuota), globalModelRatio)
			if err != nil {
				return types.PriceData{}, err
			}
			scaledOutputTokens, err := scaleTokensByGlobalModelRatioStrict(estimatedOutputTokens, globalModelRatio)
			if err != nil {
				return types.PriceData{}, err
			}
			calculation, err := billing_policy.CalculateBilling(*activePolicy, billing_policy.BillingUsage{
				TierInputTotalTokens:  int64(promptTokens),
				TierOutputTotalTokens: int64(estimatedOutputTokens),
				InputTokens:           int64(scaledInputTokens),
				OutputTokens:          int64(scaledOutputTokens),
			}, billingPolicyRequestContext(c))
			if err != nil {
				return types.PriceData{}, fmt.Errorf("模型 %s 预扣费计算失败: %w", info.OriginModelName, err)
			}
			policyCost, err := decimal.NewFromString(calculation.TotalUSD)
			if err != nil {
				return types.PriceData{}, fmt.Errorf("模型 %s 预扣费金额无效: %w", info.OriginModelName, err)
			}
			preConsumedQuota, err = common.QuotaFromDecimalStrict(policyCost.
				Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
				Mul(decimal.NewFromFloat(groupRatioInfo.GroupRatio)))
			if err != nil {
				return types.PriceData{}, err
			}
			values, _, err := billing_policy.ToLegacyValuesForUsage(*activePolicy, billing_policy.Usage{
				InputTotalTokens: int64(promptTokens), OutputTotalTokens: int64(estimatedOutputTokens),
			})
			if err != nil {
				return types.PriceData{}, fmt.Errorf("模型 %s 预扣费价格解析失败: %w", info.OriginModelName, err)
			}
			modelRatio = values.ModelRatio
			completionRatio = values.CompletionRatio
			cacheRatio = values.CacheRatio
			cacheCreationRatio = values.CacheCreationRatio
			cacheCreationRatio5m = values.CacheCreation5mRatio
			cacheCreationRatio1h = values.CacheCreation1hRatio
			imageRatio = values.ImageRatio
			audioRatio = values.AudioRatio
			audioCompletionRatio = values.AudioCompletionRatio
			policyPreConsumeCalculated = true
			activePolicyTier = calculation.TierID
		} else {
			ratio := modelRatio * groupRatioInfo.GroupRatio
			preConsumedQuota, err = common.QuotaFromFloatStrict(float64(scaledPreConsumedTokens) * ratio)
			if err != nil {
				return types.PriceData{}, err
			}
		}
		if globalModelRatio != 1 || common.DebugTraceEnabledForContext(c) {
			channelID := 0
			if info.ChannelMeta != nil {
				channelID = info.ChannelMeta.ChannelId
			}
			logger.LogInfo(c, fmt.Sprintf(
				"global model ratio token scaling pre-consume: user_id=%d channel_id=%d token_id=%d model=%s system_global_model_ratio=%.6f user_global_model_ratio=%.6f channel_model_ratio=%.6f effective_global_model_ratio=%.6f raw_preconsume_tokens=%d scaled_preconsume_tokens=%d raw_formula=%d tokens * model_ratio %.6f * group_ratio %.6f = quota %.6f scaled_formula=%d tokens * model_ratio %.6f * group_ratio %.6f = quota %d",
				info.UserId, channelID, info.TokenId, info.OriginModelName,
				systemGlobalModelRatio, userGlobalModelRatio, channelModelRatio, globalModelRatio,
				preConsumedTokens, scaledPreConsumedTokens,
				preConsumedTokens, modelRatio, groupRatioInfo.GroupRatio, float64(preConsumedTokens)*modelRatio*groupRatioInfo.GroupRatio,
				scaledPreConsumedTokens, modelRatio, groupRatioInfo.GroupRatio, preConsumedQuota,
			))
		}
	} else {
		if meta.ImagePriceRatio != 0 {
			modelPrice = modelPrice * meta.ImagePriceRatio
		}
		quotaToPreConsume := modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio
		quotaToPreConsume = requestBillingRatios.ApplyOtherRatiosToFloat(quotaToPreConsume)
		quotaToPreConsume *= policyAdjustmentMultiplier
		var err error
		preConsumedQuota, err = common.QuotaFromFloatStrict(quotaToPreConsume)
		if err != nil {
			return types.PriceData{}, err
		}
	}
	if policyAdjustmentMultiplier != 1 && !policyPreConsumeCalculated && !usePrice {
		var err error
		preConsumedQuota, err = common.QuotaFromFloatStrict(float64(preConsumedQuota) * policyAdjustmentMultiplier)
		if err != nil {
			return types.PriceData{}, err
		}
	}

	// check if free model pre-consume is disabled
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		// if model price or ratio is 0, do not pre-consume quota
		if groupRatioInfo.GroupRatio == 0 || globalModelRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		} else if usePrice {
			if modelPrice == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		} else {
			if modelRatio == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		}
	}

	priceData := types.PriceData{
		FreeModel:                        freeModel,
		ModelPrice:                       modelPrice,
		ModelRatio:                       modelRatio,
		SystemGlobalModelRatio:           systemGlobalModelRatio,
		UserGlobalModelRatio:             userGlobalModelRatio,
		ChannelModelRatio:                channelModelRatio,
		GlobalModelRatio:                 globalModelRatio,
		ConfiguredSystemGlobalModelRatio: globalRatioConfig.systemRatio,
		ConfiguredUserGlobalModelRatio:   globalRatioConfig.userRatio,
		ConfiguredChannelModelRatio:      globalRatioConfig.channelRatio,
		SystemGlobalRatioThreshold:       globalRatioConfig.systemThreshold,
		UserGlobalRatioThreshold:         globalRatioConfig.userThreshold,
		ChannelGlobalRatioThreshold:      globalRatioConfig.channelThreshold,
		GlobalRatioConfigSnapshot:        true,
		CompletionRatio:                  completionRatio,
		GroupRatioInfo:                   groupRatioInfo,
		UsePrice:                         usePrice,
		CacheRatio:                       cacheRatio,
		ImageRatio:                       imageRatio,
		AudioRatio:                       audioRatio,
		AudioCompletionRatio:             audioCompletionRatio,
		CacheCreationRatio:               cacheCreationRatio,
		CacheCreation5mRatio:             cacheCreationRatio5m,
		CacheCreation1hRatio:             cacheCreationRatio1h,
		QuotaToPreConsume:                preConsumedQuota,
	}
	if policyAdjustmentMultiplier != 1 {
		priceData.SetPolicyAdjustmentMultiplier(policyAdjustmentMultiplier)
	}
	priceData.ReplaceOtherRatios(requestBillingRatios.OtherRatios())

	if common.DebugEnabled {
		println(fmt.Sprintf("model_price_helper result: %s", priceData.ToSetting()))
	}
	info.PriceData = priceData
	if activePolicy != nil && common.DebugTraceEnabledForContext(c) {
		logger.LogInfo(c, fmt.Sprintf(
			"[billing-policy][pre-consume] model=%s revision=%d prompt_tokens=%d matched_tier=%s policy=%s legacy_values=%s adjustments=%s adjustment_multiplier=%.10g price_data=%s",
			info.OriginModelName, billing_policy.GetConfig().Revision, promptTokens, activePolicyTier,
			common.GetJsonString(activePolicy), common.GetJsonString(activePolicyValues),
			common.GetJsonString(policyAppliedAdjustments), policyAdjustmentMultiplier, common.GetJsonString(priceData),
		))
	}
	observeShadowPreConsume(c, info, promptTokens, meta, priceData)
	return priceData, nil
}

func evaluatePolicyAdjustments(c *gin.Context, policy billing_policy.Policy) (decimal.Decimal, []billing_policy.AppliedAdjustment) {
	return billing_policy.EvaluateAdjustments(policy, billingPolicyRequestContext(c))
}

func frozenBillingPolicy(c *gin.Context, info *relaycommon.RelayInfo) (billing_policy.Policy, bool) {
	if info == nil {
		return billing_policy.Policy{}, false
	}
	if info.BillingPolicySnapshot == nil {
		snapshot, ok := billing_policy.CaptureSnapshot(info.OriginModelName, billingPolicyRequestContext(c))
		if !ok {
			return billing_policy.Policy{}, false
		}
		info.BillingPolicySnapshot = snapshot
	}
	return info.BillingPolicySnapshot.Policy, true
}

func frozenPolicyAdjustments(info *relaycommon.RelayInfo, policy billing_policy.Policy) (decimal.Decimal, []billing_policy.AppliedAdjustment) {
	if info != nil && info.BillingPolicySnapshot != nil {
		return billing_policy.EvaluateAdjustments(policy, billing_policy.RequestContext{
			FreezeAdjustments:    true,
			AdjustmentMultiplier: info.BillingPolicySnapshot.AdjustmentMultiplier,
			AppliedAdjustments:   info.BillingPolicySnapshot.AppliedAdjustments,
		})
	}
	return decimal.NewFromInt(1), nil
}

func billingPolicyRequestContext(c *gin.Context) billing_policy.RequestContext {
	requestContext := billing_policy.RequestContext{}
	if c == nil || c.Request == nil {
		return requestContext
	}
	requestContext.Headers = make(map[string]string, len(c.Request.Header))
	for key, values := range c.Request.Header {
		if len(values) > 0 {
			requestContext.Headers[strings.ToLower(key)] = values[0]
		}
	}
	if storage, err := common.GetBodyStorage(c); err == nil && storage != nil {
		requestContext.Body, _ = storage.Bytes()
	}
	return requestContext
}

func observeShadowPreConsume(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta, legacy types.PriceData) {
	if !billing_policy.IsShadow() || info == nil || meta == nil {
		return
	}
	policy, ok := billing_policy.Resolve(info.OriginModelName)
	if !ok {
		billing_policy.ObserveShadowPreConsumeError(info.OriginModelName)
		logger.LogWarn(c, "shadow billing policy missing for model "+info.OriginModelName)
		return
	}
	estimatedOutputTokens := meta.MaxTokens
	if estimatedOutputTokens == 0 && legacy.GroupRatioInfo.GroupRatio != 0 && legacy.GlobalModelRatio != 0 {
		estimatedOutputTokens = defaultBillingPolicyPreConsumeOutputTokens
	}
	values, _, err := billing_policy.ToLegacyValuesForUsage(policy, billing_policy.Usage{
		InputTotalTokens: int64(promptTokens), OutputTotalTokens: int64(estimatedOutputTokens),
	})
	if err != nil {
		billing_policy.ObserveShadowPreConsumeError(info.OriginModelName)
		logger.LogWarn(c, "shadow billing policy invalid for model "+info.OriginModelName+": "+err.Error())
		return
	}
	var quota int
	if values.UsePrice {
		price := values.ModelPrice
		if meta.ImagePriceRatio != 0 {
			price *= meta.ImagePriceRatio
		}
		shadowPriceData := types.PriceData{}
		for name, ratio := range meta.BillingRatios {
			shadowPriceData.AddOtherRatio(name, ratio)
		}
		quotaValue := shadowPriceData.ApplyOtherRatiosToFloat(price * common.QuotaPerUnit * legacy.GroupRatioInfo.GroupRatio)
		adjustment, _ := evaluatePolicyAdjustments(c, policy)
		quotaValue = adjustment.Mul(decimal.NewFromFloat(quotaValue)).InexactFloat64()
		quota = common.QuotaFromFloat(quotaValue)
	} else {
		calculation, calculationErr := billing_policy.CalculateBilling(policy, billing_policy.BillingUsage{
			TierInputTotalTokens:  int64(promptTokens),
			TierOutputTotalTokens: int64(estimatedOutputTokens),
			InputTokens:           int64(ScaleTokensByGlobalModelRatio(common.Max(promptTokens, common.PreConsumedQuota), legacy.GlobalModelRatio)),
			OutputTokens:          int64(ScaleTokensByGlobalModelRatio(estimatedOutputTokens, legacy.GlobalModelRatio)),
		}, billingPolicyRequestContext(c))
		if calculationErr != nil {
			billing_policy.ObserveShadowPreConsumeError(info.OriginModelName)
			logger.LogWarn(c, "shadow billing policy pre-consume calculation failed: "+calculationErr.Error())
			return
		}
		policyCost, parseErr := decimal.NewFromString(calculation.TotalUSD)
		if parseErr != nil {
			billing_policy.ObserveShadowPreConsumeError(info.OriginModelName)
			logger.LogWarn(c, "shadow billing policy pre-consume amount invalid: "+parseErr.Error())
			return
		}
		quota = common.QuotaFromDecimal(policyCost.
			Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
			Mul(decimal.NewFromFloat(legacy.GroupRatioInfo.GroupRatio)))
	}
	if legacy.FreeModel {
		quota = 0
	}
	billing_policy.ObserveShadowPreConsume(info.OriginModelName, legacy.QuotaToPreConsume, quota)
	if quota != legacy.QuotaToPreConsume {
		logger.LogWarn(c, fmt.Sprintf("shadow billing pre-consume mismatch: model=%s legacy=%d policy=%d", info.OriginModelName, legacy.QuotaToPreConsume, quota))
	}
}

// ModelPriceHelperPerCall 按次计费的 PriceHelper (MJ、Task)
func ModelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo) (types.PriceData, error) {
	groupRatioInfo := HandleGroupRatio(c, info)
	globalRatioConfig := captureGlobalModelRatioConfig(info.UserId, getChannelModelRatio(info), getChannelModelRatioInputTokenThreshold(info))
	systemGlobalModelRatio, userGlobalModelRatio, channelModelRatio, globalModelRatio := globalRatioConfig.effective(0)

	modelPrice, success := ratio_setting.GetModelPrice(info.OriginModelName, true)
	if billing_policy.IsActive() {
		policy, ok := frozenBillingPolicy(c, info)
		if !ok {
			return types.PriceData{}, fmt.Errorf("模型 %s 未配置新版计费策略", info.OriginModelName)
		}
		values, err := billing_policy.ToLegacyValues(policy)
		if err != nil {
			return types.PriceData{}, fmt.Errorf("模型 %s 计费策略无效: %w", info.OriginModelName, err)
		}
		if !values.UsePrice {
			return types.PriceData{}, fmt.Errorf("模型 %s 不是按次计费策略", info.OriginModelName)
		}
		modelPrice, success = values.ModelPrice, true
	}
	// 如果没有配置价格，检查模型倍率配置
	if !success {

		// 没有配置费用，也要使用默认费用,否则按费率计费模型无法使用
		defaultPrice, ok := ratio_setting.GetDefaultModelPriceMap()[info.OriginModelName]
		if ok {
			modelPrice = defaultPrice
		} else {
			// 没有配置倍率也不接受没配置,那就返回错误
			_, ratioSuccess, matchName := ratio_setting.GetModelRatio(info.OriginModelName)
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !ratioSuccess && !acceptUnsetRatio {
				return types.PriceData{}, fmt.Errorf("模型 %s 倍率或价格未配置，请联系管理员设置或开始自用模式；Model %s ratio or price not set, please set or start self-use mode", matchName, matchName)
			}
			// 未配置价格但配置了倍率，使用默认预扣价格
			modelPrice = float64(common.PreConsumedQuota) / common.QuotaPerUnit
		}

	}
	quota, err := common.QuotaFromFloatStrict(modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
	if err != nil {
		return types.PriceData{}, err
	}
	policyAdjustmentMultiplier := 1.0
	if billing_policy.IsActive() {
		if policy, ok := frozenBillingPolicy(c, info); ok {
			multiplier, applied := frozenPolicyAdjustments(info, policy)
			policyAdjustmentMultiplier, _ = multiplier.Float64()
			if len(applied) > 0 {
				c.Set("billing_policy_adjustments", applied)
			}
		}
	}
	if policyAdjustmentMultiplier != 1 {
		quota, err = common.QuotaFromFloatStrict(float64(quota) * policyAdjustmentMultiplier)
		if err != nil {
			return types.PriceData{}, err
		}
	}

	// 免费模型检测（与 ModelPriceHelper 对齐）
	freeModel := false
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		if groupRatioInfo.GroupRatio == 0 || modelPrice == 0 {
			quota = 0
			freeModel = true
		}
	}

	priceData := types.PriceData{
		FreeModel:                        freeModel,
		ModelPrice:                       modelPrice,
		SystemGlobalModelRatio:           systemGlobalModelRatio,
		UserGlobalModelRatio:             userGlobalModelRatio,
		ChannelModelRatio:                channelModelRatio,
		GlobalModelRatio:                 globalModelRatio,
		ConfiguredSystemGlobalModelRatio: globalRatioConfig.systemRatio,
		ConfiguredUserGlobalModelRatio:   globalRatioConfig.userRatio,
		ConfiguredChannelModelRatio:      globalRatioConfig.channelRatio,
		SystemGlobalRatioThreshold:       globalRatioConfig.systemThreshold,
		UserGlobalRatioThreshold:         globalRatioConfig.userThreshold,
		ChannelGlobalRatioThreshold:      globalRatioConfig.channelThreshold,
		GlobalRatioConfigSnapshot:        true,
		Quota:                            quota,
		GroupRatioInfo:                   groupRatioInfo,
	}
	if policyAdjustmentMultiplier != 1 {
		priceData.SetPolicyAdjustmentMultiplier(policyAdjustmentMultiplier)
	}
	if billing_policy.IsShadow() {
		policy, ok := billing_policy.Resolve(info.OriginModelName)
		if !ok {
			billing_policy.ObserveShadowSettlementError(info.OriginModelName)
			logger.LogWarn(c, "shadow per-call billing policy missing for model "+info.OriginModelName)
		} else if values, err := billing_policy.ToLegacyValues(policy); err != nil || !values.UsePrice {
			billing_policy.ObserveShadowSettlementError(info.OriginModelName)
			logger.LogWarn(c, "shadow per-call billing policy invalid for model "+info.OriginModelName)
		} else {
			policyQuota := common.QuotaFromFloat(values.ModelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio)
			if freeModel {
				policyQuota = 0
			}
			billing_policy.ObserveShadowSettlement(info.OriginModelName, quota, policyQuota)
			if quota != policyQuota {
				logger.LogWarn(c, fmt.Sprintf("shadow per-call billing mismatch: model=%s legacy=%d policy=%d", info.OriginModelName, quota, policyQuota))
			}
		}
	}
	if billing_policy.IsActive() && common.DebugTraceEnabledForContext(c) {
		if policy, ok := billing_policy.Resolve(info.OriginModelName); ok {
			logger.LogInfo(c, fmt.Sprintf(
				"[billing-policy][per-request-pre-consume] model=%s revision=%d policy=%s adjustment_multiplier=%.10g quota=%d price_data=%s",
				info.OriginModelName, billing_policy.GetConfig().Revision, common.GetJsonString(policy),
				policyAdjustmentMultiplier, quota, common.GetJsonString(priceData),
			))
		}
	}
	return priceData, nil
}

func ContainPriceOrRatio(modelName string) bool {
	_, ok := ratio_setting.GetModelPrice(modelName, false)
	if ok {
		return true
	}
	_, ok, _ = ratio_setting.GetModelRatio(modelName)
	if ok {
		return true
	}
	return false
}
