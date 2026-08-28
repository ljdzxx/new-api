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

	// map model name
	modelMapping := c.GetString("model_mapping")
	if modelMapping != "" && modelMapping != "{}" {
		modelMap := make(map[string]string)
		err := appcommon.UnmarshalJsonStr(modelMapping, &modelMap)
		if err != nil {
			return fmt.Errorf("unmarshal_model_mapping_failed")
		}
		if err := validateConditionalModelMappingConflicts(modelMap); err != nil {
			return err
		}

		// 模型重定向只执行单跳，命中后不再继续判断目标模型是否还有映射规则。
		if modelMappingThresholdSatisfied(info) {
			requestNeedsImageInput := requestRequiresImageInput(request)
			requestEffort := requestReasoningEffort(request)
			mappedModel, mappedEffort, exists := getMappedModel(modelMap, mappingModelName, requestEffort, requestNeedsImageInput)
			if exists && mappedModel != "" {
				if mappedModel != mappingModelName || mappedEffort != "" && mappedEffort != requestEffort {
					info.IsModelMapped = true
					info.UpstreamModelName = mappedModel
					setRequestReasoningEffort(request, info, mappedEffort)
				}
			}
		}
	}

	if isResponsesCompact {
		finalUpstreamModelName := mappingModelName
		if info.IsModelMapped && info.UpstreamModelName != "" {
			finalUpstreamModelName = info.UpstreamModelName
		}
		info.UpstreamModelName = finalUpstreamModelName
		info.OriginModelName = ratio_setting.WithCompactModelSuffix(finalUpstreamModelName)
	}
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

func validateConditionalModelMappingConflicts(modelMap map[string]string) error {
	plain := make(map[string]bool)
	conditional := make(map[string]bool)
	for key, value := range modelMap {
		rule, err := parseModelMappingRule(key, value)
		if err != nil {
			return err
		}
		conflictKey := rule.model + "\x00" + rule.effort
		if rule.conditional {
			if plain[conflictKey] {
				return errors.New("model_mapping_contains_conditional_conflict")
			}
			conditional[conflictKey] = true
		} else {
			if conditional[conflictKey] {
				return errors.New("model_mapping_contains_conditional_conflict")
			}
			plain[conflictKey] = true
		}
	}
	return nil
}

func getMappedModel(modelMap map[string]string, model, effort string, requestNeedsImageInput bool) (mappedModel, mappedEffort string, exists bool) {
	var fallback *modelMappingRule
	for key, value := range modelMap {
		rule, err := parseModelMappingRule(key, value)
		if err != nil || rule.model != model || rule.conditional && requestNeedsImageInput {
			continue
		}
		if rule.effort == effort && effort != "" {
			return rule.targetModel, rule.targetEffort, true
		}
		if rule.effort == "" {
			candidate := rule
			fallback = &candidate
		}
	}
	if fallback != nil {
		return fallback.targetModel, fallback.targetEffort, true
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
