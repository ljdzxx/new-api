package common

import (
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
)

func TestSanitizeInvalidResponsesEncryptedContentRemovesInvalidReasoningItem(t *testing.T) {
	input := []byte(`{
		"model":"gpt-5.5",
		"input":[
			{
				"type":"reasoning",
				"summary":[{"type":"summary_text","text":"ok"}],
				"encrypted_content":"**需要按步骤进行处理。"
			},
			{"role":"user","content":"继续"}
		]
	}`)

	out, removed, err := SanitizeInvalidResponsesEncryptedContent(input)
	if err != nil {
		t.Fatalf("SanitizeInvalidResponsesEncryptedContent returned error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	var payload map[string]any
	if err := basecommon.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	items := payload["input"].([]any)
	if len(items) != 1 {
		t.Fatalf("input length = %d, want 1", len(items))
	}
	user := items[0].(map[string]any)
	if got := user["role"]; got != "user" {
		t.Fatalf("remaining item role = %v, want user", got)
	}
}

func TestSanitizeInvalidResponsesEncryptedContentConvertsPlaintextCompaction(t *testing.T) {
	input := []byte(`{
		"model":"gpt-5.5",
		"input":[
			{
				"type":"compaction",
				"encrypted_content":"- **User requirements**\n  - Keep traceable evidence.\n  - Do not fabricate conclusions."
			},
			{"role":"user","content":"继续"}
		]
	}`)

	out, removed, err := SanitizeInvalidResponsesEncryptedContent(input)
	if err != nil {
		t.Fatalf("SanitizeInvalidResponsesEncryptedContent returned error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	var payload map[string]any
	if err := basecommon.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	items := payload["input"].([]any)
	if len(items) != 2 {
		t.Fatalf("input length = %d, want 2", len(items))
	}
	converted := items[0].(map[string]any)
	if got := converted["type"]; got != "message" {
		t.Fatalf("converted type = %v, want message", got)
	}
	if got := converted["role"]; got != "user" {
		t.Fatalf("converted role = %v, want user", got)
	}
	if _, exists := converted["encrypted_content"]; exists {
		t.Fatalf("converted item still has encrypted_content")
	}
	content := converted["content"].([]any)
	text := content[0].(map[string]any)
	if got := text["type"]; got != "input_text" {
		t.Fatalf("converted content type = %v, want input_text", got)
	}
	if got := text["text"]; got != "- **User requirements**\n  - Keep traceable evidence.\n  - Do not fabricate conclusions." {
		t.Fatalf("converted text = %v", got)
	}
}

func TestSanitizeInvalidResponsesEncryptedContentPreservesLikelyBlob(t *testing.T) {
	blob := "gAAAAABmYXBwLWVuY3J5cHRlZF9jb250ZW50XzEyMzQ1Njc4OTAxMjM0NTY3ODkw"
	input := []byte(`{"input":[{"type":"reasoning","encrypted_content":"` + blob + `"}]}`)

	out, removed, err := SanitizeInvalidResponsesEncryptedContent(input)
	if err != nil {
		t.Fatalf("SanitizeInvalidResponsesEncryptedContent returned error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}

	var payload map[string]any
	if err := basecommon.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	items := payload["input"].([]any)
	reasoning := items[0].(map[string]any)
	if got := reasoning["encrypted_content"]; got != blob {
		t.Fatalf("encrypted_content = %v, want %s", got, blob)
	}
}

func TestSanitizeInvalidResponsesEncryptedContentDoesNotRemoveNestedMetadata(t *testing.T) {
	input := []byte(`{
		"input":[{"role":"user","content":"hi"}],
		"metadata":{"encrypted_content":"visible metadata is not a reasoning item"}
	}`)

	out, removed, err := SanitizeInvalidResponsesEncryptedContent(input)
	if err != nil {
		t.Fatalf("SanitizeInvalidResponsesEncryptedContent returned error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}

	var payload map[string]any
	if err := basecommon.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	metadata := payload["metadata"].(map[string]any)
	if got := metadata["encrypted_content"]; got != "visible metadata is not a reasoning item" {
		t.Fatalf("metadata encrypted_content = %v", got)
	}
}

func TestSanitizeInvalidResponsesEncryptedContentRemovesNestedInvalidItem(t *testing.T) {
	input := []byte(`{
		"input":[
			{
				"role":"assistant",
				"content":[
					{
						"type":"reasoning",
						"encrypted_content":"**需要按步骤进行处理，然后按主题分组）。"
					},
					{"type":"output_text","text":"ok"}
				]
			},
			{"role":"user","content":"继续"}
		]
	}`)

	out, removed, err := SanitizeInvalidResponsesEncryptedContent(input)
	if err != nil {
		t.Fatalf("SanitizeInvalidResponsesEncryptedContent returned error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	var payload map[string]any
	if err := basecommon.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	inputItems := payload["input"].([]any)
	assistant := inputItems[0].(map[string]any)
	content := assistant["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content length = %d, want 1", len(content))
	}
	text := content[0].(map[string]any)
	if got := text["type"]; got != "output_text" {
		t.Fatalf("remaining content type = %v, want output_text", got)
	}
}

func TestSanitizeInvalidResponsesEncryptedContentRemovesNestedPlaintextCompaction(t *testing.T) {
	input := []byte(`{
		"input":[
			{
				"role":"assistant",
				"content":[
					{
						"type":"compaction",
						"encrypted_content":"- compacted summary\n- enough plaintext to be invalid"
					},
					{"type":"output_text","text":"ok"}
				]
			}
		]
	}`)

	out, removed, err := SanitizeInvalidResponsesEncryptedContent(input)
	if err != nil {
		t.Fatalf("SanitizeInvalidResponsesEncryptedContent returned error: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	var payload map[string]any
	if err := basecommon.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	inputItems := payload["input"].([]any)
	assistant := inputItems[0].(map[string]any)
	content := assistant["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content length = %d, want 1", len(content))
	}
	text := content[0].(map[string]any)
	if got := text["type"]; got != "output_text" {
		t.Fatalf("remaining content type = %v, want output_text", got)
	}
}
