package helper

import (
	"errors"
	"fmt"
	"strings"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const conditionalModelMappingPrefix = "!"

func ModelMappedHelper(c *gin.Context, info *common.RelayInfo, request dto.Request) error {
	if info.ChannelMeta == nil {
		info.ChannelMeta = &common.ChannelMeta{}
	}

	isResponsesCompact := info.RelayMode == relayconstant.RelayModeResponsesCompact
	originModelName := info.OriginModelName
	mappingModelName := originModelName
	if isResponsesCompact && strings.HasSuffix(originModelName, ratio_setting.CompactModelSuffix) {
		mappingModelName = strings.TrimSuffix(originModelName, ratio_setting.CompactModelSuffix)
	}
	// 渠道可能在重试时发生变化，映射状态必须按当前渠道重新计算。
	info.IsModelMapped = false
	info.UpstreamModelName = mappingModelName
	clientReasoningEffort := requestReasoningEffort(request)
	if info.ClientReasoningEffort == "" {
		info.ClientReasoningEffort = clientReasoningEffort
	}
	// A retry may select a different channel. Reset the upstream effort to the
	// preserved client value before applying the current channel's mapping so
	// an override from the previous channel cannot leak into this request.
	info.ReasoningEffort = info.ClientReasoningEffort
	info.ModelMappingInputTokenEstimate = info.GetEstimatePromptTokens()
	info.ModelMappingThresholdEnabled = info.ChannelMeta.ModelMappingInputTokenThresholdEnabled
	info.ModelMappingThreshold = info.ChannelMeta.ModelMappingInputTokenThreshold
	info.ModelMappingThresholdSatisfied = modelMappingThresholdSatisfied(info)
	info.ModelMappingCandidateModel = mappingModelName
	info.ModelMappingCandidateEffort = info.ClientReasoningEffort
	info.ModelMappingSkippedReason = ""
	if info.ModelMappingThresholdEnabled && !info.ModelMappingThresholdSatisfied {
		info.ModelMappingSkippedReason = "input_tokens_below_threshold"
	}

	// map model name
	modelMapping := c.GetString("model_mapping")
	if modelMapping != "" && modelMapping != "{}" {
		modelMap := make(map[string]string)
		err := appcommon.UnmarshalJsonStr(modelMapping, &modelMap)
		if err != nil {
			info.ModelMappingSkippedReason = "invalid_mapping_json"
			return fmt.Errorf("unmarshal_model_mapping_failed")
		}
		if err := validateModelMappingRules(modelMap); err != nil {
			info.ModelMappingSkippedReason = "mapping_conflict"
			return err
		}

		// 模型重定向只执行单跳，命中后不再继续判断目标模型是否还有映射规则。
		if info.ModelMappingThresholdSatisfied {
			requestNeedsImageInput := requestRequiresImageInput(request)
			requestEffort := info.ClientReasoningEffort
			mappedModel, mappedEffort, exists := getMappedModel(modelMap, mappingModelName, requestEffort, requestNeedsImageInput)
			if exists && mappedModel != "" {
				if mappedModel != mappingModelName || mappedEffort != "" && mappedEffort != requestEffort {
					info.IsModelMapped = true
					info.UpstreamModelName = mappedModel
					setRequestReasoningEffort(request, info, mappedEffort)
				} else {
					info.ModelMappingSkippedReason = "identity_mapping"
				}
			} else {
				info.ModelMappingSkippedReason = "no_matching_rule"
			}
		}
	} else {
		info.ModelMappingSkippedReason = "empty_model_mapping"
	}

	// OriginModelName is the client/rating identity and must remain unchanged.
	// Only UpstreamModelName is redirected; otherwise mapping would silently
	// change pre-consume and settlement pricing to the target model.
	if request != nil {
		request.SetModelName(info.UpstreamModelName)
	}
	return nil
}

type modelMappingRule struct {
	model, effort string
	conditional   bool
	targetModel   string
	targetEffort  string
}

func parseModelMappingRule(key, value string) (modelMappingRule, error) {
	rule := modelMappingRule{}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if strings.HasPrefix(key, conditionalModelMappingPrefix) {
		rule.conditional = true
		key = strings.TrimSpace(strings.TrimPrefix(key, conditionalModelMappingPrefix))
	}
	keyParts := strings.Split(key, ",")
	valueParts := strings.Split(value, ",")
	if len(keyParts) > 2 || len(valueParts) > 2 || strings.TrimSpace(keyParts[0]) == "" || strings.TrimSpace(valueParts[0]) == "" {
		return rule, errors.New("model_mapping_invalid_entry")
	}
	rule.model = strings.TrimSpace(keyParts[0])
	if len(keyParts) == 2 {
		rule.effort = strings.TrimSpace(keyParts[1])
	}
	rule.targetModel = strings.TrimSpace(valueParts[0])
	if len(valueParts) == 2 {
		rule.targetEffort = strings.TrimSpace(valueParts[1])
	}
	return rule, nil
}

func validateModelMappingRules(modelMap map[string]string) error {
	seen := make(map[string]struct{}, len(modelMap))
	for key, value := range modelMap {
		rule, err := parseModelMappingRule(key, value)
		if err != nil {
			return err
		}
		// A conditional and a plain rule with the same model/effort are a
		// supported pair: the conditional rule handles requests without image
		// input and the plain rule is the final fallback. Precedence is resolved
		// explicitly in getMappedModel below rather than rejecting the pair.
		layer := "plain"
		if rule.conditional {
			layer = "conditional"
		}
		normalizedKey := layer + "\x00" + rule.model + "\x00" + rule.effort
		if _, exists := seen[normalizedKey]; exists {
			return errors.New("model_mapping_contains_duplicate_rule")
		}
		seen[normalizedKey] = struct{}{}
	}
	return nil
}

// Kept as a compatibility alias for package-local callers/tests using the
// previous helper name. Conditional/plain pairs are intentionally allowed.
func validateConditionalModelMappingConflicts(modelMap map[string]string) error {
	return validateModelMappingRules(modelMap)
}

func getMappedModel(modelMap map[string]string, model, effort string, requestNeedsImageInput bool) (mappedModel, mappedEffort string, exists bool) {
	var conditionalExact, conditionalFallback *modelMappingRule
	var plainExact, plainFallback *modelMappingRule
	for key, value := range modelMap {
		rule, err := parseModelMappingRule(key, value)
		if err != nil || rule.model != model || rule.conditional && requestNeedsImageInput {
			continue
		}
		if rule.effort == effort && effort != "" {
			candidate := rule
			if rule.conditional {
				conditionalExact = &candidate
			} else {
				plainExact = &candidate
			}
		}
		if rule.effort == "" {
			candidate := rule
			if rule.conditional {
				conditionalFallback = &candidate
			} else {
				plainFallback = &candidate
			}
		}
	}
	// Exact reasoning-effort rules take precedence over model-only fallbacks.
	// For requests without image input, conditional rules win ties within each
	// specificity level; for image requests they were filtered out above.
	for _, candidate := range []*modelMappingRule{
		conditionalExact,
		plainExact,
		conditionalFallback,
		plainFallback,
	} {
		if candidate != nil {
			return candidate.targetModel, candidate.targetEffort, true
		}
	}
	return "", "", false
}

func modelMappingThresholdSatisfied(info *common.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil || !info.ModelMappingInputTokenThresholdEnabled {
		return true
	}
	threshold := info.ModelMappingInputTokenThreshold
	return threshold <= 0 || int64(info.GetEstimatePromptTokens()) >= threshold
}

func requestReasoningEffort(request dto.Request) string {
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if req.ReasoningEffort != "" {
			return req.ReasoningEffort
		}
		var reasoning struct {
			Effort string `json:"effort"`
		}
		if len(req.Reasoning) > 0 && appcommon.Unmarshal(req.Reasoning, &reasoning) == nil {
			return reasoning.Effort
		}
	case *dto.OpenAIResponsesRequest:
		if req.Reasoning != nil {
			return req.Reasoning.Effort
		}
	case *dto.OpenAIResponsesCompactionRequest:
		if req.Reasoning != nil {
			return req.Reasoning.Effort
		}
	case *dto.ClaudeRequest:
		return req.GetEfforts()
	case *dto.GeminiChatRequest:
		if req.GenerationConfig.ThinkingConfig != nil {
			return req.GenerationConfig.ThinkingConfig.ThinkingLevel
		}
	}
	return ""
}

func setRequestReasoningEffort(request dto.Request, info *common.RelayInfo, effort string) {
	if effort == "" {
		return
	}
	if info != nil {
		info.ReasoningEffort = effort
	}
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		req.ReasoningEffort = effort
		if len(req.Reasoning) > 0 {
			var reasoning map[string]any
			if appcommon.Unmarshal(req.Reasoning, &reasoning) == nil {
				reasoning["effort"] = effort
				if data, err := appcommon.Marshal(reasoning); err == nil {
					req.Reasoning = data
				}
			}
		}
	case *dto.OpenAIResponsesRequest:
		if req.Reasoning == nil {
			req.Reasoning = &dto.Reasoning{}
		}
		req.Reasoning.Effort = effort
	case *dto.OpenAIResponsesCompactionRequest:
		if req.Reasoning == nil {
			req.Reasoning = &dto.Reasoning{}
		}
		req.Reasoning.Effort = effort
	case *dto.ClaudeRequest:
		var outputConfig map[string]any
		if len(req.OutputConfig) > 0 && appcommon.Unmarshal(req.OutputConfig, &outputConfig) != nil {
			outputConfig = make(map[string]any)
		}
		if outputConfig == nil {
			outputConfig = make(map[string]any)
		}
		outputConfig["effort"] = effort
		if data, err := appcommon.Marshal(outputConfig); err == nil {
			req.OutputConfig = data
		}
	case *dto.GeminiChatRequest:
		if req.GenerationConfig.ThinkingConfig == nil {
			req.GenerationConfig.ThinkingConfig = &dto.GeminiThinkingConfig{}
		}
		req.GenerationConfig.ThinkingConfig.ThinkingLevel = effort
	}
}

func requestRequiresImageInput(request dto.Request) bool {
	if request == nil {
		return false
	}
	meta := request.GetTokenCountMeta()
	if meta == nil {
		return false
	}
	for _, file := range meta.Files {
		if file != nil && file.FileType == types.FileTypeImage {
			return true
		}
	}
	return false
}
