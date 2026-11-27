package helper

import (
	"reflect"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// GeminiPassthroughThinkingChanged also accounts for normalization without a channel mapping.
func GeminiPassthroughThinkingChanged(info *relaycommon.RelayInfo, req *dto.GeminiChatRequest) bool {
	if info.ChannelMappedReasoningEffort != "" {
		return true
	}
	if original, ok := info.Request.(*dto.GeminiChatRequest); ok && original != nil {
		return !reflect.DeepEqual(original.GenerationConfig.ThinkingConfig, req.GenerationConfig.ThinkingConfig)
	}
	return info.ClientReasoningEffort != "" && info.ReasoningEffort != info.ClientReasoningEffort
}

// ApplyModelMappingToPassthroughBody applies the fields changed by model
// mapping to an otherwise untouched request body.  Passthrough requests may
// contain provider-specific fields that are not represented by a DTO, so the
// body is patched in-place instead of being re-marshaled from request.
//
// A non-JSON body (for example multipart image data) is returned unchanged;
// such bodies cannot be safely patched without changing their encoding.
func ApplyModelMappingToPassthroughBody(body []byte, info *relaycommon.RelayInfo, request dto.Request) ([]byte, error) {
	if info == nil || len(body) == 0 {
		return body, nil
	}

	if req, ok := request.(*dto.GeminiChatRequest); ok {
		if req == nil || !GeminiPassthroughThinkingChanged(info, req) || !gjson.ValidBytes(body) {
			return body, nil
		}
		return setGeminiThinkingConfig(body, req.GenerationConfig.ThinkingConfig)
	}
	if !info.IsModelMapped || !gjson.ValidBytes(body) {
		return body, nil
	}
	result := body
	// These request formats carry the model in the JSON body. Gemini and
	// several task-specific formats put it in the URL or form data instead.
	modelInBody := false
	switch request.(type) {
	case *dto.GeneralOpenAIRequest, *dto.OpenAIResponsesRequest, *dto.OpenAIResponsesCompactionRequest,
		*dto.ClaudeRequest, *dto.ImageRequest, *dto.RerankRequest:
		modelInBody = true
	}
	if modelInBody && info.UpstreamModelName != "" {
		var err error
		result, err = sjson.SetBytes(result, "model", info.UpstreamModelName)
		if err != nil {
			return body, err
		}
	}

	// Only rewrite reasoning when the mapping changed the requested effort.
	// A model-only mapping must preserve the client's original effort.
	// Responses compaction has an explicit upstream whitelist that excludes
	// reasoning; model mapping still uses the client effort for rule matching,
	// but must not add it back to a passthrough compaction payload.
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		return sjson.DeleteBytes(result, "reasoning")
	}
	effortChanged := info.ChannelMappedReasoningEffort != "" || info.ReasoningEffort != "" && info.ReasoningEffort != info.ClientReasoningEffort
	var err error
	if effortChanged {
		switch req := request.(type) {
		case *dto.GeneralOpenAIRequest:
			// OpenAI-compatible APIs use reasoning_effort.  OpenRouter-style
			// requests may additionally carry a nested reasoning object.
			hasTopLevelEffort := req.ReasoningEffort != "" || gjson.GetBytes(result, "reasoning_effort").Exists()
			hasNestedEffort := gjson.GetBytes(result, "reasoning").Type == gjson.JSON && gjson.GetBytes(result, "reasoning").IsObject()
			if hasTopLevelEffort || !hasNestedEffort {
				result, err = sjson.SetBytes(result, "reasoning_effort", info.ReasoningEffort)
				if err != nil {
					return body, err
				}
			}
			if hasNestedEffort {
				result, err = sjson.SetBytes(result, "reasoning.effort", info.ReasoningEffort)
			}
		case *dto.OpenAIResponsesRequest, *dto.OpenAIResponsesCompactionRequest:
			// Responses API carries effort under reasoning.effort.  Set the object
			// even when it was absent so a target effort cannot be bypassed.
			result, err = sjson.SetBytes(result, "reasoning.effort", info.ReasoningEffort)
		case *dto.ClaudeRequest:
			// Claude's adaptive-thinking effort is represented by output_config.
			effort := req.GetEfforts()
			if effort == "" {
				effort = info.ReasoningEffort
			}
			result, err = sjson.SetBytes(result, "output_config.effort", effort)
		}
		if err != nil {
			return body, err
		}
	}
	return result, nil
}

// Patch only the normalized thinking fields. Preserve provider extensions and the input's field style.
func setGeminiThinkingConfig(body []byte, config *dto.GeminiThinkingConfig) ([]byte, error) {
	// A client may supply both supported spellings. Update each existing block
	// so the provider's alias precedence cannot restore the old level or budget.
	found := false
	for _, container := range []string{"generationConfig", "generation_config"} {
		generation := gjson.GetBytes(body, container)
		if !generation.IsObject() {
			continue
		}
		for _, thinking := range []string{"thinkingConfig", "thinking_config"} {
			if !generation.Get(thinking).Exists() {
				continue
			}
			found = true
			var err error
			body, err = patchGeminiThinkingBlock(body, container+"."+thinking, thinking == "thinking_config", config)
			if err != nil {
				return nil, err
			}
		}
	}
	if found || config == nil {
		return body, nil
	}
	if gjson.GetBytes(body, "generation_config").Exists() {
		return patchGeminiThinkingBlock(body, "generation_config.thinking_config", true, config)
	}
	return patchGeminiThinkingBlock(body, "generationConfig.thinkingConfig", false, config)
}

func patchGeminiThinkingBlock(body []byte, path string, snakeCase bool, config *dto.GeminiThinkingConfig) ([]byte, error) {
	if config == nil {
		return sjson.DeleteBytes(body, path)
	}
	block := []byte(gjson.GetBytes(body, path).Raw)
	if len(block) == 0 || !gjson.ParseBytes(block).IsObject() {
		block = []byte("{}")
	}
	var level, budget, include any
	if config.ThinkingLevel != "" {
		level = config.ThinkingLevel
	}
	if config.ThinkingBudget != nil {
		budget = *config.ThinkingBudget
	}
	if config.IncludeThoughts != nil {
		include = *config.IncludeThoughts
	}
	for _, field := range []struct {
		camel, snake string
		value        any
	}{
		{"thinkingLevel", "thinking_level", level},
		{"thinkingBudget", "thinking_budget", budget},
		{"includeThoughts", "include_thoughts", include},
	} {
		key, alias := field.camel, field.snake
		if snakeCase {
			key, alias = alias, key
		}
		var err error
		block, err = sjson.DeleteBytes(block, alias)
		if err != nil {
			return nil, err
		}
		if field.value == nil {
			block, err = sjson.DeleteBytes(block, key)
		} else {
			block, err = sjson.SetBytes(block, key, field.value)
		}
		if err != nil {
			return nil, err
		}
	}
	return sjson.SetRawBytes(body, path, block)
}
