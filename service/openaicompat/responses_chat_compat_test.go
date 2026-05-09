package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func TestResponsesRequestToChatCompletionsRequest(t *testing.T) {
	instructions, _ := common.Marshal("be concise")
	input, _ := common.Marshal([]map[string]any{
		{
			"role": "user",
			"content": []map[string]any{
				{"type": "input_text", "text": "hi"},
			},
		},
	})
	toolChoice, _ := common.Marshal(map[string]any{
		"type": "function",
		"name": "lookup_weather",
	})
	tools, _ := common.Marshal([]map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "lookup_weather",
				"description": "Lookup weather by city",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
				},
			},
		},
	})
	req := &dto.OpenAIResponsesRequest{
		Model:           "mimo-v2.5-pro",
		Instructions:    instructions,
		Input:           input,
		Tools:           tools,
		ToolChoice:      toolChoice,
		MaxOutputTokens: common.GetPointer(uint(32)),
	}

	chatReq, err := ResponsesRequestToChatCompletionsRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chatReq.Model != "mimo-v2.5-pro" {
		t.Fatalf("expected model to be preserved, got %q", chatReq.Model)
	}
	if chatReq.GetMaxTokens() != 32 {
		t.Fatalf("expected max tokens 32, got %d", chatReq.GetMaxTokens())
	}
	if len(chatReq.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(chatReq.Messages))
	}
	if chatReq.Messages[0].Role == "" || chatReq.Messages[0].StringContent() != "be concise" {
		t.Fatalf("unexpected instruction message: %#v", chatReq.Messages[0])
	}
	if chatReq.Messages[1].Role != "user" || chatReq.Messages[1].StringContent() != "hi" {
		t.Fatalf("unexpected input message: %#v", chatReq.Messages[1])
	}
	if len(chatReq.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(chatReq.Tools))
	}
	if chatReq.Tools[0].Function.Name != "lookup_weather" {
		t.Fatalf("expected function name lookup_weather, got %q", chatReq.Tools[0].Function.Name)
	}
}

func TestChatCompletionsResponseToResponsesResponse(t *testing.T) {
	resp := &dto.OpenAITextResponse{
		Id:      "chatcmpl-test",
		Model:   "mimo-v2.5-pro",
		Object:  "chat.completion",
		Created: 123,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index: 0,
				Message: dto.Message{
					Role:    "assistant",
					Content: "hello",
				},
				FinishReason: "stop",
			},
		},
		Usage: dto.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	responsesResp, usage, err := ChatCompletionsResponseToResponsesResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if responsesResp.Object != "response" {
		t.Fatalf("expected response object, got %q", responsesResp.Object)
	}
	if responsesResp.ID != "resp_test" {
		t.Fatalf("expected response id resp_test, got %q", responsesResp.ID)
	}
	if len(responsesResp.Output) == 0 || responsesResp.Output[0].Type != "message" {
		t.Fatalf("unexpected output payload: %#v", responsesResp.Output)
	}
	if got := responsesResp.Output[0].Content[0].Text; got != "hello" {
		t.Fatalf("expected output text hello, got %q", got)
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 5 {
		t.Fatalf("expected normalized usage input/output 10/5, got %d/%d", usage.InputTokens, usage.OutputTokens)
	}
}
