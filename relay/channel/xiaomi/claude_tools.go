package xiaomi

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func normalizeClaudeToolsForMimo(request *dto.ClaudeRequest) error {
	if request == nil || request.Tools == nil {
		return nil
	}

	switch tools := request.Tools.(type) {
	case []any:
		for i, tool := range tools {
			normalized, err := normalizeClaudeToolForMimo(tool)
			if err != nil {
				return err
			}
			tools[i] = normalized
		}
		request.Tools = tools
	case []dto.Tool:
		for i := range tools {
			if strings.TrimSpace(tools[i].Type) == "" {
				tools[i].Type = "custom"
			}
		}
		request.Tools = tools
	case []*dto.Tool:
		for _, tool := range tools {
			if tool != nil && strings.TrimSpace(tool.Type) == "" {
				tool.Type = "custom"
			}
		}
	default:
		var raw []map[string]any
		encoded, err := common.Marshal(request.Tools)
		if err != nil || common.Unmarshal(encoded, &raw) != nil {
			return nil
		}
		for _, tool := range raw {
			if strings.TrimSpace(common.Interface2String(tool["type"])) == "" {
				tool["type"] = "custom"
			}
		}
		request.Tools = raw
	}
	return nil
}

func normalizeClaudeToolForMimo(tool any) (any, error) {
	switch t := tool.(type) {
	case *dto.Tool:
		if t != nil && strings.TrimSpace(t.Type) == "" {
			t.Type = "custom"
		}
		return t, nil
	case dto.Tool:
		if strings.TrimSpace(t.Type) == "" {
			t.Type = "custom"
		}
		return t, nil
	case map[string]any:
		if strings.TrimSpace(common.Interface2String(t["type"])) == "" {
			t["type"] = "custom"
		}
		return t, nil
	}
	return tool, nil
}

func summarizeClaudeToolsForLog(tools any) string {
	if tools == nil {
		return "tools=0"
	}

	var rawTools []map[string]any
	encoded, err := common.Marshal(tools)
	if err != nil || common.Unmarshal(encoded, &rawTools) != nil {
		return fmt.Sprintf("tools_unmarshal_failed type=%T", tools)
	}

	types := make([]string, 0, len(rawTools))
	names := make([]string, 0, len(rawTools))
	maxUses := make([]string, 0, len(rawTools))
	for _, tool := range rawTools {
		types = append(types, strings.TrimSpace(common.Interface2String(tool["type"])))
		names = append(names, strings.TrimSpace(common.Interface2String(tool["name"])))
		if value, ok := tool["max_uses"]; ok {
			maxUses = append(maxUses, common.Interface2String(value))
		}
	}

	return fmt.Sprintf("tools=%d types=%v names=%v max_uses=%v", len(rawTools), types, names, maxUses)
}
