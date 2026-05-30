package common

import (
	"strings"

	basecommon "github.com/QuantumNous/new-api/common"
)

const minLikelyEncryptedContentLen = 32

// SanitizeInvalidResponsesEncryptedContent removes plainly invalid reasoning
// input items before forwarding Responses requests. Valid encrypted payloads
// are opaque and should be long ASCII blobs; visible markdown/natural-language
// text in encrypted_content causes OpenAI to reject the request. The whole
// reasoning item is removed because encrypted_content is required for that
// item type.
func SanitizeInvalidResponsesEncryptedContent(jsonData []byte) ([]byte, int, error) {
	var data map[string]any
	if err := basecommon.Unmarshal(jsonData, &data); err != nil {
		return jsonData, 0, err
	}

	input, ok := data["input"].([]any)
	if !ok {
		return jsonData, 0, nil
	}

	sanitizedInput := make([]any, 0, len(input))
	removed := 0
	for _, item := range input {
		itemMap, ok := item.(map[string]any)
		if !ok || itemMap["type"] != "reasoning" {
			sanitizedInput = append(sanitizedInput, item)
			continue
		}

		text, ok := itemMap["encrypted_content"].(string)
		if ok && isPlainlyInvalidEncryptedContent(text) {
			removed++
			continue
		}
		sanitizedInput = append(sanitizedInput, item)
	}

	if removed == 0 {
		return jsonData, 0, nil
	}
	data["input"] = sanitizedInput

	sanitized, err := basecommon.Marshal(data)
	if err != nil {
		return jsonData, removed, err
	}
	return sanitized, removed, nil
}

func isPlainlyInvalidEncryptedContent(value string) bool {
	if strings.TrimSpace(value) != value {
		return true
	}
	if len(value) < minLikelyEncryptedContentLen {
		return true
	}
	for _, r := range value {
		if r > 127 {
			return true
		}
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '+', r == '/', r == '=', r == '_', r == '-':
		default:
			return true
		}
	}
	return false
}
