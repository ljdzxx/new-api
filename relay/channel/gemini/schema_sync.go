package gemini

import (
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
)

// geminiSupportsJSONSchema 区分 Gemini API v1 与支持完整 JSON Schema 的端点。
func geminiSupportsJSONSchema(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return true
	}
	if info.ChannelType == constant.ChannelTypeVertexAi {
		return true
	}
	return model_setting.GetGeminiVersionSetting(info.UpstreamModelName) != "v1"
}

// convertGeminiFunctionDeclaration 根据端点能力选择有损旧字段或完整 JSON Schema 字段。
func convertGeminiFunctionDeclaration(c *gin.Context, info *relaycommon.RelayInfo, function dto.FunctionRequest) dto.GeminiFunctionDeclaration {
	declaration := dto.GeminiFunctionDeclaration{Name: function.Name, Description: function.Description}
	if function.Parameters == nil {
		return declaration
	}
	misfits := geminiSchemaMisfits(function.Parameters)
	object, _ := function.Parameters.(map[string]any)
	properties, hasProperties := object["properties"].(map[string]any)
	emptyObject := hasProperties && len(properties) == 0
	if emptyObject {
		for key, value := range object {
			if key == "type" || key == "properties" {
				continue
			}
			if key == "required" {
				if required, _ := value.([]any); len(required) == 0 {
					continue
				}
			}
			if _, allowed := geminiOpenAPISchemaAllowedFields[key]; allowed {
				misfits = append(misfits, "constraints on an empty object")
				break
			}
		}
	}
	if len(misfits) > 0 && geminiSupportsJSONSchema(info) {
		declaration.ParametersJsonSchema = function.Parameters
		return declaration
	}
	if len(misfits) > 0 && c != nil {
		logger.LogWarn(c, "Gemini v1 function schema downgraded: "+strings.Join(misfits, ", "))
	}
	if emptyObject {
		return declaration
	}
	declaration.Parameters = cleanFunctionParameters(function.Parameters)
	return declaration
}

// geminiSchemaMisfits 识别旧版 typed Schema 会丢失的约束。
func geminiSchemaMisfits(schema any) []string {
	found := make(map[string]struct{})
	collectGeminiSchemaMisfits(schema, 0, found)
	result := make([]string, 0, len(found))
	for name := range found {
		result = append(result, name)
	}
	slices.Sort(result)
	return result
}

// collectGeminiSchemaMisfits 递归检查字段、联合类型和枚举的兼容性。
func collectGeminiSchemaMisfits(schema any, depth int, found map[string]struct{}) {
	if depth >= geminiFunctionSchemaMaxDepth {
		found[fmt.Sprintf("nesting deeper than %d levels", geminiFunctionSchemaMaxDepth)] = struct{}{}
		return
	}
	object, ok := schema.(map[string]any)
	if !ok {
		if _, isBool := schema.(bool); isBool {
			found["boolean schemas"] = struct{}{}
		}
		return
	}
	for key, value := range object {
		if _, allowed := geminiOpenAPISchemaAllowedFields[key]; !allowed {
			if key != "additionalProperties" && key != "$schema" {
				found[key] = struct{}{}
			}
			continue
		}
		switch key {
		case "type":
			if _, isArray := value.([]any); isArray {
				found["type arrays"] = struct{}{}
			}
		case "enum":
			if values, ok := value.([]any); ok {
				for _, entry := range values {
					if _, isString := entry.(string); !isString {
						found["non-string enum values"] = struct{}{}
					}
				}
			}
		case "properties":
			if properties, ok := value.(map[string]any); ok {
				for _, nested := range properties {
					collectGeminiSchemaMisfits(nested, depth+1, found)
				}
			}
		case "items":
			if items, isTuple := value.([]any); isTuple {
				found["tuple items"] = struct{}{}
				for _, nested := range items {
					collectGeminiSchemaMisfits(nested, depth+1, found)
				}
			} else {
				collectGeminiSchemaMisfits(value, depth+1, found)
			}
		case "anyOf":
			if variants, ok := value.([]any); ok {
				for _, nested := range variants {
					collectGeminiSchemaMisfits(nested, depth+1, found)
				}
			}
		}
	}
}
