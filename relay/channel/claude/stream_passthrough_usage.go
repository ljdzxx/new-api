package claude

import "bytes"

// Only the billing parser buffers incomplete lines; forwarded bytes never wait
// here. Bound its memory even if an upstream never sends a line ending.
type claudePassThroughUsageLines struct {
	line        []byte
	discardLine bool
	maxLineSize int
}

func (b *claudePassThroughUsageLines) feed(data []byte, onLine func([]byte)) (oversized bool) {
	for len(data) > 0 {
		end := bytes.IndexAny(data, "\r\n")
		part := data
		if end >= 0 {
			part = data[:end]
		}
		if !b.discardLine {
			if len(part) > b.maxLineSize-len(b.line) {
				b.line = nil
				b.discardLine = true
				oversized = true
			} else if end >= 0 && len(b.line) == 0 {
				onLine(part)
			} else {
				b.line = append(b.line, part...)
			}
		}
		if end < 0 {
			break
		}
		b.finish(onLine)
		b.discardLine = false
		data = data[end+1:]
	}
	return oversized
}

func (b *claudePassThroughUsageLines) finish(onLine func([]byte)) {
	if !b.discardLine && len(b.line) > 0 {
		onLine(b.line)
	}
	b.line = b.line[:0]
}
