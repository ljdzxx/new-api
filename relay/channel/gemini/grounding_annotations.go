package gemini

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

type geminiGroundingSource struct {
	URI   string `json:"uri"`
	Title string `json:"title"`
}

type geminiGroundingChunk struct {
	Web              *geminiGroundingSource `json:"web,omitempty"`
	RetrievedContext *geminiGroundingSource `json:"retrievedContext,omitempty"`
}

type geminiGroundingSupport struct {
	Segment struct {
		PartIndex  *int   `json:"partIndex,omitempty"`
		StartIndex int    `json:"startIndex"`
		EndIndex   int    `json:"endIndex"`
		Text       string `json:"text,omitempty"`
	} `json:"segment"`
	GroundingChunkIndices []int `json:"groundingChunkIndices"`
}

type renderedGeminiPart struct {
	text      string
	startByte int
}
type geminiTextSpan struct{ start, end, rendered int }
type streamedGeminiPart struct {
	text  strings.Builder
	spans []geminiTextSpan
}
type geminiGroundingStreamState struct {
	rendered strings.Builder
	seen     map[string]struct{}
	chunks   []geminiGroundingChunk
	parts    map[int]*streamedGeminiPart
}

func (s *geminiGroundingStreamState) recordPart(index int, text string, renderedStart int) {
	if text == "" {
		return
	}
	if s.parts == nil {
		s.parts = make(map[int]*streamedGeminiPart)
	}
	part := s.parts[index]
	if part == nil {
		part = &streamedGeminiPart{}
		s.parts[index] = part
	}
	start := part.text.Len()
	part.text.WriteString(text)
	if renderedStart >= 0 {
		part.spans = append(part.spans, geminiTextSpan{start, start + len(text), renderedStart})
	}
}

func (s *geminiGroundingStreamState) appendContent(content dto.GeminiChatContent, parts []renderedGeminiPart, rendered string) {
	for i, part := range content.Parts {
		if part.Thought || part.Text == "" {
			continue
		}
		start := -1
		if i < len(parts) && parts[i].startByte >= 0 {
			start = s.rendered.Len() + parts[i].startByte
		}
		s.recordPart(i, part.Text, start)
	}
	s.rendered.WriteString(rendered)
}

func (s *geminiGroundingStreamState) annotations(metadata *dto.GeminiGroundingMetadata) []any {
	if s == nil || metadata == nil {
		return nil
	}
	var incoming []geminiGroundingChunk
	if len(metadata.GroundingChunks) > 0 && common.Unmarshal(metadata.GroundingChunks, &incoming) == nil {
		s.chunks = append(s.chunks, incoming...)
	}
	if s.seen == nil {
		s.seen = make(map[string]struct{})
	}
	return groundingAnnotationsForParts(metadata.GroundingSupports, s.chunks, s.parts, s.rendered.String(), s.seen)
}

// geminiGroundingAnnotations 将 Gemini 字节位置转换成 OpenAI 的 Unicode 字符位置。
// 流式调用传入累计文本与 seen；非流式传入 nil 以按当前响应去重。
func geminiGroundingAnnotations(metadata *dto.GeminiGroundingMetadata, content dto.GeminiChatContent, rendered string, seen map[string]struct{}, locations ...[]renderedGeminiPart) []any {
	if metadata == nil {
		return nil
	}
	var chunks []geminiGroundingChunk
	if common.Unmarshal(metadata.GroundingChunks, &chunks) != nil {
		return nil
	}
	if seen == nil {
		seen = make(map[string]struct{})
	}
	state := &geminiGroundingStreamState{}
	cursor := 0
	for i, part := range content.Parts {
		if part.Thought || part.Text == "" {
			continue
		}
		start := cursor
		if len(locations) > 0 {
			if i >= len(locations[0]) {
				continue
			}
			start = locations[0][i].startByte
		}
		state.recordPart(i, part.Text, start)
		cursor += len(part.Text)
	}
	return groundingAnnotationsForParts(metadata.GroundingSupports, chunks, state.parts, rendered, seen)
}

func groundingAnnotationsForParts(raw []byte, chunks []geminiGroundingChunk, parts map[int]*streamedGeminiPart, rendered string, seen map[string]struct{}) []any {
	var supports []geminiGroundingSupport
	if len(chunks) == 0 || common.Unmarshal(raw, &supports) != nil {
		return nil
	}
	solePart := -1
	for index, part := range parts {
		if len(part.spans) == 0 {
			continue
		}
		if solePart != -1 {
			solePart = -2
			break
		}
		solePart = index
	}
	annotations := make([]any, 0)
	for _, support := range supports {
		index := solePart
		if support.Segment.PartIndex != nil {
			index = *support.Segment.PartIndex
		}
		part := parts[index]
		if part == nil {
			continue
		}
		text := part.text.String()
		startByte, endByte := support.Segment.StartIndex, support.Segment.EndIndex
		if startByte < 0 || endByte <= startByte || endByte > len(text) {
			continue
		}
		if !utf8.ValidString(text[:startByte]) || !utf8.ValidString(text[:endByte]) {
			continue
		}
		if support.Segment.Text != "" && text[startByte:endByte] != support.Segment.Text {
			continue
		}
		start, end := -1, -1
		for _, span := range part.spans {
			if startByte >= span.start && startByte < span.end {
				start = span.rendered + startByte - span.start
			}
			if endByte > span.start && endByte <= span.end {
				end = span.rendered + endByte - span.start
			}
		}
		if start < 0 || end <= start || end > len(rendered) || rendered[start:end] != text[startByte:endByte] {
			continue
		}
		start, end = utf8.RuneCountInString(rendered[:start]), utf8.RuneCountInString(rendered[:end])
		for _, chunkIndex := range support.GroundingChunkIndices {
			if chunkIndex < 0 || chunkIndex >= len(chunks) {
				continue
			}
			source := chunks[chunkIndex].Web
			if source == nil {
				source = chunks[chunkIndex].RetrievedContext
			}
			if source == nil || source.URI == "" {
				continue
			}
			key := fmt.Sprintf("%d:%d:%s", start, end, source.URI)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			annotations = append(annotations, map[string]any{"type": "url_citation", "url_citation": map[string]any{"start_index": start, "end_index": end, "url": source.URI, "title": source.Title}})
		}
	}
	return annotations
}
