package service

import (
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type TokenDetails struct {
	TextTokens  int
	AudioTokens int
}

type QuotaInfo struct {
	InputDetails               TokenDetails
	OutputDetails              TokenDetails
	ModelName                  string
	UsePrice                   bool
	ModelPrice                 float64
	ModelRatio                 float64
	GroupRatio                 float64
	GlobalRatio                float64
	CompletionRatio            float64
	AudioRatio                 float64
	AudioCompletionRatio       float64
	UseResolvedRatios          bool
	PolicyAdjustmentMultiplier float64
	OtherRatioMultiplier       float64
}

func hasCustomModelRatio(modelName string, currentRatio float64) bool {
	defaultRatio, exists := ratio_setting.GetDefaultModelRatioMap()[modelName]
	if !exists {
		return true
	}
	return currentRatio != defaultRatio
}

func calculateAudioQuota(info QuotaInfo) (int, *common.QuotaClamp) {
	otherRatioMultiplier := info.OtherRatioMultiplier
	if otherRatioMultiplier <= 0 {
		otherRatioMultiplier = 1
	}
	policyAdjustmentMultiplier := info.PolicyAdjustmentMultiplier
	if policyAdjustmentMultiplier <= 0 || math.IsNaN(policyAdjustmentMultiplier) || math.IsInf(policyAdjustmentMultiplier, 0) {
		policyAdjustmentMultiplier = 1
	}
	totalMultiplier := otherRatioMultiplier * policyAdjustmentMultiplier
	scaledInputTextTokens := relayhelper.ScaleTokensByGlobalModelRatio(info.InputDetails.TextTokens, info.GlobalRatio)
	scaledOutputTextTokens := relayhelper.ScaleTokensByGlobalModelRatio(info.OutputDetails.TextTokens, info.GlobalRatio)
	scaledInputAudioTokens := relayhelper.ScaleTokensByGlobalModelRatio(info.InputDetails.AudioTokens, info.GlobalRatio)
	scaledOutputAudioTokens := relayhelper.ScaleTokensByGlobalModelRatio(info.OutputDetails.AudioTokens, info.GlobalRatio)

	if info.UsePrice {
		modelPrice := decimal.NewFromFloat(info.ModelPrice)
		quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		groupRatio := decimal.NewFromFloat(info.GroupRatio)

		quota := modelPrice.Mul(quotaPerUnit).Mul(groupRatio).Mul(decimal.NewFromFloat(totalMultiplier))
		return common.QuotaFromDecimalChecked(quota)
	}

	completionRatioValue := ratio_setting.GetCompletionRatio(info.ModelName)
	audioRatioValue := ratio_setting.GetAudioRatio(info.ModelName)
	audioCompletionRatioValue := ratio_setting.GetAudioCompletionRatio(info.ModelName)
	if info.UseResolvedRatios {
		completionRatioValue = info.CompletionRatio
		audioRatioValue = info.AudioRatio
		audioCompletionRatioValue = info.AudioCompletionRatio
	}
	completionRatio := decimal.NewFromFloat(completionRatioValue)
	audioRatio := decimal.NewFromFloat(audioRatioValue)
	audioCompletionRatio := decimal.NewFromFloat(audioCompletionRatioValue)

	groupRatio := decimal.NewFromFloat(info.GroupRatio)
	modelRatio := decimal.NewFromFloat(info.ModelRatio)
	ratio := groupRatio.Mul(modelRatio)

	inputTextTokens := decimal.NewFromInt(int64(scaledInputTextTokens))
	outputTextTokens := decimal.NewFromInt(int64(scaledOutputTextTokens))
	inputAudioTokens := decimal.NewFromInt(int64(scaledInputAudioTokens))
	outputAudioTokens := decimal.NewFromInt(int64(scaledOutputAudioTokens))

	quota := decimal.Zero
	quota = quota.Add(inputTextTokens)
	quota = quota.Add(outputTextTokens.Mul(completionRatio))
	quota = quota.Add(inputAudioTokens.Mul(audioRatio))
	quota = quota.Add(outputAudioTokens.Mul(audioRatio).Mul(audioCompletionRatio))

	quota = quota.Mul(ratio).Mul(decimal.NewFromFloat(totalMultiplier))

	// If ratio is not zero and quota is less than or equal to zero, set quota to 1
	if !ratio.IsZero() && quota.LessThanOrEqual(decimal.Zero) {
		quota = decimal.NewFromInt(1)
	}

	return common.QuotaFromDecimalChecked(quota)
}

func PreWssConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.RealtimeUsage) error {
	if relayInfo.UsePrice {
		return nil
	}
	userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return err
	}

	token, err := model.GetTokenByKey(strings.TrimPrefix(relayInfo.TokenKey, "sk-"), false)
	if err != nil {
		return err
	}

	modelName := relayInfo.OriginModelName
	textInputTokens := usage.InputTokenDetails.TextTokens
	textOutTokens := usage.OutputTokenDetails.TextTokens
	audioInputTokens := usage.InputTokenDetails.AudioTokens
	audioOutTokens := usage.OutputTokenDetails.AudioTokens
	applyActivePolicyUsage(ctx, relayInfo, int64(usage.InputTokens), int64(usage.OutputTokens))
	groupRatio := ratio_setting.GetGroupRatio(relayInfo.UsingGroup)
	modelRatio, _, _ := ratio_setting.GetModelRatio(modelName)
	useResolvedRatios := false
	if billing_policy.IsActive() {
		modelRatio = relayInfo.PriceData.ModelRatio
		useResolvedRatios = true
	}

	autoGroup, exists := common.GetContextKey(ctx, constant.ContextKeyAutoGroup)
	if exists {
		groupRatio = ratio_setting.GetGroupRatio(autoGroup.(string))
		log.Printf("final group ratio: %f", groupRatio)
		relayInfo.UsingGroup = autoGroup.(string)
	}

	actualGroupRatio := groupRatio
	userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup)
	if ok {
		actualGroupRatio = userGroupRatio
	}

	quotaInfo := QuotaInfo{
		InputDetails: TokenDetails{
			TextTokens:  textInputTokens,
			AudioTokens: audioInputTokens,
		},
		OutputDetails: TokenDetails{
			TextTokens:  textOutTokens,
			AudioTokens: audioOutTokens,
		},
		ModelName:                  modelName,
		UsePrice:                   relayInfo.UsePrice,
		ModelRatio:                 modelRatio,
		GroupRatio:                 actualGroupRatio,
		GlobalRatio:                relayInfo.PriceData.GlobalModelRatio,
		CompletionRatio:            relayInfo.PriceData.CompletionRatio,
		AudioRatio:                 relayInfo.PriceData.AudioRatio,
		AudioCompletionRatio:       relayInfo.PriceData.AudioCompletionRatio,
		UseResolvedRatios:          useResolvedRatios,
		PolicyAdjustmentMultiplier: relayInfo.PriceData.EffectivePolicyAdjustmentMultiplier(),
		OtherRatioMultiplier:       relayInfo.PriceData.OtherRatioMultiplier(),
	}

	quota, clamp := calculateAudioQuota(quotaInfo)
	noteQuotaClamp(relayInfo, clamp)
	observeAudioQuotaShadow(ctx, relayInfo, quotaInfo, int64(usage.InputTokens), int64(usage.OutputTokens), quota)
	if relayInfo.PriceData.GlobalModelRatio != 1 || common.DebugTraceEnabledForContext(ctx) {
		channelID := 0
		if relayInfo.ChannelMeta != nil {
			channelID = relayInfo.ChannelMeta.ChannelId
		}
		logger.LogInfo(ctx, fmt.Sprintf(
			"global model ratio realtime token scaling billing: user_id=%d channel_id=%d token_id=%d model=%s system_global_model_ratio=%.6f user_global_model_ratio=%.6f channel_model_ratio=%.6f effective_global_model_ratio=%.6f raw_tokens={text_input:%d audio_input:%d text_output:%d audio_output:%d} scaled_tokens={text_input:%d audio_input:%d text_output:%d audio_output:%d} raw_formula=(text_input %d + audio_input %d*%.6f + text_output %d*%.6f + audio_output %d*%.6f*%.6f) * model_ratio %.6f * group_ratio %.6f scaled_formula=(text_input %d + audio_input %d*%.6f + text_output %d*%.6f + audio_output %d*%.6f*%.6f) * model_ratio %.6f * group_ratio %.6f, quota=%d",
			relayInfo.UserId, channelID, relayInfo.TokenId, modelName,
			relayInfo.PriceData.SystemGlobalModelRatio, relayInfo.PriceData.UserGlobalModelRatio, relayInfo.PriceData.ChannelModelRatio, relayInfo.PriceData.GlobalModelRatio,
			textInputTokens, audioInputTokens, textOutTokens, audioOutTokens,
			relayhelper.ScaleTokensByGlobalModelRatio(textInputTokens, relayInfo.PriceData.GlobalModelRatio),
			relayhelper.ScaleTokensByGlobalModelRatio(audioInputTokens, relayInfo.PriceData.GlobalModelRatio),
			relayhelper.ScaleTokensByGlobalModelRatio(textOutTokens, relayInfo.PriceData.GlobalModelRatio),
			relayhelper.ScaleTokensByGlobalModelRatio(audioOutTokens, relayInfo.PriceData.GlobalModelRatio),
			textInputTokens, audioInputTokens, ratio_setting.GetAudioRatio(modelName), textOutTokens, ratio_setting.GetCompletionRatio(modelName), audioOutTokens, ratio_setting.GetAudioRatio(modelName), ratio_setting.GetAudioCompletionRatio(modelName), modelRatio, groupRatio,
			relayhelper.ScaleTokensByGlobalModelRatio(textInputTokens, relayInfo.PriceData.GlobalModelRatio), relayhelper.ScaleTokensByGlobalModelRatio(audioInputTokens, relayInfo.PriceData.GlobalModelRatio), ratio_setting.GetAudioRatio(modelName), relayhelper.ScaleTokensByGlobalModelRatio(textOutTokens, relayInfo.PriceData.GlobalModelRatio), ratio_setting.GetCompletionRatio(modelName), relayhelper.ScaleTokensByGlobalModelRatio(audioOutTokens, relayInfo.PriceData.GlobalModelRatio), ratio_setting.GetAudioRatio(modelName), ratio_setting.GetAudioCompletionRatio(modelName), modelRatio, groupRatio, quota,
		))
	}

	if userQuota < quota {
		return fmt.Errorf("user quota is not enough, user quota: %s, need quota: %s", logger.FormatQuota(userQuota), logger.FormatQuota(quota))
	}

	if !token.UnlimitedQuota && token.RemainQuota < quota {
		return fmt.Errorf("token quota is not enough, token remain quota: %s, need quota: %s", logger.FormatQuota(token.RemainQuota), logger.FormatQuota(quota))
	}

	err = PostConsumeQuota(relayInfo, quota, 0, false)
	if err != nil {
		return err
	}
	logger.LogInfo(ctx, "realtime streaming consume quota success, quota: "+fmt.Sprintf("%d", quota))
	return nil
}

func PostWssConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelName string,
	usage *dto.RealtimeUsage, extraContent string) {

	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	textInputTokens := usage.InputTokenDetails.TextTokens
	textOutTokens := usage.OutputTokenDetails.TextTokens

	audioInputTokens := usage.InputTokenDetails.AudioTokens
	audioOutTokens := usage.OutputTokenDetails.AudioTokens
	applyActivePolicyUsage(ctx, relayInfo, int64(usage.InputTokens), int64(usage.OutputTokens))

	tokenName := ctx.GetString("token_name")
	completionRatio := decimal.NewFromFloat(relayInfo.PriceData.CompletionRatio)
	audioRatio := decimal.NewFromFloat(relayInfo.PriceData.AudioRatio)
	audioCompletionRatio := decimal.NewFromFloat(relayInfo.PriceData.AudioCompletionRatio)

	modelRatio := relayInfo.PriceData.ModelRatio
	groupRatio := relayInfo.PriceData.GroupRatioInfo.GroupRatio
	modelPrice := relayInfo.PriceData.ModelPrice
	usePrice := relayInfo.PriceData.UsePrice
	globalModelRatio := relayInfo.PriceData.GlobalModelRatio

	quotaInfo := QuotaInfo{
		InputDetails: TokenDetails{
			TextTokens:  textInputTokens,
			AudioTokens: audioInputTokens,
		},
		OutputDetails: TokenDetails{
			TextTokens:  textOutTokens,
			AudioTokens: audioOutTokens,
		},
		ModelName:                  modelName,
		UsePrice:                   usePrice,
		ModelRatio:                 modelRatio,
		GroupRatio:                 groupRatio,
		GlobalRatio:                globalModelRatio,
		CompletionRatio:            relayInfo.PriceData.CompletionRatio,
		AudioRatio:                 relayInfo.PriceData.AudioRatio,
		AudioCompletionRatio:       relayInfo.PriceData.AudioCompletionRatio,
		UseResolvedRatios:          true,
		PolicyAdjustmentMultiplier: relayInfo.PriceData.EffectivePolicyAdjustmentMultiplier(),
		OtherRatioMultiplier:       relayInfo.PriceData.OtherRatioMultiplier(),
	}

	quota, clamp := calculateAudioQuota(quotaInfo)
	noteQuotaClamp(relayInfo, clamp)
	observeAudioQuotaShadow(ctx, relayInfo, quotaInfo, int64(usage.InputTokens), int64(usage.OutputTokens), quota)
	totalTokens := usage.TotalTokens
	var logContent string
	if !usePrice {
		logContent = fmt.Sprintf("模型倍率 %.2f，补全倍率 %.2f，音频倍率 %.2f，音频补全倍率 %.2f，分组倍率 %.2f",
			modelRatio, completionRatio.InexactFloat64(), audioRatio.InexactFloat64(), audioCompletionRatio.InexactFloat64(), groupRatio)
	} else {
		logContent = fmt.Sprintf("模型价格 %.2f，分组倍率 %.2f", modelPrice, groupRatio)
	}

	// record all the consume log even if quota is 0
	if totalTokens == 0 {
		// in this case, must be some error happened
		// we cannot just return, because we may have to return the pre-consumed quota
		quota = 0
		logContent += fmt.Sprintf("（可能是上游超时）")
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, "+
			"tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, modelName, relayInfo.FinalPreConsumedQuota))
	} else {
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, quota)
	}

	logModel := modelName
	if extraContent != "" {
		logContent += ", " + extraContent
	}
	other := GenerateWssOtherInfo(ctx, relayInfo, usage, modelRatio, groupRatio,
		completionRatio.InexactFloat64(), audioRatio.InexactFloat64(), audioCompletionRatio.InexactFloat64(), modelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	attachAudioBillingPolicySnapshot(ctx, relayInfo,
		textInputTokens, textOutTokens, audioInputTokens, audioOutTokens,
		usage.InputTokens, usage.OutputTokens, quota, other)
	attachQuotaSaturation(ctx, relayInfo, other)
	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     relayhelper.ScaleTokensByGlobalModelRatio(usage.InputTokens, globalModelRatio),
		CompletionTokens: relayhelper.ScaleTokensByGlobalModelRatio(usage.OutputTokens, globalModelRatio),
		ModelName:        logModel,
		TokenName:        tokenName,
		Quota:            quota,
		Content:          logContent,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(useTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other,
	})
}

func CalcOpenRouterCacheCreateTokens(usage dto.Usage, priceData types.PriceData) int {
	if priceData.CacheCreationRatio == 1 {
		return 0
	}
	quotaPrice := priceData.ModelRatio / common.QuotaPerUnit
	promptCacheCreatePrice := quotaPrice * priceData.CacheCreationRatio
	promptCacheReadPrice := quotaPrice * priceData.CacheRatio
	completionPrice := quotaPrice * priceData.CompletionRatio

	cost, _ := usage.Cost.(float64)
	totalPromptTokens := float64(usage.PromptTokens)
	completionTokens := float64(usage.CompletionTokens)
	promptCacheReadTokens := float64(usage.PromptTokensDetails.CachedTokens)

	return int(math.Round((cost -
		totalPromptTokens*quotaPrice +
		promptCacheReadTokens*(quotaPrice-promptCacheReadPrice) -
		completionTokens*completionPrice) /
		(promptCacheCreatePrice - quotaPrice)))
}

func PostAudioConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent string) {

	useTimeSeconds := time.Now().Unix() - relayInfo.StartTime.Unix()
	textInputTokens := usage.PromptTokensDetails.TextTokens
	textOutTokens := usage.CompletionTokenDetails.TextTokens

	audioInputTokens := usage.PromptTokensDetails.AudioTokens
	audioOutTokens := usage.CompletionTokenDetails.AudioTokens
	applyActivePolicyUsage(ctx, relayInfo, int64(usage.PromptTokens), int64(usage.CompletionTokens))

	tokenName := ctx.GetString("token_name")
	completionRatio := decimal.NewFromFloat(relayInfo.PriceData.CompletionRatio)
	audioRatio := decimal.NewFromFloat(relayInfo.PriceData.AudioRatio)
	audioCompletionRatio := decimal.NewFromFloat(relayInfo.PriceData.AudioCompletionRatio)

	modelRatio := relayInfo.PriceData.ModelRatio
	groupRatio := relayInfo.PriceData.GroupRatioInfo.GroupRatio
	modelPrice := relayInfo.PriceData.ModelPrice
	usePrice := relayInfo.PriceData.UsePrice
	globalModelRatio := relayInfo.PriceData.GlobalModelRatio

	quotaInfo := QuotaInfo{
		InputDetails: TokenDetails{
			TextTokens:  textInputTokens,
			AudioTokens: audioInputTokens,
		},
		OutputDetails: TokenDetails{
			TextTokens:  textOutTokens,
			AudioTokens: audioOutTokens,
		},
		ModelName:                  relayInfo.OriginModelName,
		UsePrice:                   usePrice,
		ModelRatio:                 modelRatio,
		GroupRatio:                 groupRatio,
		GlobalRatio:                globalModelRatio,
		CompletionRatio:            relayInfo.PriceData.CompletionRatio,
		AudioRatio:                 relayInfo.PriceData.AudioRatio,
		AudioCompletionRatio:       relayInfo.PriceData.AudioCompletionRatio,
		UseResolvedRatios:          true,
		PolicyAdjustmentMultiplier: relayInfo.PriceData.EffectivePolicyAdjustmentMultiplier(),
		OtherRatioMultiplier:       relayInfo.PriceData.OtherRatioMultiplier(),
	}

	quota, clamp := calculateAudioQuota(quotaInfo)
	noteQuotaClamp(relayInfo, clamp)
	observeAudioQuotaShadow(ctx, relayInfo, quotaInfo, int64(usage.PromptTokens), int64(usage.CompletionTokens), quota)
	if globalModelRatio != 1 || common.DebugTraceEnabledForContext(ctx) {
		channelID := 0
		if relayInfo.ChannelMeta != nil {
			channelID = relayInfo.ChannelMeta.ChannelId
		}
		logger.LogInfo(ctx, fmt.Sprintf(
			"global model ratio audio token scaling billing: user_id=%d channel_id=%d token_id=%d model=%s system_global_model_ratio=%.6f user_global_model_ratio=%.6f channel_model_ratio=%.6f effective_global_model_ratio=%.6f raw_tokens={text_input:%d audio_input:%d text_output:%d audio_output:%d} scaled_tokens={text_input:%d audio_input:%d text_output:%d audio_output:%d} raw_formula=(text_input %d + audio_input %d*%.6f + text_output %d*%.6f + audio_output %d*%.6f*%.6f) * model_ratio %.6f * group_ratio %.6f scaled_formula=(text_input %d + audio_input %d*%.6f + text_output %d*%.6f + audio_output %d*%.6f*%.6f) * model_ratio %.6f * group_ratio %.6f, quota=%d",
			relayInfo.UserId, channelID, relayInfo.TokenId, relayInfo.OriginModelName,
			relayInfo.PriceData.SystemGlobalModelRatio, relayInfo.PriceData.UserGlobalModelRatio, relayInfo.PriceData.ChannelModelRatio, globalModelRatio,
			textInputTokens, audioInputTokens, textOutTokens, audioOutTokens,
			relayhelper.ScaleTokensByGlobalModelRatio(textInputTokens, globalModelRatio),
			relayhelper.ScaleTokensByGlobalModelRatio(audioInputTokens, globalModelRatio),
			relayhelper.ScaleTokensByGlobalModelRatio(textOutTokens, globalModelRatio),
			relayhelper.ScaleTokensByGlobalModelRatio(audioOutTokens, globalModelRatio),
			textInputTokens, audioInputTokens, audioRatio.InexactFloat64(), textOutTokens, completionRatio.InexactFloat64(), audioOutTokens, audioRatio.InexactFloat64(), audioCompletionRatio.InexactFloat64(), modelRatio, groupRatio,
			relayhelper.ScaleTokensByGlobalModelRatio(textInputTokens, globalModelRatio), relayhelper.ScaleTokensByGlobalModelRatio(audioInputTokens, globalModelRatio), audioRatio.InexactFloat64(), relayhelper.ScaleTokensByGlobalModelRatio(textOutTokens, globalModelRatio), completionRatio.InexactFloat64(), relayhelper.ScaleTokensByGlobalModelRatio(audioOutTokens, globalModelRatio), audioRatio.InexactFloat64(), audioCompletionRatio.InexactFloat64(), modelRatio, groupRatio, quota,
		))
	}

	totalTokens := usage.TotalTokens
	var logContent string
	if !usePrice {
		logContent = fmt.Sprintf("模型倍率 %.2f，补全倍率 %.2f，音频倍率 %.2f，音频补全倍率 %.2f，分组倍率 %.2f",
			modelRatio, completionRatio.InexactFloat64(), audioRatio.InexactFloat64(), audioCompletionRatio.InexactFloat64(), groupRatio)
	} else {
		logContent = fmt.Sprintf("模型价格 %.2f，分组倍率 %.2f", modelPrice, groupRatio)
	}

	// record all the consume log even if quota is 0
	if totalTokens == 0 {
		// in this case, must be some error happened
		// we cannot just return, because we may have to return the pre-consumed quota
		quota = 0
		logContent += fmt.Sprintf("（可能是上游超时）")
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, "+
			"tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, relayInfo.OriginModelName, relayInfo.FinalPreConsumedQuota))
	} else {
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, quota)
	}

	if err := SettleBilling(ctx, relayInfo, quota); err != nil {
		logger.LogError(ctx, "error settling billing: "+err.Error())
	}

	logModel := relayInfo.OriginModelName
	if extraContent != "" {
		logContent += ", " + extraContent
	}
	other := GenerateAudioOtherInfo(ctx, relayInfo, usage, modelRatio, groupRatio,
		completionRatio.InexactFloat64(), audioRatio.InexactFloat64(), audioCompletionRatio.InexactFloat64(), modelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	attachAudioBillingPolicySnapshot(ctx, relayInfo,
		textInputTokens, textOutTokens, audioInputTokens, audioOutTokens,
		usage.PromptTokens, usage.CompletionTokens, quota, other)
	attachQuotaSaturation(ctx, relayInfo, other)
	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     relayhelper.ScaleTokensByGlobalModelRatio(usage.PromptTokens, globalModelRatio),
		CompletionTokens: relayhelper.ScaleTokensByGlobalModelRatio(usage.CompletionTokens, globalModelRatio),
		ModelName:        logModel,
		TokenName:        tokenName,
		Quota:            quota,
		Content:          logContent,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(useTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other,
	})
}

func applyActivePolicyUsage(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, inputTokens, outputTokens int64) {
	if !billing_policy.IsActive() || relayInfo == nil {
		return
	}
	ensureBillingPolicySnapshot(ctx, relayInfo)
	policy, ok := requestBillingPolicy(relayInfo)
	if !ok || policy.Mode != "tiered" {
		return
	}
	values, tierID, err := billing_policy.ToLegacyValuesForUsage(policy, billing_policy.Usage{InputTotalTokens: inputTokens, OutputTotalTokens: outputTokens})
	if err != nil {
		logger.LogWarn(ctx, "failed to resolve active usage policy: "+err.Error())
		return
	}
	relayInfo.PriceData.ModelRatio = values.ModelRatio
	relayInfo.PriceData.CompletionRatio = values.CompletionRatio
	relayInfo.PriceData.CacheRatio = values.CacheRatio
	relayInfo.PriceData.CacheCreationRatio = values.CacheCreationRatio
	relayInfo.PriceData.CacheCreation5mRatio = values.CacheCreation5mRatio
	relayInfo.PriceData.CacheCreation1hRatio = values.CacheCreation1hRatio
	relayInfo.PriceData.ImageRatio = values.ImageRatio
	relayInfo.PriceData.AudioRatio = values.AudioRatio
	relayInfo.PriceData.AudioCompletionRatio = values.AudioCompletionRatio
	ctx.Set("billing_policy_tier", tierID)
}

func observeAudioQuotaShadow(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, quotaInfo QuotaInfo, inputTokens, outputTokens int64, legacyQuota int) {
	if !billing_policy.IsShadow() || relayInfo == nil {
		return
	}
	policy, ok := billing_policy.Resolve(relayInfo.OriginModelName)
	if !ok {
		billing_policy.ObserveShadowSettlementError(relayInfo.OriginModelName)
		logger.LogWarn(ctx, "shadow audio billing policy missing for model "+relayInfo.OriginModelName)
		return
	}
	values, _, err := billing_policy.ToLegacyValuesForUsage(policy, billing_policy.Usage{InputTotalTokens: inputTokens, OutputTotalTokens: outputTokens})
	if err != nil {
		billing_policy.ObserveShadowSettlementError(relayInfo.OriginModelName)
		logger.LogWarn(ctx, "shadow audio billing policy invalid: "+err.Error())
		return
	}
	quotaInfo.UsePrice = values.UsePrice
	quotaInfo.ModelPrice = values.ModelPrice
	quotaInfo.ModelRatio = values.ModelRatio
	quotaInfo.CompletionRatio = values.CompletionRatio
	quotaInfo.AudioRatio = values.AudioRatio
	quotaInfo.AudioCompletionRatio = values.AudioCompletionRatio
	quotaInfo.UseResolvedRatios = true
	policyQuota, _ := calculateAudioQuota(quotaInfo)
	billing_policy.ObserveShadowSettlement(relayInfo.OriginModelName, legacyQuota, policyQuota)
	if legacyQuota != policyQuota {
		logger.LogWarn(ctx, fmt.Sprintf("shadow audio billing mismatch: model=%s legacy=%d policy=%d", relayInfo.OriginModelName, legacyQuota, policyQuota))
	}
}

func PreConsumeTokenQuota(relayInfo *relaycommon.RelayInfo, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if relayInfo.IsPlayground {
		return nil
	}
	//if relayInfo.TokenUnlimited {
	//	return nil
	//}
	token, err := model.GetTokenByKey(relayInfo.TokenKey, false)
	if err != nil {
		return err
	}
	if !relayInfo.TokenUnlimited && token.RemainQuota < quota {
		return fmt.Errorf("token quota is not enough, token remain quota: %s, need quota: %s", logger.FormatQuota(token.RemainQuota), logger.FormatQuota(quota))
	}
	err = model.DecreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, quota)
	if err != nil {
		return err
	}
	return nil
}

func PostConsumeQuota(relayInfo *relaycommon.RelayInfo, quota int, preConsumedQuota int, sendEmail bool) (err error) {

	// 1) Consume from wallet quota OR subscription item
	if relayInfo != nil && relayInfo.BillingSource == BillingSourceSubscription {
		if relayInfo.SubscriptionId == 0 {
			return errors.New("subscription id is missing")
		}
		delta := int64(quota)
		if delta != 0 {
			if err := model.PostConsumeUserSubscriptionDelta(relayInfo.SubscriptionId, delta); err != nil {
				return err
			}
			relayInfo.SubscriptionPostDelta += delta
		}
	} else {
		// Wallet
		if quota > 0 {
			err = model.DecreaseUserQuota(relayInfo.UserId, quota)
		} else {
			err = model.IncreaseUserQuota(relayInfo.UserId, -quota, false)
		}
		if err != nil {
			return err
		}
	}

	if !relayInfo.IsPlayground {
		if quota > 0 {
			err = model.DecreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, quota)
		} else {
			err = model.IncreaseTokenQuota(relayInfo.TokenId, relayInfo.TokenKey, -quota)
		}
		if err != nil {
			return err
		}
	}

	if sendEmail {
		if (quota + preConsumedQuota) != 0 {
			checkAndSendQuotaNotify(relayInfo, quota, preConsumedQuota)
		}
	}

	return nil
}

func checkAndSendQuotaNotify(relayInfo *relaycommon.RelayInfo, quota int, preConsumedQuota int) {
	gopool.Go(func() {
		userSetting := relayInfo.UserSetting
		threshold := common.QuotaRemindThreshold
		if userSetting.QuotaWarningThreshold != 0 {
			threshold = int(userSetting.QuotaWarningThreshold)
		}

		//noMoreQuota := userCache.Quota-(quota+preConsumedQuota) <= 0
		quotaTooLow := false
		consumeQuota := quota + preConsumedQuota
		if relayInfo.UserQuota-consumeQuota < threshold {
			quotaTooLow = true
		}
		if quotaTooLow {
			prompt := "您的额度即将用尽"
			topUpLink := fmt.Sprintf("%s/console/topup", system_setting.ServerAddress)

			// 根据通知方式生成不同的内容格式
			var content string
			var values []interface{}

			notifyType := userSetting.NotifyType
			if notifyType == "" {
				notifyType = dto.NotifyTypeEmail
			}

			if notifyType == dto.NotifyTypeBark {
				// Bark推送使用简短文本，不支持HTML
				content = "{{value}}，剩余额度：{{value}}，请及时充值"
				values = []interface{}{prompt, logger.FormatQuota(relayInfo.UserQuota)}
			} else if notifyType == dto.NotifyTypeGotify {
				content = "{{value}}，当前剩余额度为 {{value}}，请及时充值。"
				values = []interface{}{prompt, logger.FormatQuota(relayInfo.UserQuota)}
			} else {
				// 默认内容格式，适用于Email和Webhook（支持HTML）
				content = "{{value}}，当前剩余额度为 {{value}}，为了不影响您的使用，请及时充值。<br/>充值链接：<a href='{{value}}'>{{value}}</a>"
				values = []interface{}{prompt, logger.FormatQuota(relayInfo.UserQuota), topUpLink, topUpLink}
			}

			err := NotifyUser(relayInfo.UserId, relayInfo.UserEmail, relayInfo.UserSetting, dto.NewNotify(dto.NotifyTypeQuotaExceed, prompt, content, values))
			if err != nil {
				common.SysError(fmt.Sprintf("failed to send quota notify to user %d: %s", relayInfo.UserId, err.Error()))
			}
		}
	})
}

func checkAndSendSubscriptionQuotaNotify(relayInfo *relaycommon.RelayInfo) {
	gopool.Go(func() {
		if relayInfo == nil {
			return
		}
		if relayInfo.SubscriptionId == 0 || relayInfo.SubscriptionAmountTotal <= 0 {
			return
		}

		userSetting := relayInfo.UserSetting
		threshold := common.QuotaRemindThreshold
		if userSetting.QuotaWarningThreshold != 0 {
			threshold = int(userSetting.QuotaWarningThreshold)
		}

		usedAfter := relayInfo.SubscriptionAmountUsedAfterPreConsume + relayInfo.SubscriptionPostDelta
		remaining := relayInfo.SubscriptionAmountTotal - usedAfter
		if remaining >= int64(threshold) {
			return
		}

		prompt := "您的订阅额度即将用尽"
		topUpLink := fmt.Sprintf("%s/console/topup", system_setting.ServerAddress)

		var content string
		var values []interface{}
		notifyType := userSetting.NotifyType
		if notifyType == "" {
			notifyType = dto.NotifyTypeEmail
		}

		if notifyType == dto.NotifyTypeBark {
			content = "{{value}}，剩余额度：{{value}}，请及时充值"
			values = []interface{}{prompt, logger.FormatQuota(int(remaining))}
		} else if notifyType == dto.NotifyTypeGotify {
			content = "{{value}}，当前剩余额度为 {{value}}，请及时充值。"
			values = []interface{}{prompt, logger.FormatQuota(int(remaining))}
		} else {
			content = "{{value}}，当前剩余额度为 {{value}}，为了不影响您的使用，请及时充值。<br/>充值链接：<a href='{{value}}'>{{value}}</a>"
			values = []interface{}{prompt, logger.FormatQuota(int(remaining)), topUpLink, topUpLink}
		}

		if err := NotifyUser(relayInfo.UserId, relayInfo.UserEmail, relayInfo.UserSetting, dto.NewNotify(dto.NotifyTypeQuotaExceed, prompt, content, values)); err != nil {
			common.SysError(fmt.Sprintf("failed to send subscription quota notify to user %d: %s", relayInfo.UserId, err.Error()))
		}
	})
}
