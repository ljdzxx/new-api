package common

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
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

var (
	codexSelectedPageName = regexp.MustCompile(`^codex_apps_selected_page_part_([1-9][0-9]*)_of_([1-9][0-9]*)$`)
	codexToolOutputTypes  = map[string]struct{}{
		"function_call_output":    {},
		"custom_tool_call_output": {},
		"tool_search_output":      {},
		"mcp_tool_call_output":    {},
	}
	codexToolCallTypes = map[string]struct{}{
		"function_call":     {},
		"custom_tool_call":  {},
		"tool_call":         {},
		"local_shell_call":  {},
		"tool_search_call":  {},
		"mcp_tool_call":     {},
	}
	codexToolOutputMarkers = [][]byte{
		[]byte("function_call_output"),
		[]byte("custom_tool_call_output"),
		[]byte("tool_search_output"),
		[]byte("mcp_tool_call_output"),
	}
)

// NormalizeCodexUntrustedContext rewrites Codex-injected external context into
// plain user messages so strict HTTP Responses upstreams accept the request.
//
// Codex 0.169+ sends additional context (browser info, goal objective, page
// snapshots) as standalone function_call_output items that have no call_id and
// no matching function_call. Some upstreams reject those on HTTP with
// "function_call_output requires call_id". The items are not real tool results,
// so the legacy user-message representation is semantically equivalent.
//
// Only items matching the exact namespace/name/output shape are rewritten.
// Real tool outputs are never modified and a call_id is never fabricated.
// Returns the rewritten body and the normalized item names.
func NormalizeCodexUntrustedContext(jsonData []byte) ([]byte, []string, error) {
	if !gjson.ValidBytes(jsonData) {
		return jsonData, nil, fmt.Errorf("invalid Responses JSON")
	}
	// Cheap pre-check: without a tool output item type no rewrite is possible.
	if !containsAnyToolOutputMarker(jsonData) {
		return jsonData, nil, nil
	}
	if !gjson.GetBytes(jsonData, "input").IsArray() {
		return jsonData, nil, nil
	}

	var normalized []string
	var rewriteErr error

	// Pass 1: known Codex external-context items get a labelled user message.
	gjson.GetBytes(jsonData, "input").ForEach(func(index, item gjson.Result) bool {
		kind := item.Get("type")
		if kind.Type != gjson.String || kind.Str != "function_call_output" {
			return true
		}
		namespace := item.Get("namespace")
		if namespace.Type != gjson.String || namespace.Str != "agent" {
			return true
		}
		// A present call_id that is anything but blank means a real tool result.
		if callID := item.Get("call_id"); callID.Exists() {
			if callID.Type != gjson.String || strings.TrimSpace(callID.Str) != "" {
				return true
			}
		}
		name := item.Get("name")
		output := item.Get("output")
		if name.Type != gjson.String || output.Type != gjson.String {
			return true
		}
		if !validCodexUntrustedContext(name.Str, output.Str) {
			return true
		}
		text := "<external_" + name.Str + ">" + output.Str + "</external_" + name.Str + ">"
		if validCodexPageContext(name.Str, output.Str) {
			text = "<external_" + name.Str + ">\n" + output.Str + "\n</external_" + name.Str + ">"
		}
		updated, err := replaceWithUserMessage(jsonData, index.Int(), text)
		if err != nil {
			rewriteErr = err
			return false
		}
		jsonData = updated
		normalized = append(normalized, name.Str)
		return true
	})
	if rewriteErr != nil {
		return jsonData, nil, rewriteErr
	}

	// Pass 2: dangling tool outputs. With no previous_response_id and no call
	// item carrying a call_id, a strict upstream rejects the body outright, so
	// rewriting the outputs beats forwarding a guaranteed 400.
	if !codexHasContinuationOrCall(jsonData) {
		gjson.GetBytes(jsonData, "input").ForEach(func(index, item gjson.Result) bool {
			kind := item.Get("type")
			if kind.Type != gjson.String {
				return true
			}
			if _, ok := codexToolOutputTypes[kind.Str]; !ok {
				return true
			}
			if callID := item.Get("call_id"); callID.Exists() {
				if callID.Type != gjson.String || strings.TrimSpace(callID.Str) != "" {
					return true
				}
			}
			text := codexToolOutputText(item)
			if strings.TrimSpace(text) == "" {
				return true
			}
			updated, err := replaceWithUserMessage(jsonData, index.Int(), text)
			if err != nil {
				rewriteErr = err
				return false
			}
			jsonData = updated
			normalized = append(normalized, "?"+codexToolOutputLabel(item, kind.Str))
			return true
		})
		if rewriteErr != nil {
			return jsonData, nil, rewriteErr
		}
	}

	if len(normalized) == 0 {
		return jsonData, nil, nil
	}
	return jsonData, normalized, nil
}

func containsAnyToolOutputMarker(jsonData []byte) bool {
	for _, marker := range codexToolOutputMarkers {
		if bytes.Contains(jsonData, marker) {
			return true
		}
	}
	return false
}

// codexHasContinuationOrCall mirrors the upstream acceptance rule: a non-empty
// previous_response_id, or any call item carrying a call_id, makes dangling tool
// outputs acceptable. Only when both are absent is the request guaranteed to fail.
func codexHasContinuationOrCall(jsonData []byte) bool {
	if strings.TrimSpace(gjson.GetBytes(jsonData, "previous_response_id").String()) != "" {
		return true
	}
	hasCall := false
	gjson.GetBytes(jsonData, "input").ForEach(func(_, item gjson.Result) bool {
		kind := item.Get("type")
		if kind.Type != gjson.String {
			return true
		}
		if _, ok := codexToolCallTypes[kind.Str]; !ok {
			return true
		}
		if callID := item.Get("call_id"); callID.Exists() && callID.Type == gjson.String && strings.TrimSpace(callID.Str) != "" {
			hasCall = true
			return false
		}
		return true
	})
	return hasCall
}

func codexToolOutputText(item gjson.Result) string {
	for _, field := range []string{"output", "content"} {
		value := item.Get(field)
		if !value.Exists() {
			continue
		}
		if value.Type == gjson.String {
			return value.Str
		}
		return value.Raw
	}
	return ""
}

func codexToolOutputLabel(item gjson.Result, kind string) string {
	if name := strings.TrimSpace(item.Get("name").String()); name != "" {
		return name
	}
	return kind
}

func replaceWithUserMessage(jsonData []byte, index int64, text string) ([]byte, error) {
	message, err := basecommon.Marshal(map[string]any{
		"type": "message",
		"role": "user",
		"content": []map[string]string{{
			"type": "input_text",
			"text": text,
		}},
	})
	if err != nil {
		return jsonData, err
	}
	return sjson.SetRawBytes(jsonData, fmt.Sprintf("input.%d", index), message)
}

func validCodexUntrustedContext(name, output string) bool {
	switch name {
	case "browser_info", "goal_objective", "untrusted_context":
		return strings.TrimSpace(output) != ""
	default:
		return validCodexPageContext(name, output)
	}
}

func validCodexPageContext(name, output string) bool {
	if name == "codex_apps_open_page" {
		return validCodexPageSnapshot(output)
	}
	parts := codexSelectedPageName.FindStringSubmatch(name)
	if len(parts) != 3 {
		return false
	}
	part, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	total, err := strconv.Atoi(parts[2])
	if err != nil || part > total {
		return false
	}
	header, snapshot, ok := strings.Cut(output, "\n")
	if !ok || strings.TrimSuffix(header, "\r") != fmt.Sprintf("Companion Page snapshot part %d of %d:", part, total) || strings.TrimSpace(snapshot) == "" {
		return false
	}
	// A multi-part snapshot may split inside a JSON string; preserve each chunk verbatim.
	return total > 1 || validCodexPageSnapshot(snapshot)
}

func validCodexPageSnapshot(text string) bool {
	if !gjson.Valid(text) {
		return false
	}
	snapshot := gjson.Parse(text)
	if !snapshot.IsObject() {
		return false
	}
	id := snapshot.Get("page_id")
	if !id.Exists() {
		return false
	}
	if id.Type == gjson.Null {
		return true
	}
	return id.Type == gjson.String && strings.TrimSpace(id.Str) != ""
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
