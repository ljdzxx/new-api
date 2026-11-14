package common

import (
	"fmt"
	"strings"
	"unicode/utf8"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	minLikelyEncryptedContentLen = 32
	maxResponsesItemIDLen        = 64
)

var responsesItemIDPrefixes = map[string]string{
	"message":          "msg",
	"reasoning":        "rs",
	"function_call":    "fc",
	"custom_tool_call": "ctc",
	"web_search_call":  "ws",
}

// SanitizeInvalidResponsesItemIDs removes replayed Responses item IDs that are
// too long or whose prefix does not match the item type. Some Responses-compatible
// providers emit non-standard IDs; Codex preserves those IDs in its history and
// strict upstreams reject them on the next request. The call_id field is
// intentionally preserved because it correlates tool calls with their outputs.
func SanitizeInvalidResponsesItemIDs(jsonData []byte) ([]byte, int, error) {
	if !gjson.ValidBytes(jsonData) {
		return jsonData, 0, fmt.Errorf("invalid Responses JSON")
	}
	input := gjson.GetBytes(jsonData, "input")
	if !input.IsArray() {
		return jsonData, 0, nil
	}
	removed := 0
	for i, item := range input.Array() {
		kind := item.Get("type").String()
		if kind == "" && item.Get("role").Exists() {
			kind = "message"
		}
		prefix, known := responsesItemIDPrefixes[kind]
		// Unknown/hosted items are opaque, including their identifiers.
		if !known {
			continue
		}
		id := item.Get("id")
		if id.Type != gjson.String || id.String() == "" {
			continue
		}
		if utf8.RuneCountInString(id.String()) <= maxResponsesItemIDLen && strings.HasPrefix(id.String(), prefix) {
			continue
		}
		var err error
		jsonData, err = deleteJSONPath(jsonData, fmt.Sprintf("input.%d.id", i))
		if err != nil {
			return jsonData, removed, err
		}
		removed++
	}
	return jsonData, removed, nil
}

// SanitizeInvalidResponsesEncryptedContent fixes plainly invalid encrypted_content
// fields before forwarding Responses requests. Valid encrypted payloads are
// opaque and should be long ASCII blobs; visible markdown/natural-language text
// in encrypted_content causes OpenAI to reject the request.
//
// Codex compaction history may carry a plaintext summary in a compaction item's
// encrypted_content field. Preserve that context by converting it to a regular
// user message. Other invalid encrypted_content items are removed because the
// field is required for those item types.
func SanitizeInvalidResponsesEncryptedContent(jsonData []byte) ([]byte, int, error) {
	if !gjson.ValidBytes(jsonData) {
		return jsonData, 0, fmt.Errorf("invalid Responses JSON")
	}
	return sanitizeEncryptedProtocolArray(jsonData, "input", true)
}

// Only visit protocol input items and message content. Tool outputs, agent
// messages and arbitrary metadata are opaque even if their keys look familiar.
func sanitizeEncryptedProtocolArray(body []byte, path string, topLevel bool) ([]byte, int, error) {
	array := gjson.GetBytes(body, path)
	if !array.IsArray() {
		return body, 0, nil
	}
	items := array.Array()
	removed := 0
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		kind := item.Get("type").String()
		itemPath := fmt.Sprintf("%s.%d", path, i)
		text := item.Get("encrypted_content")
		if (kind == "reasoning" || kind == "compaction") && text.Type == gjson.String && isPlainlyInvalidEncryptedContent(text.String()) {
			var err error
			if topLevel && kind == "compaction" {
				replacement, marshalErr := basecommon.Marshal(map[string]any{"type": "message", "role": "user", "content": []any{map[string]string{"type": "input_text", "text": text.String()}}})
				if marshalErr != nil {
					return body, removed, marshalErr
				}
				body, err = sjson.SetRawBytes(body, itemPath, replacement)
			} else {
				body, err = sjson.DeleteBytes(body, itemPath)
			}
			if err != nil {
				return body, removed, err
			}
			removed++
			continue
		}
		if topLevel && (kind == "message" || kind == "" && item.Get("role").Exists()) {
			updated, count, err := sanitizeEncryptedProtocolArray(body, itemPath+".content", false)
			if err != nil {
				return body, removed, err
			}
			body = updated
			removed += count
		}
	}
	return body, removed, nil
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
