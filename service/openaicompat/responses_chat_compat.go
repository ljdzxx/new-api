package openaicompat

import (
	"encoding/json"
	"errors"
	"fmt"
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
	if err := req.ValidateStatelessConversion(); err != nil {
		return nil, err
	}
	if req.PreviousResponseID != "" || len(req.Conversation) > 0 && string(req.Conversation) != "null" || len(req.ContextManagement) > 0 && string(req.ContextManagement) != "null" || len(req.Prompt) > 0 && string(req.Prompt) != "null" {
		return nil, errors.New("Chat Completions requires inline input history; Responses state references are unsupported")
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

	tools, choice, mapping, err := convertResponsesChatTools(req.Tools, req.ToolChoice)
	if err != nil {
		return nil, err
	}
	out.Tools = tools
	out.ToolChoice = choice
	out.ResponsesToolMapping = mapping

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

	inputMessages, err := responsesInputToChatMessagesWithTools(req.Input, mapping)
	if err != nil {
		return nil, err
	}
	messages = append(messages, inputMessages...)
	out.Messages = messages

	return out, nil
}

func responsesInputToChatMessages(raw json.RawMessage) ([]dto.Message, error) {
	return responsesInputToChatMessagesWithTools(raw, nil)
}
func responsesInputToChatMessagesWithTools(raw json.RawMessage, mapping map[string]dto.ResponsesToolIdentity) ([]dto.Message, error) {
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
	if err := common.UnmarshalUseNumber(raw, &items); err != nil {
		return nil, err
	}

	messages := make([]dto.Message, 0, len(items))
	for _, item := range items {
		itemType := strings.TrimSpace(common.Interface2String(item["type"]))
		switch itemType {
		case "function_call_output", "custom_tool_call_output":
			msg := dto.Message{
				Role:       "tool",
				ToolCallId: strings.TrimSpace(common.Interface2String(item["call_id"])),
			}
			output, err := responsesChatOutput(item["output"])
			if err != nil {
				return nil, err
			}
			if msg.ToolCallId == "" {
				return nil, errors.New("tool output requires call_id")
			}
			msg.SetStringContent(output)
			messages = append(messages, msg)
			continue
		case "function_call", "custom_tool_call":
			name, err := responsesChatName(common.Interface2String(item["namespace"]), common.Interface2String(item["name"]))
			if err != nil {
				return nil, err
			}
			if common.Interface2String(item["call_id"]) == "" {
				return nil, errors.New("tool call requires call_id")
			}
			arguments, ok := item["arguments"].(string)
			if itemType == "custom_tool_call" {
				input, valid := item["input"].(string)
				if !valid {
					return nil, errors.New("custom_tool_call.input must be a string")
				}
				encoded, err := common.Marshal(map[string]string{"input": input})
				if err != nil {
					return nil, err
				}
				arguments = string(encoded)
				ok = true
			}
			if !ok {
				return nil, errors.New("function_call.arguments must be a string")
			}
			if mapping != nil {
				namespace := strings.TrimSpace(common.Interface2String(item["namespace"]))
				if namespace == "functions" {
					namespace = ""
				}
				if prior, exists := mapping[name]; exists && (prior.Custom != (itemType == "custom_tool_call") || prior.Name != strings.TrimSpace(common.Interface2String(item["name"])) || prior.Namespace != namespace) {
					return nil, errors.New("tool history conflicts with definition")
				}
			}
			msg := dto.Message{Role: "assistant"}
			msg.SetToolCalls([]dto.ToolCallRequest{
				{
					ID:   strings.TrimSpace(common.Interface2String(item["call_id"])),
					Type: "function",
					Function: dto.FunctionRequest{
						Name:      name,
						Arguments: arguments,
					},
				},
			})
			messages = append(messages, msg)
			continue
		}

		if itemType != "" && itemType != "message" {
			return nil, fmt.Errorf("Responses item %q cannot be represented in Chat Completions", itemType)
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
	if usage.BillingUsage == nil {
		usage.BillingUsage = dto.NewOpenAIChatBillingUsage(&resp.Usage)
	}
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
		textTokens := usage.PromptTokens - usage.PromptTokensDetails.CachedTokens - usage.PromptTokensDetails.CacheWriteTokens
		if textTokens < 0 {
			textTokens = 0
		}
		usage.InputTokensDetails = &dto.InputTokenDetails{
			CachedTokens:     usage.PromptTokensDetails.CachedTokens,
			CacheWriteTokens: usage.PromptTokensDetails.CacheWriteTokens,
			ImageTokens:      usage.PromptTokensDetails.ImageTokens,
			AudioTokens:      usage.PromptTokensDetails.AudioTokens,
			TextTokens:       textTokens,
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
				ID:        responsesFunctionCallItemID(toolCall.ID),
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

func responsesFunctionCallItemID(callID string) string {
	callID = strings.TrimSpace(callID)
	if strings.HasPrefix(callID, "fc") {
		return callID
	}
	suffix := strings.TrimPrefix(callID, "call_")
	if suffix == "" {
		suffix = common.GetUUID()
	}
	return "fc_" + suffix
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
