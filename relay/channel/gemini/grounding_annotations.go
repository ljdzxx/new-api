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

// geminiGroundingStreamState 保存同一候选结果的已发送文本及引用去重状态。
type geminiGroundingStreamState struct {
	rendered strings.Builder
	seen     map[string]struct{}
	chunks   []geminiGroundingChunk
	parts    map[int]string
}

// annotations 汇总前后帧的来源，再把累计文本中的引用位置映射到输出。
func (s *geminiGroundingStreamState) annotations(metadata *dto.GeminiGroundingMetadata) []any {
	if s == nil || metadata == nil {
		return nil
	}
	var incoming []geminiGroundingChunk
	if len(metadata.GroundingChunks) > 0 && common.Unmarshal(metadata.GroundingChunks, &incoming) == nil {
		// Gemini 的 chunk 索引跨帧累计，按到达顺序追加。
		s.chunks = append(s.chunks, incoming...)
	}
	if len(s.chunks) == 0 || len(metadata.GroundingSupports) == 0 {
		return nil
	}
	chunks, err := common.Marshal(s.chunks)
	if err != nil {
		return nil
	}
	merged := *metadata
	merged.GroundingChunks = chunks
	content := dto.GeminiChatContent{}
	if len(s.parts) > 0 {
		maxPart := 0
		for index := range s.parts {
			maxPart = max(maxPart, index)
		}
		content.Parts = make([]dto.GeminiPart, maxPart+1)
		for index, text := range s.parts {
			content.Parts[index].Text = text
		}
	} else {
		content.Parts = []dto.GeminiPart{{Text: s.rendered.String()}}
	}
	return geminiGroundingAnnotations(&merged, content, s.rendered.String(), s.seen)
}

// observeCandidate 累计源文本 part，以便将延迟到达的 grounding offsets 映射回流式文本。
func (s *geminiGroundingStreamState) observeCandidate(candidate dto.GeminiChatCandidate) {
	if s == nil {
		return
	}
	if s.parts == nil {
		s.parts = make(map[int]string)
	}
	for index, part := range candidate.Content.Parts {
		if part.Text != "" && !part.Thought {
			s.parts[index] += part.Text
		}
	}
}

// geminiGroundingAnnotations 将 Gemini 字节位置转换成 OpenAI 的 Unicode 字符位置。
// 流式调用传入累计文本与 seen；非流式传入 nil 以按当前响应去重。
func geminiGroundingAnnotations(metadata *dto.GeminiGroundingMetadata, content dto.GeminiChatContent, rendered string, seen map[string]struct{}) []any {
	if metadata == nil || len(metadata.GroundingChunks) == 0 || len(metadata.GroundingSupports) == 0 {
		return nil
	}
	var chunks []geminiGroundingChunk
	var supports []geminiGroundingSupport
	if common.Unmarshal(metadata.GroundingChunks, &chunks) != nil || common.Unmarshal(metadata.GroundingSupports, &supports) != nil {
		return nil
	}
	if seen == nil {
		seen = make(map[string]struct{})
	}
	parts := make([]struct {
		text  string
		start int
	}, len(content.Parts))
	textParts := 0
	solePart := -1
	cursor := 0
	for i, part := range content.Parts {
		parts[i].start = -1
		if part.Thought || part.Text == "" || part.Text == "\n" {
			continue
		}
		start := strings.Index(rendered[cursor:], part.Text)
		if start < 0 {
			continue
		}
		parts[i].text = part.Text
		parts[i].start = cursor + start
		cursor = parts[i].start + len(part.Text)
		textParts++
		solePart = i
	}
	// 流式 metadata 往往在文本帧之后到达，使用累计文本作单一 part。
	if textParts == 0 && len(content.Parts) == 0 && rendered != "" {
		parts = append(parts, struct {
			text  string
			start int
		}{text: rendered, start: 0})
		textParts, solePart = 1, 0
	}
	annotations := make([]any, 0)
	for _, support := range supports {
		partIndex := solePart
		if support.Segment.PartIndex != nil {
			partIndex = *support.Segment.PartIndex
		} else if textParts != 1 {
			continue
		}
		if partIndex < 0 || partIndex >= len(parts) || parts[partIndex].start < 0 {
			continue
		}
		part := parts[partIndex]
		startByte, endByte := support.Segment.StartIndex, support.Segment.EndIndex
		if startByte < 0 || endByte <= startByte || endByte > len(part.text) {
			continue
		}
		if !utf8.ValidString(part.text[:startByte]) || !utf8.ValidString(part.text[:endByte]) {
			continue
		}
		if support.Segment.Text != "" && part.text[startByte:endByte] != support.Segment.Text {
			continue
		}
		start := utf8.RuneCountInString(rendered[:part.start+startByte])
		end := utf8.RuneCountInString(rendered[:part.start+endByte])
		for _, index := range support.GroundingChunkIndices {
			if index < 0 || index >= len(chunks) {
				continue
			}
			source := chunks[index].Web
			if source == nil {
				source = chunks[index].RetrievedContext
			}
			if source == nil || source.URI == "" {
				continue
			}
			key := fmt.Sprintf("%d:%d:%s", start, end, source.URI)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			annotations = append(annotations, map[string]any{
				"type":         "url_citation",
				"url_citation": map[string]any{"start_index": start, "end_index": end, "url": source.URI, "title": source.Title},
			})
		}
	}
	return annotations
}
