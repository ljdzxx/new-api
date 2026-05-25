package common

import (
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
)

func TestSanitizeInvalidResponsesEncryptedContentRemovesVisibleText(t *testing.T) {
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
	reasoning := items[0].(map[string]any)
	if _, exists := reasoning["encrypted_content"]; exists {
		t.Fatalf("encrypted_content was not removed: %v", reasoning["encrypted_content"])
	}
	if _, exists := reasoning["summary"]; !exists {
		t.Fatalf("summary should be preserved")
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
