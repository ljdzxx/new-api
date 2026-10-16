package gemini

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// normalizeGeminiThinkingConfig 把 Gemini 2.5 的 budget 与 Gemini 3 的 level
// 互相转换，并限制模型接受的范围，避免上游因组合不兼容返回 400。
func normalizeGeminiThinkingConfig(request *dto.GeminiChatRequest, info *relaycommon.RelayInfo) error {
	if request == nil || request.GenerationConfig.ThinkingConfig == nil || info == nil {
		return nil
	}
	model := strings.ToLower(strings.TrimPrefix(info.UpstreamModelName, "models/"))
	config := request.GenerationConfig.ThinkingConfig
	if strings.HasPrefix(model, "gemini-2.5-flash-image") {
		request.GenerationConfig.ThinkingConfig = nil
		return nil
	}
	if strings.HasPrefix(model, "gemini-3-pro-image") {
		config.ThinkingBudget = nil
		config.ThinkingLevel = ""
		return nil
	}
	if config.ThinkingLevel != "" {
		config.ThinkingLevel = strings.ToLower(strings.TrimSpace(config.ThinkingLevel))
	}

	levelModel := strings.HasPrefix(model, "gemini-3") || model == "gemini-flash-latest" || model == "gemini-flash-lite-latest" || model == "gemini-pro-latest"
	budgetModel := strings.HasPrefix(model, "gemini-2.5")
	if levelModel {
		if config.ThinkingBudget != nil {
			budget := *config.ThinkingBudget
			if budget == 0 {
				config.ThinkingLevel = "minimal"
			} else if budget > 0 {
				config.ThinkingLevel = geminiThinkingLevelFromBudget(budget)
			}
			config.ThinkingBudget = nil
		}
		if config.ThinkingLevel != "" {
			config.ThinkingLevel = normalizeGeminiThinkingLevel(config.ThinkingLevel)
			if strings.HasPrefix(model, "gemini-3-pro") && !strings.HasPrefix(model, "gemini-3.1-pro") {
				if config.ThinkingLevel == "minimal" {
					config.ThinkingLevel = "low"
				} else if config.ThinkingLevel == "medium" {
					config.ThinkingLevel = "high"
				}
			}
			if strings.HasPrefix(model, "gemini-3.1-flash-image") && config.ThinkingLevel != "minimal" {
				config.ThinkingLevel = "high"
			}
		}
		return nil
	}
	if !budgetModel {
		return nil
	}
	maxBudget := 24576
	minBudget := 0
	if strings.HasPrefix(model, "gemini-2.5-pro") {
		maxBudget = 32768
		minBudget = 128
	} else if strings.HasPrefix(model, "gemini-2.5-flash-lite") {
		minBudget = 512
	}
	if config.ThinkingLevel != "" {
		budget := geminiThinkingBudgetForLevel(config.ThinkingLevel, maxBudget)
		config.ThinkingBudget = common.GetPointer(budget)
		config.ThinkingLevel = ""
	}
	if config.ThinkingBudget != nil {
		budget := *config.ThinkingBudget
		if budget < 0 && budget != -1 {
			budget = 0
		}
		if budget > 0 {
			budget = max(minBudget, min(maxBudget, budget))
		} else if budget == 0 && strings.HasPrefix(model, "gemini-2.5-pro") {
			// Pro 不支持关闭思考；Flash-Lite 的显式零值需要保留。
			budget = minBudget
		}
		config.ThinkingBudget = common.GetPointer(budget)
	}
	return nil
}

func normalizeGeminiThinkingLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "minimal", "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(level))
	case "xhigh", "max":
		return "high"
	default:
		return "medium"
	}
}

func geminiThinkingLevelFromBudget(budget int) string {
	switch {
	case budget <= 0:
		return "minimal"
	case budget <= 1024:
		return "low"
	case budget <= 8192:
		return "medium"
	default:
		return "high"
	}
}

func geminiThinkingBudgetForLevel(level string, maxBudget int) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "minimal", "low":
		return min(maxBudget, 1024)
	case "medium":
		return min(maxBudget, 8192)
	case "high":
		return min(maxBudget, 24576)
	case "xhigh", "max":
		return maxBudget
	default:
		return maxBudget
	}
}

// NormalizeNativeThinkingRequest 统一 Gemini 原生与 Vertex 转发的思考参数。
func NormalizeNativeThinkingRequest(request *dto.GeminiChatRequest, info *relaycommon.RelayInfo) error {
	return normalizeGeminiThinkingConfig(request, info)
}
