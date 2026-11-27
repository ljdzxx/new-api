package xiaomi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func normalizeClaudeToolsForMimo(request *dto.ClaudeRequest) error {
	if request == nil || request.Tools == nil {
		return nil
	}

	switch tools := request.Tools.(type) {
	case json.RawMessage:
		var items []json.RawMessage
		if err := common.Unmarshal(tools, &items); err != nil {
			return err
		}
		normalizedTools := make([]any, 0, len(items))
		for _, item := range items {
			normalized, err := normalizeRawClaudeToolForMimo(item)
			if err != nil {
				return err
			}
			normalizedTools = append(normalizedTools, normalized)
		}
		request.Tools = normalizedTools
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
	case json.RawMessage:
		return normalizeRawClaudeToolForMimo(t)
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

func normalizeRawClaudeToolForMimo(tool json.RawMessage) (json.RawMessage, error) {
	if common.GetJsonType(tool) != "object" || strings.TrimSpace(gjson.GetBytes(tool, "type").String()) != "" {
		return tool, nil
	}
	return sjson.SetBytes(tool, "type", "custom")
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
