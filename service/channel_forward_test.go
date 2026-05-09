package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
)

func TestExtractChannelForwardNewMessageOpenAIChatOnlyUsesLatestUser(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "system", Content: "system keyword"},
			{Role: "user", Content: "history keyword"},
			{Role: "assistant", Content: "ok"},
			{Role: "user", Content: "latest keyword"},
		},
	}

	text := ExtractChannelForwardNewMessage(req, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions)
	if text != "latest keyword" {
		t.Fatalf("expected latest user message, got %q", text)
	}
}

func TestExtractChannelForwardNewMessageResponsesOnlyUsesLatestUser(t *testing.T) {
	input, err := common.Marshal([]map[string]any{
		{
			"role":    "user",
			"content": []map[string]any{{"type": "input_text", "text": "history keyword"}},
		},
		{
			"role":    "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "ok"}},
		},
		{
			"role":    "user",
			"content": []map[string]any{{"type": "input_text", "text": "latest keyword"}},
		},
	})
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}

	req := &dto.OpenAIResponsesRequest{Input: input}
	text := ExtractChannelForwardNewMessage(req, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses)
	if text != "latest keyword" {
		t.Fatalf("expected latest responses input, got %q", text)
	}

	instructions, err := common.Marshal("codex keyword")
	if err != nil {
		t.Fatalf("failed to marshal instructions: %v", err)
	}
	req.Instructions = instructions
	text = ExtractChannelForwardNewMessage(req, types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses)
	if text == "codex keyword" {
		t.Fatalf("expected responses instructions to be ignored")
	}
}

func TestExtractChannelForwardNewMessageIgnoresClaudeSystemPrompt(t *testing.T) {
	req := &dto.ClaudeRequest{
		System: []dto.ClaudeMediaMessage{
			{Type: dto.ContentTypeText, Text: common.GetPointer("system keyword")},
		},
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "latest keyword"},
		},
	}

	text := ExtractChannelForwardNewMessage(req, types.RelayFormatClaude, relayconstant.RelayModeUnknown)
	if text != "latest keyword" {
		t.Fatalf("expected claude latest user message, got %q", text)
	}
}

func TestExtractChannelForwardNewMessageClaudeUsesLastUserTextBlock(t *testing.T) {
	req := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type": "text",
						"text": "<system-reminder>\ncontext\n</system-reminder>",
					},
					map[string]any{
						"type": "text",
						"text": "CLAUDE.md context",
					},
					map[string]any{
						"type": "text",
						"text": "你好",
					},
				},
			},
		},
	}

	text := ExtractChannelForwardNewMessage(req, types.RelayFormatClaude, relayconstant.RelayModeUnknown)
	if text != "你好" {
		t.Fatalf("expected claude latest user text block, got %q", text)
	}
}

func TestExtractChannelForwardNewMessageClaudeSkipsTrailingReminderBlock(t *testing.T) {
	req := &dto.ClaudeRequest{
		Messages: []dto.ClaudeMessage{
			{
				Role: "user",
				Content: []any{
					map[string]any{
						"type": "text",
						"text": "你好",
					},
					map[string]any{
						"type": "text",
						"text": "<system-reminder>\ncontext\n</system-reminder>",
					},
				},
			},
		},
	}

	text := ExtractChannelForwardNewMessage(req, types.RelayFormatClaude, relayconstant.RelayModeUnknown)
	if text != "你好" {
		t.Fatalf("expected claude reminder block to be skipped, got %q", text)
	}
}

func TestIsChannelForwardMessageTooLong(t *testing.T) {
	setting := dto.ChannelSettings{ForwardMaxMessageChars: 3}
	if !IsChannelForwardMessageTooLong("你好世界", setting) {
		t.Fatalf("expected message longer than max chars to skip forwarding")
	}
	if IsChannelForwardMessageTooLong("你好", setting) {
		t.Fatalf("expected shorter message to pass length gate")
	}
}

func TestEvaluateChannelForwardPrecheckOR(t *testing.T) {
	setting := dto.ChannelSettings{
		ForwardEnabled:     true,
		ForwardMetricLogic: "or",
		ForwardMetricRules: []dto.ChannelForwardMetricRule{
			{Key: "is_test", Op: ">=", Value: 80},
			{Key: "is_check", Op: ">=", Value: 90},
		},
		ForwardModelTargets: []dto.ChannelForwardModelTarget{
			{Model: "gpt-5.4", TargetChannelID: 12},
		},
	}

	matched, matchInfo, err := EvaluateChannelForwardPrecheck(map[string]float64{
		"is_test":  20,
		"is_check": 90,
	}, setting, "gpt-5.4", "who are you")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !matched {
		t.Fatalf("expected OR metric rule to match")
	}
	if matchInfo == nil || matchInfo.TargetChannelID != 12 {
		t.Fatalf("expected target channel 12, got %+v", matchInfo)
	}
}

func TestEvaluateChannelForwardPrecheckAND(t *testing.T) {
	setting := dto.ChannelSettings{
		ForwardEnabled:     true,
		ForwardMetricLogic: "and",
		ForwardMetricRules: []dto.ChannelForwardMetricRule{
			{Key: "is_test", Op: ">=", Value: 80},
			{Key: "is_check", Op: ">=", Value: 90},
		},
		ForwardModelTargets: []dto.ChannelForwardModelTarget{
			{Model: "gpt-*", TargetChannelID: 12},
		},
	}

	matched, _, err := EvaluateChannelForwardPrecheck(map[string]float64{
		"is_test":  80,
		"is_check": 89,
	}, setting, "gpt-5.4", "test model")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if matched {
		t.Fatalf("expected AND metric rule to fail when one metric is below threshold")
	}

	matched, matchInfo, err := EvaluateChannelForwardPrecheck(map[string]float64{
		"is_test":  80,
		"is_check": 90,
	}, setting, "gpt-5.4", "test model")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !matched || matchInfo.TargetChannelID != 12 {
		t.Fatalf("expected AND metric rule to match target channel 12, got matched=%t info=%+v", matched, matchInfo)
	}
}

func TestMatchChannelForwardTargetUsesOriginalModel(t *testing.T) {
	setting := dto.ChannelSettings{
		ForwardModelTargets: []dto.ChannelForwardModelTarget{
			{Model: "mimo-*", TargetChannelID: 1},
			{Model: "gpt-*", TargetChannelID: 2},
		},
	}

	targetID, pattern, ok := MatchChannelForwardTarget("gpt-5.4", setting)
	if !ok || targetID != 2 || pattern != "gpt-*" {
		t.Fatalf("expected original model gpt-5.4 to match gpt-* target, got id=%d pattern=%q ok=%t", targetID, pattern, ok)
	}
}
