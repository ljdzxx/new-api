package openaicompat

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func responsesChatName(namespace, name string) (string, error) {
	namespace, name = strings.TrimSpace(namespace), strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("tool name is required")
	}
	if namespace == "functions" {
		namespace = ""
	}
	alias := name
	if namespace != "" {
		alias = namespace + "__" + name
	}
	valid := len(alias) <= 64
	for _, r := range alias {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			valid = false
		}
	}
	if !valid {
		hash := sha256.Sum256([]byte(namespace + "\x00" + name))
		alias = fmt.Sprintf("tool_%x", hash[:16])
	}
	return alias, nil
}
func responsesChatOutput(value any) (string, error) {
	if text, ok := value.(string); ok {
		return text, nil
	}
	data, err := common.Marshal(value)
	return string(data), err
}
func convertResponsesChatTools(raw, rawChoice json.RawMessage) ([]dto.ToolCallRequest, any, map[string]dto.ResponsesToolIdentity, error) {
	mapping := map[string]dto.ResponsesToolIdentity{}
	var tools []dto.ToolCallRequest
	var definitions []map[string]json.RawMessage
	if len(raw) > 0 {
		if err := common.Unmarshal(raw, &definitions); err != nil {
			return nil, nil, nil, err
		}
	}
	var appendTool func(map[string]json.RawMessage, string) error
	appendTool = func(tool map[string]json.RawMessage, namespace string) error {
		var kind, name, description string
		_ = common.Unmarshal(tool["type"], &kind)
		_ = common.Unmarshal(tool["name"], &name)
		_ = common.Unmarshal(tool["description"], &description)
		if kind == "namespace" {
			if namespace != "" || name == "" {
				return fmt.Errorf("invalid nested or unnamed namespace")
			}
			var children []map[string]json.RawMessage
			if err := common.Unmarshal(tool["tools"], &children); err != nil {
				return err
			}
			for _, child := range children {
				if err := appendTool(child, name); err != nil {
					return err
				}
			}
			return nil
		}
		parameters := tool["parameters"]
		strict := tool["strict"]
		if kind == "function" && len(tool["function"]) > 0 {
			var fn map[string]json.RawMessage
			if err := common.Unmarshal(tool["function"], &fn); err != nil {
				return err
			}
			if name == "" {
				_ = common.Unmarshal(fn["name"], &name)
			}
			if description == "" {
				_ = common.Unmarshal(fn["description"], &description)
			}
			if len(parameters) == 0 {
				parameters = fn["parameters"]
			}
			if len(strict) == 0 {
				strict = fn["strict"]
			}
		}
		if kind != "function" && kind != "custom" {
			return fmt.Errorf("Responses tool %q cannot be represented in Chat Completions", kind)
		}
		alias, err := responsesChatName(namespace, name)
		if err != nil {
			return err
		}
		if namespace == "functions" {
			namespace = ""
		}
		identity := dto.ResponsesToolIdentity{Namespace: namespace, Name: name, Custom: kind == "custom"}
		if _, exists := mapping[alias]; exists {
			return fmt.Errorf("duplicate or conflicting tool alias %q", alias)
		}
		mapping[alias] = identity
		if kind == "custom" {
			var format struct {
				Type string `json:"type"`
			}
			if len(tool["format"]) > 0 {
				if err := common.Unmarshal(tool["format"], &format); err != nil {
					return err
				}
			}
			if format.Type != "" && format.Type != "text" {
				return fmt.Errorf("custom grammar constraints require a native Responses upstream")
			}
			parameters = json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"],"additionalProperties":false}`)
		}
		fn := dto.FunctionRequest{Name: alias, Description: description}
		if len(strict) > 0 {
			if err := common.Unmarshal(strict, &fn.Strict); err != nil {
				return err
			}
		}
		if len(parameters) > 0 {
			fn.Parameters = parameters
		}
		tools = append(tools, dto.ToolCallRequest{Type: "function", Function: fn})
		return nil
	}
	for _, tool := range definitions {
		if err := appendTool(tool, ""); err != nil {
			return nil, nil, nil, err
		}
	}
	var choice any
	if len(rawChoice) > 0 {
		if err := common.UnmarshalUseNumber(rawChoice, &choice); err != nil {
			return nil, nil, nil, err
		}
		if object, ok := choice.(map[string]any); ok {
			kind, _ := object["type"].(string)
			if kind != "function" && kind != "custom" {
				return nil, nil, nil, fmt.Errorf("unsupported tool_choice type %q", kind)
			}
			name, _ := object["name"].(string)
			ns, _ := object["namespace"].(string)
			if name == "" {
				if fn, ok := object["function"].(map[string]any); ok {
					name, _ = fn["name"].(string)
				}
			}
			alias, err := responsesChatName(ns, name)
			if err != nil {
				return nil, nil, nil, err
			}
			choice = map[string]any{"type": "function", "function": map[string]string{"name": alias}}
		}
	}
	return tools, choice, mapping, nil
}

// Use the exact request mapping; never infer a namespace by splitting '__'.
func ChatCompletionsResponseToResponsesResponseWithTools(resp *dto.OpenAITextResponse, mapping map[string]dto.ResponsesToolIdentity) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	out, usage, err := ChatCompletionsResponseToResponsesResponse(resp)
	if err != nil {
		return nil, nil, err
	}
	for i := range out.Output {
		item := &out.Output[i]
		if item.Type != "function_call" {
			continue
		}
		identity, ok := mapping[item.Name]
		if !ok {
			continue
		}
		item.Name = identity.Name
		item.Namespace = identity.Namespace
		if identity.Custom {
			var args struct {
				Input *string `json:"input"`
			}
			if err := common.UnmarshalJsonStr(item.ArgumentsString(), &args); err != nil || args.Input == nil {
				return nil, nil, fmt.Errorf("invalid converted custom tool input")
			}
			item.Type = "custom_tool_call"
			item.ID = "ctc_" + strings.TrimPrefix(item.CallId, "call_")
			item.Input = args.Input
			item.Arguments = nil
		}
	}
	return out, usage, nil
}
