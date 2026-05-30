package common

import (
	"strings"

	basecommon "github.com/QuantumNous/new-api/common"
)

const minLikelyEncryptedContentLen = 32

// SanitizeInvalidResponsesEncryptedContent removes plainly invalid input items
// before forwarding Responses requests. Valid encrypted payloads are opaque and
// should be long ASCII blobs; visible markdown/natural-language text in
// encrypted_content causes OpenAI to reject the request. The whole item is
// removed because encrypted_content is required for reasoning items.
func SanitizeInvalidResponsesEncryptedContent(jsonData []byte) ([]byte, int, error) {
	var data map[string]any
	if err := basecommon.Unmarshal(jsonData, &data); err != nil {
		return jsonData, 0, err
	}

	input, exists := data["input"]
	if !exists {
		return jsonData, 0, nil
	}

	sanitizedInput, removed := sanitizeInvalidEncryptedContentItems(input)
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

func sanitizeInvalidEncryptedContentItems(value any) (any, int) {
	switch typed := value.(type) {
	case []any:
		sanitized := make([]any, 0, len(typed))
		removed := 0
		changed := false
		for _, child := range typed {
			if childMap, ok := child.(map[string]any); ok && hasPlainlyInvalidEncryptedContent(childMap) {
				removed++
				changed = true
				continue
			}

			sanitizedChild, childRemoved := sanitizeInvalidEncryptedContentItems(child)
			if childRemoved > 0 {
				changed = true
			}
			removed += childRemoved
			sanitized = append(sanitized, sanitizedChild)
		}
		if !changed {
			return value, 0
		}
		return sanitized, removed
	case map[string]any:
		removed := 0
		for key, child := range typed {
			sanitizedChild, childRemoved := sanitizeInvalidEncryptedContentItems(child)
			if childRemoved > 0 {
				typed[key] = sanitizedChild
				removed += childRemoved
			}
		}
		return typed, removed
	default:
		return value, 0
	}
}

func hasPlainlyInvalidEncryptedContent(item map[string]any) bool {
	text, ok := item["encrypted_content"].(string)
	return ok && isPlainlyInvalidEncryptedContent(text)
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
