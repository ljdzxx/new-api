package common

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// RebuildResponsesInputWithoutReasoning is a lossy fallback for an upstream
// invalid_encrypted_content error or HTTP 502/503 with encrypted reasoning,
// never a normal request sanitizer. It only replays self-contained messages
// and paired client tool calls/results. Opaque compaction and server-side
// history cannot be recovered from this request.
// No history or ciphertext is retained beyond the current request.
func RebuildResponsesInputWithoutReasoning(body []byte) ([]byte, int, error) {
	if !gjson.ValidBytes(body) {
		return nil, 0, fmt.Errorf("invalid responses JSON")
	}
	for _, field := range []string{"previous_response_id", "conversation"} {
		value := gjson.GetBytes(body, field)
		if value.Exists() && value.Type != gjson.Null && value.Raw != `""` {
			return nil, 0, fmt.Errorf("server-side history reference: %s", field)
		}
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return nil, 0, nil
	}

	pendingCalls := make(map[string]string)
	seenCalls := make(map[string]bool)
	removed, removedBytes, retained := 0, 0, 0
	hasEncryptedReasoning := false
	var invalid error
	input.ForEach(func(_, item gjson.Result) bool {
		if !item.IsObject() {
			invalid = fmt.Errorf("non-object responses input item")
			return false
		}
		itemType := item.Get("type").String()
		if itemType == "reasoning" {
			removed++
			removedBytes += len(item.Raw)
			ciphertext := item.Get("encrypted_content")
			hasEncryptedReasoning = hasEncryptedReasoning || (ciphertext.Type == gjson.String && ciphertext.Str != "")
			return true
		}
		if itemType == "compaction" {
			invalid = fmt.Errorf("compaction history is not recoverable from the current request")
			return false
		}
		if item.Get("encrypted_content").Exists() {
			invalid = fmt.Errorf("encrypted non-reasoning input item")
			return false
		}
		switch itemType {
		case "", "message":
			role := item.Get("role").String()
			content := item.Get("content")
			if (role != "user" && role != "assistant" && role != "system" && role != "developer") ||
				(content.Type != gjson.String && !content.IsArray()) {
				invalid = fmt.Errorf("message is not self-contained")
			}
		case "function_call", "custom_tool_call":
			callID := item.Get("call_id").String()
			payloadField := "arguments"
			if itemType == "custom_tool_call" {
				payloadField = "input"
			}
			if callID == "" || seenCalls[callID] || item.Get("name").String() == "" || item.Get(payloadField).Type != gjson.String {
				invalid = fmt.Errorf("incomplete or duplicate tool call")
			} else {
				seenCalls[callID] = true
				pendingCalls[callID] = itemType + "_output"
			}
		case "function_call_output", "custom_tool_call_output":
			callID := item.Get("call_id").String()
			output := item.Get("output")
			if callID == "" || pendingCalls[callID] != itemType || (output.Type != gjson.String && !output.IsArray()) {
				invalid = fmt.Errorf("tool result has no matching preceding call or content")
			} else {
				delete(pendingCalls, callID)
			}
		default:
			// Item references and hosted-tool state need provider-side objects.
			invalid = fmt.Errorf("input contains an unsupported replay item")
		}
		retained++
		return invalid == nil
	})
	if invalid != nil {
		return nil, 0, invalid
	}
	if len(pendingCalls) != 0 {
		return nil, 0, fmt.Errorf("tool call has no matching result")
	}
	if !hasEncryptedReasoning || retained == 0 {
		return nil, 0, nil
	}

	// Copy raw JSON rather than decoding arbitrary parameters/tool output into
	// float64 or retaining a second parsed history tree. Explicit zero/false,
	// large integers, multimodal content and unknown request fields survive.
	rebuilt := make([]byte, 0, len(body)-removedBytes)
	rebuilt = append(rebuilt, body[:input.Index]...)
	rebuilt = append(rebuilt, '[')
	first := true
	input.ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() == "reasoning" {
			return true
		}
		raw := item.Raw
		if item.Get("id").Exists() {
			// These whitelisted items carry their full content. call_id and
			// nested file/resource IDs must remain untouched.
			raw, invalid = sjson.Delete(raw, "id")
			if invalid != nil {
				return false
			}
		}
		if !first {
			rebuilt = append(rebuilt, ',')
		}
		first = false
		rebuilt = append(rebuilt, raw...)
		return true
	})
	if invalid != nil {
		return nil, 0, invalid
	}
	rebuilt = append(rebuilt, ']')
	rebuilt = append(rebuilt, body[input.Index+len(input.Raw):]...)
	return rebuilt, removed, nil
}
