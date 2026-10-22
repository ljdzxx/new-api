package dto

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// GeminiEstimateTokenMeta includes agent history and schemas without changing
// the existing metadata/estimator used by other model families.
func GeminiEstimateTokenMeta(request Request) *types.TokenCountMeta {
	switch r := request.(type) {
	case *GeminiChatRequest:
		return r.GetTokenCountMeta()
	case *GeneralOpenAIRequest:
		copy := *r
		copy.Tools = nil
		meta := copy.GetTokenCountMeta()
		texts := []string{meta.CombineText}
		for _, message := range r.Messages {
			for _, call := range message.ParseToolCalls() {
				texts = append(texts, call.Function.Name, call.Function.Arguments)
			}
		}
		if len(r.Tools) > 0 {
			texts = append(texts, geminiTokenJSON(r.Tools))
		}
		meta.CombineText = strings.Join(texts, "\n")
		return meta
	case *OpenAIResponsesRequest:
		copy := *r
		copy.Input, copy.Tools = nil, nil
		meta := copy.GetTokenCountMeta()
		texts := []string{meta.CombineText}
		var input any
		if common.Unmarshal(r.Input, &input) == nil {
			collectGeminiResponsesTokens(input, &texts, &meta.Files)
		}
		if len(r.Tools) > 0 {
			var tools any
			if common.Unmarshal(r.Tools, &tools) == nil {
				texts = append(texts, geminiTokenJSON(tools))
			}
		}
		meta.CombineText = strings.Join(texts, "\n")
		return meta
	default:
		return nil
	}
}

func geminiTokenJSON(value any) string {
	data, _ := common.Marshal(value)
	return string(data)
}

func (r *GeminiChatRequest) geminiTokenCountMeta() *types.TokenCountMeta {
	meta := &types.TokenCountMeta{}
	if r.GenerationConfig.MaxOutputTokens != nil {
		meta.MaxTokens = common.QuotaFromFloat(float64(*r.GenerationConfig.MaxOutputTokens))
	}
	var texts []string
	contents := r.Contents
	if r.SystemInstructions != nil {
		contents = append([]GeminiChatContent{*r.SystemInstructions}, contents...)
	}
	for _, content := range contents {
		for _, part := range content.Parts {
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
			if part.FunctionCall != nil {
				texts = append(texts, part.FunctionCall.FunctionName, geminiTokenJSON(part.FunctionCall.Arguments))
			}
			media := []GeminiPart{part}
			if result := part.FunctionResponse; result != nil {
				texts = append(texts, result.Name)
				keys := make([]string, 0, len(result.Response))
				for key := range result.Response {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					if text, ok := result.Response[key].(string); ok {
						texts = append(texts, text)
					} else {
						texts = append(texts, geminiTokenJSON(result.Response[key]))
					}
				}
				var parts []GeminiPart
				if common.Unmarshal(result.Parts, &parts) == nil {
					media = append(media, parts...)
				}
			}
			for _, part := range media {
				source := part.InlineData.ToFileSource()
				mime := ""
				if part.InlineData != nil {
					mime = part.InlineData.MimeType
				}
				if source == nil && part.FileData != nil && part.FileData.FileUri != "" {
					mime = part.FileData.MimeType
					source = types.NewFileSourceFromData(part.FileData.FileUri, mime)
				}
				if source == nil {
					continue
				}
				detail := string(r.GenerationConfig.MediaResolution)
				var resolution struct {
					Level string `json:"level"`
				}
				if common.Unmarshal(part.MediaResolution, &resolution) == nil && resolution.Level != "" {
					detail = resolution.Level
				}
				meta.Files = append(meta.Files, &types.FileMeta{FileType: geminiTokenFileType(mime), Source: source, Detail: detail})
			}
		}
	}
	if len(r.Tools) > 0 {
		var tools any
		if common.Unmarshal(r.Tools, &tools) == nil {
			texts = append(texts, geminiTokenJSON(tools))
		}
	}
	meta.CombineText = strings.Join(texts, "\n")
	return meta
}

func geminiTokenFileType(mime string) types.FileType {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return types.FileTypeImage
	case strings.HasPrefix(mime, "audio/"):
		return types.FileTypeAudio
	case strings.HasPrefix(mime, "video/"):
		return types.FileTypeVideo
	default:
		return types.FileTypeFile
	}
}

// Only visit protocol content containers. IDs, encrypted reasoning, metadata
// and media payloads must never become ordinary prompt text.
func collectGeminiResponsesTokens(value any, texts *[]string, files *[]*types.FileMeta) {
	switch v := value.(type) {
	case string:
		*texts = append(*texts, v)
	case []any:
		for _, item := range v {
			collectGeminiResponsesTokens(item, texts, files)
		}
	case map[string]any:
		kind, _ := v["type"].(string)
		if _, media := responsesMediaInputTypes[kind]; media {
			if file := geminiResponsesTokenFile(v, kind); file != nil {
				*files = append(*files, file)
			}
			return
		}
		switch kind {
		case "function_call", "custom_tool_call":
			if name, ok := v["name"].(string); ok {
				*texts = append(*texts, name)
			}
			field := "arguments"
			if kind == "custom_tool_call" {
				field = "input"
			}
			if text, ok := v[field].(string); ok {
				*texts = append(*texts, text)
			}
		case "function_call_output", "custom_tool_call_output":
			output := v["output"]
			// Arbitrary JSON tool results are model input too. Content-part
			// arrays are walked instead so their base64 is never tokenized.
			if parts, ok := output.([]any); ok {
				isParts := false
				for _, part := range parts {
					if obj, ok := part.(map[string]any); ok {
						t, _ := obj["type"].(string)
						_, media := responsesMediaInputTypes[t]
						isParts = isParts || media || t == "input_text" || t == "output_text"
					}
				}
				if isParts {
					collectGeminiResponsesTokens(parts, texts, files)
					return
				}
			}
			if text, ok := output.(string); ok {
				*texts = append(*texts, text)
			} else {
				*texts = append(*texts, geminiTokenJSON(output))
			}
		case "additional_tools":
			*texts = append(*texts, geminiTokenJSON(v["tools"]))
		default:
			for _, field := range responsesTextFields {
				collectGeminiResponsesTokens(v[field], texts, files)
			}
		}
	}
}

func geminiResponsesTokenFile(part map[string]any, kind string) *types.FileMeta {
	mime, _ := part["mime_type"].(string)
	data := ""
	for _, key := range []string{"image_url", "file", "file_data", "file_url", "video_url", "input_audio", "data", "url"} {
		switch value := part[key].(type) {
		case string:
			data = value
		case map[string]any:
			if mime == "" {
				mime, _ = value["mime_type"].(string)
			}
			for _, nested := range []string{"url", "file_data", "file_url", "data"} {
				if s, ok := value[nested].(string); ok && s != "" {
					data = s
					break
				}
			}
		}
		if data != "" {
			break
		}
	}
	if data == "" {
		return nil
	}
	fileType := types.FileTypeFile
	switch kind {
	case "input_image":
		fileType = types.FileTypeImage
	case "input_audio":
		fileType = types.FileTypeAudio
	case "input_video":
		fileType = types.FileTypeVideo
	}
	detail, _ := part["detail"].(string)
	return &types.FileMeta{FileType: fileType, Source: types.NewFileSourceFromData(data, mime), Detail: detail}
}
