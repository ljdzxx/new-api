package common

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDebugTraceBodyTextSanitizesLongJSONStrings(t *testing.T) {
	body := []byte(`{"model":"your-model","messages":[{"role":"user","content":"` + strings.Repeat("你", 20) + `"}],"boundary":"` + strings.Repeat("a", 50) + `"}`)

	encoding, text := debugTraceBodyText(http.Header{"Content-Type": {"application/json"}}, body)
	if encoding != "json" {
		t.Fatalf("expected json encoding, got %q", encoding)
	}
	if strings.Contains(text, strings.Repeat("你", 20)) {
		t.Fatal("long JSON string was not sanitized")
	}
	if !strings.Contains(text, `"content":"Size(60)"`) {
		t.Fatalf("expected byte size marker, got %s", text)
	}
	if !strings.Contains(text, `"boundary":"`+strings.Repeat("a", 50)+`"`) {
		t.Fatalf("50-byte boundary value should be preserved, got %s", text)
	}
}

func TestDebugTraceBodyTextPreservesJSONNumbers(t *testing.T) {
	body := []byte(`{"large":18446744073686646784,"enabled":false,"nothing":null}`)
	_, text := debugTraceBodyText(nil, body)
	if !strings.Contains(text, `18446744073686646784`) {
		t.Fatalf("JSON number changed during sanitization: %s", text)
	}
}

func TestDebugTraceBodyTextFallsBackForNonJSON(t *testing.T) {
	encoding, text := debugTraceBodyText(nil, []byte("plain text"))
	if encoding != "text" || text != "plain text" {
		t.Fatalf("unexpected fallback: encoding=%q text=%q", encoding, text)
	}
}

func TestDebugTraceStreamReaderTracksSizeWithoutBuffering(t *testing.T) {
	reader := &debugTraceReadCloser{
		rc:       io.NopCloser(strings.NewReader(strings.Repeat("x", 128))),
		sizeOnly: true,
	}
	data := make([]byte, 128)
	n, err := reader.Read(data)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if n != 128 || reader.bodySize != 128 {
		t.Fatalf("unexpected sizes: read=%d tracked=%d", n, reader.bodySize)
	}
	if reader.buf.Len() != 0 {
		t.Fatalf("stream body should not be buffered, buffered=%d", reader.buf.Len())
	}
}
