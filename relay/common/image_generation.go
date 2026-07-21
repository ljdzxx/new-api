package common

import (
	"fmt"
	"strconv"
	"strings"

	appcommon "github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const imageGenerationToolType = "image_generation"

// ShouldStripImageGeneration reports whether image_generation must be removed
// from an outbound request. Image API requests remain untouched.
func ShouldStripImageGeneration(relayMode int) bool {
	if !appcommon.DisableImageGenerationOnNonImageEndpoints {
		return false
	}
	return relayMode != relayconstant.RelayModeImagesGenerations &&
		relayMode != relayconstant.RelayModeImagesEdits
}

// StripImageGenerationTool removes the hosted image_generation tool and a
// tool_choice that explicitly selects it. Other tools and choices are retained.
func StripImageGenerationTool(data []byte) ([]byte, int, error) {
	if len(data) == 0 {
		return data, 0, nil
	}
	if !gjson.ValidBytes(data) {
		return nil, 0, fmt.Errorf("invalid JSON request body")
	}

	result := data
	removed := 0
	tools := gjson.GetBytes(result, "tools")
	if tools.IsArray() {
		items := tools.Array()
		for i := len(items) - 1; i >= 0; i-- {
			if !strings.EqualFold(strings.TrimSpace(items[i].Get("type").String()), imageGenerationToolType) {
				continue
			}
			var err error
			result, err = sjson.DeleteBytes(result, "tools."+strconv.Itoa(i))
			if err != nil {
				return nil, removed, fmt.Errorf("remove image_generation tool: %w", err)
			}
			removed++
		}
	}

	if toolChoiceSelectsImageGeneration(gjson.GetBytes(result, "tool_choice")) {
		var err error
		result, err = sjson.DeleteBytes(result, "tool_choice")
		if err != nil {
			return nil, removed, fmt.Errorf("remove image_generation tool_choice: %w", err)
		}
		removed++
	}

	return result, removed, nil
}

func toolChoiceSelectsImageGeneration(choice gjson.Result) bool {
	if !choice.Exists() {
		return false
	}
	if choice.Type == gjson.String {
		return strings.EqualFold(strings.TrimSpace(choice.String()), imageGenerationToolType)
	}
	if !choice.IsObject() {
		return false
	}
	choiceType := strings.TrimSpace(choice.Get("type").String())
	if strings.EqualFold(choiceType, imageGenerationToolType) {
		return true
	}
	return strings.EqualFold(choiceType, "tool") &&
		strings.EqualFold(strings.TrimSpace(choice.Get("name").String()), imageGenerationToolType)
}
