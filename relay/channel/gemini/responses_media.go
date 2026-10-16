package gemini

import (
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

func responsesCallID(item map[string]any) string {
	if id := responsesString(item["call_id"]); id != "" {
		return id
	}
	return responsesString(item["id"])
}

// As in upstream's GeminiResponseMap, JSON scalar strings remain text.
func responsesGeminiResponseMap(value any) map[string]any {
	switch v := value.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return v
	case string:
		var object map[string]any
		if common.UnmarshalJsonStr(v, &object) == nil {
			return object
		}
		var array []any
		if common.UnmarshalJsonStr(v, &array) == nil {
			return map[string]any{"result": array}
		}
		return map[string]any{"content": v}
	case []any:
		return map[string]any{"result": v}
	default:
		return map[string]any{"content": v}
	}
}

func responsesPartDataAndMime(part map[string]any, keys ...string) (string, string) {
	mime := strings.TrimSpace(responsesString(part["mime_type"]))
	for _, key := range keys {
		switch v := part[key].(type) {
		case string:
			if v != "" {
				return v, mime
			}
		case map[string]any:
			if mime == "" {
				mime = strings.TrimSpace(responsesString(v["mime_type"]))
			}
			for _, nested := range []string{"url", "file_data", "file_url", "data"} {
				if data := strings.TrimSpace(responsesString(v[nested])); data != "" {
					return data, mime
				}
			}
		}
	}
	return "", mime
}

func responsesContentFileSource(part map[string]any) types.FileSource {
	var data, mime string
	switch responsesString(part["type"]) {
	case "input_image":
		data, mime = responsesPartDataAndMime(part, "image_url", "url")
	case "input_file":
		data, mime = responsesPartDataAndMime(part, "file", "file_data", "file_url", "url")
	case "input_video":
		data, mime = responsesPartDataAndMime(part, "video_url", "url")
	case "input_audio":
		data, mime = responsesPartDataAndMime(part, "input_audio", "data", "url")
		if mime == "" {
			if audio, ok := part["input_audio"].(map[string]any); ok {
				if format := responsesString(audio["format"]); format != "" {
					if format == "mp3" {
						format = "mpeg"
					}
					mime = "audio/" + format
				}
			}
		}
	}
	if data == "" {
		return nil
	}
	return types.NewFileSourceFromData(data, mime)
}

func responsesContentParts(content any) ([]map[string]any, error) {
	switch value := content.(type) {
	case nil:
		return nil, nil
	case string:
		return []map[string]any{{"type": "input_text", "text": value}}, nil
	case []any:
		parts := make([]map[string]any, 0, len(value))
		for _, entry := range value {
			switch part := entry.(type) {
			case map[string]any:
				parts = append(parts, part)
			case string:
				parts = append(parts, map[string]any{"type": "input_text", "text": part})
			default:
				raw, err := common.Marshal(part)
				if err != nil {
					return nil, err
				}
				parts = append(parts, map[string]any{"type": "input_text", "text": string(raw)})
			}
		}
		return parts, nil
	default:
		raw, err := common.Marshal(value)
		if err != nil {
			return nil, err
		}
		return []map[string]any{{"type": "input_text", "text": string(raw)}}, nil
	}
}

func isResponsesTextPart(kind string) bool {
	return kind == "input_text" || kind == "output_text" || kind == "text"
}
func isResponsesMediaPart(kind string) bool {
	return kind == "input_image" || kind == "input_file" || kind == "input_audio" || kind == "input_video"
}

func (p *responsesRequestPlan) loadMedia(part map[string]any) (*dto.GeminiPart, error) {
	source := responsesContentFileSource(part)
	if source == nil {
		return nil, fmt.Errorf("content requires inline data or a URL")
	}
	data, mime, err := service.GetBase64Data(p.c, source, "formatting Responses media for Gemini")
	if err != nil {
		return nil, fmt.Errorf("load Responses media: %w", err)
	}
	if _, ok := geminiSupportedMimeTypes[strings.ToLower(mime)]; !ok {
		return nil, fmt.Errorf("unsupported Gemini MIME type %q", mime)
	}
	return &dto.GeminiPart{InlineData: &dto.GeminiInlineData{Data: data, MimeType: mime}}, nil
}

func (p *responsesRequestPlan) content(content any) ([]dto.GeminiPart, error) {
	input, err := responsesContentParts(content)
	if err != nil {
		return nil, err
	}
	var parts []dto.GeminiPart
	for _, part := range input {
		kind := responsesString(part["type"])
		switch {
		case isResponsesTextPart(kind):
			text, ok := part["text"].(string)
			if !ok {
				return nil, fmt.Errorf("%s.text must be a string", kind)
			}
			if text != "" {
				parts = append(parts, dto.GeminiPart{Text: text})
			}
		case isResponsesMediaPart(kind):
			if responsesContentFileSource(part) == nil {
				p.loss("input.content", "unsupported_media_type", "provider file IDs or media without data cannot be converted; the part was omitted", true)
				continue
			}
			media, err := p.loadMedia(part)
			if err != nil {
				return nil, err
			}
			parts = append(parts, *media)
		default:
			p.loss("input.content", "unsupported_content_part", fmt.Sprintf("content part %q was omitted", kind), true)
		}
	}
	return parts, nil
}

func (p *responsesRequestPlan) toolOutput(value any, mediaInParts bool) (map[string]any, []dto.GeminiPart, error) {
	array, ok := value.([]any)
	if !ok || len(array) == 0 {
		return responsesGeminiResponseMap(value), nil, nil
	}
	for _, v := range array {
		part, ok := v.(map[string]any)
		if !ok || (!isResponsesTextPart(responsesString(part["type"])) && !isResponsesMediaPart(responsesString(part["type"]))) {
			return responsesGeminiResponseMap(value), nil, nil
		}
	}
	var texts, labels []string
	var media []dto.GeminiPart
	for _, v := range array {
		part := v.(map[string]any)
		kind := responsesString(part["type"])
		if isResponsesTextPart(kind) {
			if text := responsesString(part["text"]); text != "" {
				texts = append(texts, text)
			}
			continue
		}
		label := "[" + strings.TrimPrefix(kind, "input_") + "]"
		if !slices.Contains(labels, label) {
			labels = append(labels, label)
		}
		var converted *dto.GeminiPart
		var err error
		if mediaInParts && (kind == "input_audio" || kind == "input_video") {
			err = fmt.Errorf("unsupported function response media")
		} else {
			converted, err = p.loadMedia(part)
		}
		if err == nil && mediaInParts && (strings.HasPrefix(converted.InlineData.MimeType, "audio/") || strings.HasPrefix(converted.InlineData.MimeType, "video/")) {
			err = fmt.Errorf("unsupported function response MIME type")
		}
		if err != nil {
			// Diagnostics name the kind, never inline payloads or signed URLs.
			p.loss("input.output", "unsupported_media_type", fmt.Sprintf("tool output %s could not be loaded or carried by Gemini; the part was omitted", kind), true)
			continue
		}
		media = append(media, *converted)
	}
	if len(texts) > 0 {
		return responsesGeminiResponseMap(strings.Join(texts, "\n")), media, nil
	}
	if len(media) > 0 {
		return map[string]any{}, media, nil
	}
	return responsesGeminiResponseMap(strings.Join(labels, " ")), nil, nil
}
