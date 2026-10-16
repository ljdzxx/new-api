package service

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// geminiToolCallID 优先沿用 Gemini 的函数调用 ID。
func geminiToolCallID(id *string, fallback int) string {
	if id != nil && *id != "" {
		return *id
	}
	return fmt.Sprintf("call_%d", fallback)
}

// geminiFunctionResponseID 保留工具结果与原调用的关联。
func geminiFunctionResponseID(raw json.RawMessage, fallback int) string {
	var id string
	if len(raw) > 0 && common.Unmarshal(raw, &id) == nil && id != "" {
		return id
	}
	return fmt.Sprintf("call_%d", fallback)
}
