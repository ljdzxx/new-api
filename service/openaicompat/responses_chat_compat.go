package openaicompat

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/samber/lo"
)

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	if req.Model == "" {
		return nil, errors.New("model is required")
	}

	out := &dto.GeneralOpenAIRequest{
		Model:       req.Model,
		Stream:      req.Stream,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		User:        req.User,
		Store:       req.Store,
		Metadata:    req.Metadata,
	}
	if req.MaxOutputTokens != nil {
		out.MaxTokens = lo.ToPtr(*req.MaxOutputTokens)
	}
	if req.Reasoning != nil {
		out.ReasoningEffort = req.Reasoning.Effort
	}

	if len(req.Tools) > 0 {
		var tools []map[string]any
		if err := common.Unmarshal(req.Tools, &tools); err == nil {
			out.Tools = make([]dto.ToolCallRequest, 0, len(tools))
			for _, tool := range tools {
				toolType := strings.TrimSpace(common.Interface2String(tool["type"]))
				switch toolType {
				case "function":
					name := strings.TrimSpace(common.Interface2String(tool["name"]))
					description := strings.TrimSpace(common.Interface2String(tool["description"]))
					parameters := tool["parameters"]
					if fn, ok := tool["function"].(map[string]any); ok {
						if name == "" {
							name = strings.TrimSpace(common.Interface2String(fn["name"]))
						}
						if description == "" {
							description = strings.TrimSpace(common.Interface2String(fn["description"]))
						}
						if parameters == nil {
							parameters = fn["parameters"]
						}
					}
					out.Tools = append(out.Tools, dto.ToolCallRequest{
						Type: "function",
						Function: dto.FunctionRequest{
							Name:        name,
							Description: description,
							Parameters:  parameters,
						},
					})
				default:
					encoded, _ := common.Marshal(tool)
					out.Tools = append(out.Tools, dto.ToolCallRequest{
						Type:   toolType,
						Custom: encoded,
					})
				}
			}
		}
	}

	if len(req.ToolChoice) > 0 {
		switch common.GetJsonType(req.ToolChoice) {
		case "string":
			var v string
			if err := common.Unmarshal(req.ToolChoice, &v); err == nil {
				out.ToolChoice = v
			}
		case "object":
			var v map[string]any
			if err := common.Unmarshal(req.ToolChoice, &v); err == nil {
				if strings.TrimSpace(common.Interface2String(v["type"])) == "function" {
					if name := strings.TrimSpace(common.Interface2String(v["name"])); name != "" {
						out.ToolChoice = map[string]any{
							"type": "function",
							"function": map[string]any{
								"name": name,
							},
						}
					} else {
						out.ToolChoice = v
					}
				} else {
					out.ToolChoice = v
				}
			}
		}
	}

	var messages []dto.Message
	if len(req.Instructions) > 0 {
		msg := dto.Message{Role: out.GetSystemRoleName()}
		switch common.GetJsonType(req.Instructions) {
		case "string":
			var instruction string
			if err := common.Unmarshal(req.Instructions, &instruction); err == nil {
				msg.SetStringContent(instruction)
			} else {
				msg.SetStringContent(string(req.Instructions))
			}
		default:
			msg.SetStringContent(string(req.Instructions))
		}
		messages = append(messages, msg)
	}

	inputMessages, err := responsesInputToChatMessages(req.Input)
	if err != nil {
		return nil, err
	}
	messages = append(messages, inputMessages...)
	out.Messages = messages

	return out, nil
}

func responsesInputToChatMessages(raw json.RawMessage) ([]dto.Message, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if common.GetJsonType(raw) == "string" {
		var text string
		if err := common.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		msg := dto.Message{Role: "user"}
		msg.SetStringContent(text)
		return []dto.Message{msg}, nil
	}
	if common.GetJsonType(raw) != "array" {
		return nil, nil
	}

	var items []map[string]any
	if err := common.Unmarshal(raw, &items); err != nil {
		return nil, err
	}

	messages := make([]dto.Message, 0, len(items))
	for _, item := range items {
		itemType := strings.TrimSpace(common.Interface2String(item["type"]))
		switch itemType {
		case "function_call_output":
			msg := dto.Message{
				Role:       "tool",
				ToolCallId: strings.TrimSpace(common.Interface2String(item["call_id"])),
			}
			msg.SetStringContent(common.Interface2String(item["output"]))
			messages = append(messages, msg)
			continue
		case "function_call":
			msg := dto.Message{Role: "assistant"}
			msg.SetToolCalls([]dto.ToolCallRequest{
				{
					ID:   strings.TrimSpace(common.Interface2String(item["call_id"])),
					Type: "function",
					Function: dto.FunctionRequest{
						Name:      strings.TrimSpace(common.Interface2String(item["name"])),
						Arguments: common.Interface2String(item["arguments"]),
					},
				},
			})
			messages = append(messages, msg)
			continue
		}

		role := strings.TrimSpace(common.Interface2String(item["role"]))
		if role == "" {
			role = "user"
		}
		msg := dto.Message{Role: role}
		content := item["content"]
		switch typed := content.(type) {
		case string:
			msg.SetStringContent(typed)
		case []any:
			media := make([]dto.MediaContent, 0, len(typed))
			var plainText strings.Builder
			for _, partAny := range typed {
				part, ok := partAny.(map[string]any)
				if !ok {
					continue
				}
				partType := strings.TrimSpace(common.Interface2String(part["type"]))
				switch partType {
				case "input_text", "output_text", "text":
					text := common.Interface2String(part["text"])
					if text == "" {
						continue
					}
					media = append(media, dto.MediaContent{Type: dto.ContentTypeText, Text: text})
					plainText.WriteString(text)
				case "input_image":
					imageURL := part["image_url"]
					switch v := imageURL.(type) {
					case string:
						media = append(media, dto.MediaContent{
							Type:     dto.ContentTypeImageURL,
							ImageUrl: &dto.MessageImageUrl{Url: v},
						})
					case map[string]any:
						media = append(media, dto.MediaContent{
							Type: dto.ContentTypeImageURL,
							ImageUrl: &dto.MessageImageUrl{
								Url:    common.Interface2String(v["url"]),
								Detail: common.Interface2String(v["detail"]),
							},
						})
					}
				case "input_audio":
					if inputAudio, ok := part["input_audio"]; ok {
						media = append(media, dto.MediaContent{
							Type:       dto.ContentTypeInputAudio,
							InputAudio: inputAudio,
						})
					}
				}
			}
			if len(media) == 1 && media[0].Type == dto.ContentTypeText {
				msg.SetStringContent(plainText.String())
			} else if len(media) > 0 {
				msg.SetMediaContent(media)
			}
		default:
			msg.SetStringContent(common.Interface2String(content))
		}
		if msg.Content == nil && len(msg.ParseToolCalls()) == 0 {
			continue
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func ChatCompletionsResponseToResponsesResponse(resp *dto.OpenAITextResponse) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}

	usage := resp.Usage
	if usage.InputTokens == 0 && usage.PromptTokens != 0 {
		usage.InputTokens = usage.PromptTokens
	}
	if usage.OutputTokens == 0 && usage.CompletionTokens != 0 {
		usage.OutputTokens = usage.CompletionTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if usage.InputTokensDetails == nil {
		usage.InputTokensDetails = &dto.InputTokenDetails{
			CachedTokens: usage.PromptTokensDetails.CachedTokens,
			ImageTokens:  usage.PromptTokensDetails.ImageTokens,
			AudioTokens:  usage.PromptTokensDetails.AudioTokens,
			TextTokens:   usage.PromptTokens - usage.PromptTokensDetails.CachedTokens,
		}
	}

	createdAt := normalizeResponseCreatedAt(resp.Created)
	output := make([]dto.ResponsesOutput, 0, 2)
	messageID := "msg_" + strings.TrimPrefix(resp.Id, "chatcmpl-")

	var textBuilder strings.Builder
	for _, choice := range resp.Choices {
		if text := choice.Message.StringContent(); text != "" {
			textBuilder.WriteString(text)
		} else if reasoning := choice.Message.GetReasoningContent(); reasoning != "" {
			textBuilder.WriteString(reasoning)
		}

		for _, toolCall := range choice.Message.ParseToolCalls() {
			arguments, _ := common.Marshal(toolCall.Function.Arguments)
			output = append(output, dto.ResponsesOutput{
				Type:      "function_call",
				ID:        toolCall.ID,
				CallId:    toolCall.ID,
				Status:    "completed",
				Name:      toolCall.Function.Name,
				Arguments: json.RawMessage(arguments),
			})
		}
	}

	output = append([]dto.ResponsesOutput{{
		Type:   "message",
		ID:     messageID,
		Status: "completed",
		Role:   "assistant",
		Content: []dto.ResponsesOutputContent{{
			Type:        "output_text",
			Text:        textBuilder.String(),
			Annotations: []interface{}{},
		}},
	}}, output...)

	statusRaw, _ := common.Marshal("completed")
	out := &dto.OpenAIResponsesResponse{
		ID:        "resp_" + strings.TrimPrefix(resp.Id, "chatcmpl-"),
		Object:    "response",
		CreatedAt: createdAt,
		Status:    statusRaw,
		Model:     resp.Model,
		Output:    output,
		Usage:     &usage,
	}
	return out, &usage, nil
}

func normalizeResponseCreatedAt(created any) int {
	switch v := created.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return int(i)
		}
	}
	return 0
}
