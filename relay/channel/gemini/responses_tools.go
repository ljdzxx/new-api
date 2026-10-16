package gemini

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

const geminiResponsesToolNamesKey = "gemini_responses_tool_names"

type responsesToolName struct {
	Namespace string
	Name      string
}

// Gemini has a flat function list. Keep an explicit reverse map instead of
// splitting names on '__', which can also occur in an ordinary function name.
type responsesToolNames map[string]responsesToolName

func (names responsesToolNames) register(namespace, name string) (string, error) {
	namespace, name = strings.TrimSpace(namespace), strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("Responses tool is missing name")
	}
	// Codex's default functions namespace uses bare names on the response wire.
	if namespace == "functions" {
		namespace = ""
	}
	identity := responsesToolName{Namespace: namespace, Name: name}
	alias := name
	if namespace != "" {
		alias = namespace + "__" + name
		valid := len(alias) <= 64
		for i, ch := range alias {
			letter := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch == '_'
			if !letter && !(i > 0 && (ch >= '0' && ch <= '9' || ch == '.' || ch == ':' || ch == '-')) {
				valid = false
			}
		}
		if !valid {
			hash := sha256.Sum256([]byte(namespace + "\x00" + name))
			alias = fmt.Sprintf("ns_%x", hash[:16])
		}
	}
	if previous, ok := names[alias]; ok && previous != identity {
		return "", fmt.Errorf("Responses tool names collide after namespace conversion: %q", alias)
	}
	names[alias] = identity
	return alias, nil
}

func flattenGeminiResponsesTools(tools []map[string]any, names responsesToolNames, plan *responsesRequestPlan) ([]map[string]any, error) {
	var flattened []map[string]any
	appendTool := func(tool map[string]any, namespace, description string) error {
		copyTool := make(map[string]any, len(tool))
		for k, v := range tool {
			copyTool[k] = v
		}
		kind := responsesString(tool["type"])
		if kind == "function" || kind == "custom" {
			alias, err := names.register(namespace, responsesString(tool["name"]))
			if err != nil {
				if responsesString(tool["name"]) == "" {
					return err
				}
				plan.loss("tools", "namespaced_tool_dropped", err.Error()+"; the conflicting definition was omitted", true)
				return nil
			}
			copyTool["name"] = alias
			if description != "" {
				copyTool["description"] = strings.TrimSpace(description + "\n" + responsesString(tool["description"]))
			}
		}
		flattened = append(flattened, copyTool)
		return nil
	}
	for _, tool := range tools {
		if responsesString(tool["type"]) != "namespace" {
			if err := appendTool(tool, "", ""); err != nil {
				return nil, err
			}
			continue
		}
		namespace := responsesString(tool["name"])
		if strings.TrimSpace(namespace) == "" {
			return nil, fmt.Errorf("Responses namespace is missing name")
		}
		if namespace != "functions" {
			plan.loss("tools", "tool_namespace_flattened", fmt.Sprintf("tools in namespace %q were encoded as flat functions", namespace), false)
		}
		children, ok := tool["tools"].([]any)
		if !ok {
			return nil, fmt.Errorf("Responses namespace %q tools must be an array", namespace)
		}
		for _, child := range children {
			childTool, ok := child.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid tool in Responses namespace %q", namespace)
			}
			if err := appendTool(childTool, namespace, responsesString(tool["description"])); err != nil {
				return nil, err
			}
		}
	}
	return flattened, nil
}
