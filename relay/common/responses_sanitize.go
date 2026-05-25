package common

import (
	"strings"

	basecommon "github.com/QuantumNous/new-api/common"
)

const minLikelyEncryptedContentLen = 32

// SanitizeInvalidResponsesEncryptedContent removes plainly invalid
// reasoning.encrypted_content values before forwarding Responses requests.
// Valid encrypted payloads are opaque and should be long ASCII blobs; visible
// markdown/natural-language text here causes OpenAI to reject the request.
func SanitizeInvalidResponsesEncryptedContent(jsonData []byte) ([]byte, int, error) {
	var data any
	if err := basecommon.Unmarshal(jsonData, &data); err != nil {
		return jsonData, 0, err
	}

	removed := sanitizeEncryptedContentValue(data)
	if removed == 0 {
		return jsonData, 0, nil
	}

	sanitized, err := basecommon.Marshal(data)
	if err != nil {
		return jsonData, removed, err
	}
	return sanitized, removed, nil
}

func sanitizeEncryptedContentValue(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		removed := 0
		for key, child := range typed {
			if key == "encrypted_content" {
				if text, ok := child.(string); ok && isPlainlyInvalidEncryptedContent(text) {
					delete(typed, key)
					removed++
					continue
				}
			}
			removed += sanitizeEncryptedContentValue(child)
		}
		return removed
	case []any:
		removed := 0
		for _, child := range typed {
			removed += sanitizeEncryptedContentValue(child)
		}
		return removed
	default:
		return 0
	}
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
