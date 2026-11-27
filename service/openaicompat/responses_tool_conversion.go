package openaicompat

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/dto"
)

// ResponsesToolsToFunctions shares the reversible namespace/custom-tool mapping
// with protocols that need their own media and history conversion.
func ResponsesToolsToFunctions(tools, choice json.RawMessage) ([]dto.ToolCallRequest, any, map[string]dto.ResponsesToolIdentity, error) {
	return convertResponsesChatTools(tools, choice)
}

func ResponsesFunctionName(namespace, name string) (string, error) {
	return responsesChatName(namespace, name)
}
