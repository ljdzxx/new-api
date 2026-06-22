package common

import "testing"

func TestDebugTraceTokenMatches(t *testing.T) {
	original := DebugTraceToken
	defer func() {
		DebugTraceToken = original
	}()

	DebugTraceToken = ""
	if !DebugTraceTokenMatches("anything") {
		t.Fatalf("empty debug trace token should match any token")
	}

	DebugTraceToken = "sk-abc123"
	for _, value := range []string{"abc123", "sk-abc123", "Bearer sk-abc123", "abc123-42"} {
		if !DebugTraceTokenMatches(value) {
			t.Fatalf("expected %q to match configured debug trace token", value)
		}
	}
	if DebugTraceTokenMatches("other") {
		t.Fatalf("unexpected token match")
	}
}
