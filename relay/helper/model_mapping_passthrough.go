package helper

import (
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ApplyModelMappingToPassthroughBody applies the fields changed by model
// mapping to an otherwise untouched request body.  Passthrough requests may
// contain provider-specific fields that are not represented by a DTO, so the
// body is patched in-place instead of being re-marshaled from request.
//
// A non-JSON body (for example multipart image data) is returned unchanged;
// such bodies cannot be safely patched without changing their encoding.
func ApplyModelMappingToPassthroughBody(body []byte, info *relaycommon.RelayInfo, request dto.Request) ([]byte, error) {
	if info == nil || !info.IsModelMapped || len(body) == 0 || !gjson.ValidBytes(body) {
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
	effortChanged := info.ReasoningEffort != "" && info.ReasoningEffort != info.ClientReasoningEffort
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
			result, err = sjson.SetBytes(result, "output_config.effort", info.ReasoningEffort)
		case *dto.GeminiChatRequest:
			// Gemini's effort adapter uses thinkingLevel in generationConfig.
			result, err = setGeminiThinkingField(result, "thinkingLevel", info.ReasoningEffort)
		}
		if err != nil {
			return body, err
		}
	}

	// Claude/Gemini adapters can derive thinking settings from the mapped
	// target model (for example a -thinking suffix) even when no explicit
	// effort was present in the client request. Mirror those normalized fields
	// into the passthrough body as well.
	if effortChanged {
		switch req := request.(type) {
		case *dto.ClaudeRequest:
			if req.GetEfforts() != "" {
				result, err = sjson.SetBytes(result, "output_config.effort", req.GetEfforts())
			}
		case *dto.GeminiChatRequest:
			if config := req.GenerationConfig.ThinkingConfig; config != nil {
				result, err = setGeminiThinkingConfig(result, config)
			}
		}
	}
	if err != nil {
		return body, err
	}
	return result, nil
}

func setGeminiThinkingField(body []byte, field, value string) ([]byte, error) {
	if value == "" {
		return body, nil
	}
	path := geminiThinkingPath(body, field)
	return sjson.SetBytes(body, path, value)
}

func setGeminiThinkingConfig(body []byte, config *dto.GeminiThinkingConfig) ([]byte, error) {
	if config == nil {
		return body, nil
	}
	var err error
	if config.ThinkingLevel != "" {
		body, err = setGeminiThinkingField(body, "thinkingLevel", config.ThinkingLevel)
		if err != nil {
			return body, err
		}
	}
	if config.ThinkingBudget != nil {
		path := geminiThinkingPath(body, "thinkingBudget")
		body, err = sjson.SetBytes(body, path, *config.ThinkingBudget)
		if err != nil {
			return body, err
		}
	}
	if config.IncludeThoughts {
		path := geminiThinkingPath(body, "includeThoughts")
		body, err = sjson.SetBytes(body, path, true)
	}
	return body, err
}

func geminiThinkingPath(body []byte, field string) string {
	camelField := field
	snake := field
	switch field {
	case "thinkingLevel":
		snake = "thinking_level"
	case "thinkingBudget":
		snake = "thinking_budget"
	case "includeThoughts":
		snake = "include_thoughts"
	}
	paths := []struct {
		container string
		config    string
		field     string
	}{
		{"generationConfig", "thinkingConfig", camelField},
		{"generationConfig", "thinking_config", snake},
		{"generation_config", "thinkingConfig", camelField},
		{"generation_config", "thinking_config", snake},
	}
	for _, candidate := range paths {
		if gjson.GetBytes(body, candidate.container+"."+candidate.config).Exists() {
			return candidate.container + "." + candidate.config + "." + candidate.field
		}
	}
	if gjson.GetBytes(body, "generation_config").Exists() {
		return "generation_config.thinking_config." + snake
	}
	if gjson.GetBytes(body, "generationConfig").Exists() {
		return "generationConfig.thinkingConfig." + camelField
	}
	// Gemini accepts camelCase by default; use it when the target object was
	// omitted and must be created.
	return "generationConfig.thinkingConfig." + camelField
}
