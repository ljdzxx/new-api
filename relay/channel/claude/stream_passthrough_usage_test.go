package claude

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudePassThroughUsageLines(t *testing.T) {
	for _, tc := range []struct {
		name     string
		chunks   []string
		want     []string
		oversize bool
	}{
		{
			name:   "fragmented fields and CRLF",
			chunks: []string{"da", "ta: first\r", "\n\r", "\ndata:", " second\n\ndata: last"},
			want:   []string{"data: first", "data: second", "data: last"},
		},
		{
			name:   "CR and unterminated EOF",
			chunks: []string{"data: first\r\rdata: second\rdata:", " last"},
			want:   []string{"data: first", "data: second", "data: last"},
		},
		{
			name:     "oversized line does not hide later usage",
			chunks:   []string{"data: too", " long to parse", "data: fake\r", "\ndata: valid\n"},
			want:     []string{"data: valid"},
			oversize: true,
		},
		{
			name:     "oversized complete line and EOF",
			chunks:   []string{"data: too long to parse\ndata: valid\rdata: too long to parse"},
			want:     []string{"data: valid"},
			oversize: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parser := claudePassThroughUsageLines{maxLineSize: 12}
			var got []string
			onLine := func(line []byte) {
				if len(line) > 0 {
					got = append(got, string(line))
				}
			}
			oversized := false
			for _, chunk := range tc.chunks {
				if parser.feed([]byte(chunk), onLine) {
					oversized = true
				}
				require.LessOrEqual(t, len(parser.line), parser.maxLineSize)
			}
			parser.finish(onLine)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.oversize, oversized)
		})
	}
}
