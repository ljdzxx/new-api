package gemini

// sanitizeGeminiEnum 删除旧版 typed Schema 不接受的非字符串枚举。
func sanitizeGeminiEnum(schema map[string]any) {
	raw, exists := schema["enum"]
	if !exists {
		return
	}
	values, ok := raw.([]any)
	if !ok {
		delete(schema, "enum")
		return
	}
	for _, value := range values {
		if _, ok := value.(string); !ok {
			delete(schema, "enum")
			return
		}
	}
}
