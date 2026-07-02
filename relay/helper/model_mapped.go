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
		requestNeedsImageInput := requestRequiresImageInput(request)
		mappedModel, conditional, exists := getMappedModel(modelMap, mappingModelName)
		if exists && mappedModel != "" && mappedModel != mappingModelName {
			if !(conditional && requestNeedsImageInput) {
				info.IsModelMapped = true
				info.UpstreamModelName = mappedModel
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

func validateConditionalModelMappingConflicts(modelMap map[string]string) error {
	for model := range modelMap {
		if !strings.HasPrefix(model, conditionalModelMappingPrefix) {
			if _, exists := modelMap[conditionalModelMappingPrefix+model]; exists {
				return errors.New("model_mapping_contains_conditional_conflict")
			}
			continue
		}
		plainModel := strings.TrimPrefix(model, conditionalModelMappingPrefix)
		if plainModel == "" {
			continue
		}
		if _, exists := modelMap[plainModel]; exists {
			return errors.New("model_mapping_contains_conditional_conflict")
		}
	}
	return nil
}

func getMappedModel(modelMap map[string]string, model string) (mappedModel string, conditional bool, exists bool) {
	if mappedModel, exists = modelMap[model]; exists {
		return mappedModel, false, true
	}
	if mappedModel, exists = modelMap[conditionalModelMappingPrefix+model]; exists {
		return mappedModel, true, true
	}
	return "", false, false
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
