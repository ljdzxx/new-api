package service

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
)

type ChannelForwardMatchInfo struct {
	Source          string
	TextSnippet     string
	MatchedText     string
	MetricLogic     string
	Metrics         map[string]float64
	TargetModel     string
	TargetChannelID int
	SkipReason      string
}

func ShouldForwardChannelRequest(request dto.Request, relayFormat types.RelayFormat, relayMode int, channelSetting dto.ChannelSettings) (bool, error) {
	match, _, err := ShouldForwardChannelRequestWithMatchInfo(request, relayFormat, relayMode, channelSetting)
	return match, err
}

func ShouldForwardChannelRequestWithMatchInfo(request dto.Request, relayFormat types.RelayFormat, relayMode int, channelSetting dto.ChannelSettings) (bool, *ChannelForwardMatchInfo, error) {
	if request == nil || !channelSetting.ForwardEnabled {
		return false, nil, nil
	}

	text := ExtractChannelForwardNewMessage(request, relayFormat, relayMode)
	if text == "" {
		return false, nil, nil
	}
	if IsChannelForwardMessageTooLong(text, channelSetting) {
		return false, &ChannelForwardMatchInfo{
			Source:      "input",
			TextSnippet: buildForwardTextSnippet(text),
			SkipReason:  "message_too_long",
		}, nil
	}
	return false, &ChannelForwardMatchInfo{
		Source:      "input",
		TextSnippet: buildForwardTextSnippet(text),
		SkipReason:  "precheck_required",
	}, nil
}

func ExtractChannelForwardNewMessage(request dto.Request, relayFormat types.RelayFormat, relayMode int) string {
	texts := extractChannelForwardTexts(request, relayFormat, relayMode)
	if len(texts) == 0 {
		return ""
	}
	return strings.TrimSpace(texts[0].Text)
}

func IsChannelForwardMessageTooLong(text string, channelSetting dto.ChannelSettings) bool {
	limit := channelSetting.ForwardMaxMessageChars
	return limit > 0 && len([]rune(strings.TrimSpace(text))) > limit
}

func MatchChannelForwardTarget(originalModel string, channelSetting dto.ChannelSettings) (int, string, bool) {
	originalModel = strings.TrimSpace(originalModel)
	if originalModel == "" {
		return 0, "", false
	}
	for _, target := range channelSetting.ForwardModelTargets {
		pattern := strings.TrimSpace(target.Model)
		if pattern == "" || target.TargetChannelID <= 0 {
			continue
		}
		if wildcardModelMatch(pattern, originalModel) {
			return target.TargetChannelID, pattern, true
		}
	}
	return 0, "", false
}

func EvaluateChannelForwardPrecheck(metrics map[string]float64, channelSetting dto.ChannelSettings, originalModel string, message string) (bool, *ChannelForwardMatchInfo, error) {
	if !channelSetting.ForwardEnabled {
		return false, nil, nil
	}
	targetChannelID, targetModel, ok := MatchChannelForwardTarget(originalModel, channelSetting)
	if !ok {
		return false, &ChannelForwardMatchInfo{
			Source:      "precheck",
			TextSnippet: buildForwardTextSnippet(message),
			Metrics:     metrics,
			SkipReason:  "target_not_matched",
		}, nil
	}
	logic := strings.ToLower(strings.TrimSpace(channelSetting.ForwardMetricLogic))
	if logic == "" {
		logic = "or"
	}
	if logic != "and" && logic != "or" {
		return false, nil, fmt.Errorf("unsupported forward metric logic %q", channelSetting.ForwardMetricLogic)
	}
	if len(channelSetting.ForwardMetricRules) == 0 {
		return false, nil, fmt.Errorf("forward metric rules are empty")
	}

	matched := logic == "and"
	matchedRules := make([]string, 0, len(channelSetting.ForwardMetricRules))
	for _, rule := range channelSetting.ForwardMetricRules {
		value, ok := metrics[strings.TrimSpace(rule.Key)]
		if !ok {
			value = 0
		}
		ruleMatched, err := compareChannelForwardMetric(value, strings.TrimSpace(rule.Op), rule.Value)
		if err != nil {
			return false, nil, err
		}
		if ruleMatched {
			matchedRules = append(matchedRules, fmt.Sprintf("%s %s %g", rule.Key, rule.Op, rule.Value))
		}
		if logic == "and" && !ruleMatched {
			matched = false
			break
		}
		if logic == "or" && ruleMatched {
			matched = true
			break
		}
	}

	matchInfo := &ChannelForwardMatchInfo{
		Source:          "precheck",
		TextSnippet:     buildForwardTextSnippet(message),
		MatchedText:     strings.Join(matchedRules, "; "),
		MetricLogic:     strings.ToUpper(logic),
		Metrics:         metrics,
		TargetModel:     targetModel,
		TargetChannelID: targetChannelID,
	}
	if !matched {
		matchInfo.SkipReason = "metrics_not_matched"
	}
	return matched, matchInfo, nil
}

func compareChannelForwardMetric(value float64, op string, expected float64) (bool, error) {
	switch op {
	case ">=":
		return value >= expected, nil
	case ">":
		return value > expected, nil
	case "<=":
		return value <= expected, nil
	case "<":
		return value < expected, nil
	case "==":
		return value == expected, nil
	case "!=":
		return value != expected, nil
	default:
		return false, fmt.Errorf("unsupported forward metric operator %q", op)
	}
}

func wildcardModelMatch(pattern string, model string) bool {
	patternRunes := []rune(pattern)
	modelRunes := []rune(model)
	p, m := 0, 0
	star := -1
	match := 0
	for m < len(modelRunes) {
		if p < len(patternRunes) && (patternRunes[p] == '?' || patternRunes[p] == modelRunes[m]) {
			p++
			m++
			continue
		}
		if p < len(patternRunes) && patternRunes[p] == '*' {
			star = p
			match = m
			p++
			continue
		}
		if star != -1 {
			p = star + 1
			match++
			m = match
			continue
		}
		return false
	}
	for p < len(patternRunes) && patternRunes[p] == '*' {
		p++
	}
	return p == len(patternRunes)
}

type channelForwardText struct {
	Source string
	Text   string
}

func extractChannelForwardTexts(request dto.Request, relayFormat types.RelayFormat, relayMode int) []channelForwardText {
	texts := make([]channelForwardText, 0, 4)
	appendText := func(source string, text string) {
		text = strings.TrimSpace(text)
		if text != "" {
			texts = append(texts, channelForwardText{
				Source: source,
				Text:   text,
			})
		}
	}

	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		switch relayMode {
		case relayconstant.RelayModeChatCompletions:
			appendText("input", extractLastOpenAIUserText(req.Messages))
		case relayconstant.RelayModeCompletions:
			appendText("input", textFromAny(req.Prompt))
		case relayconstant.RelayModeModerations:
			appendText("input", textFromAny(req.Input))
		case relayconstant.RelayModeEdits:
			appendText("input", textFromAny(req.Input))
		}
	case *dto.ClaudeRequest:
		appendText("input", extractLastClaudeUserText(req.Messages))
	case *dto.OpenAIResponsesRequest:
		appendText("input", extractResponsesNewMessageText(req.Input))
	case *dto.OpenAIResponsesCompactionRequest:
		appendText("input", extractResponsesNewMessageText(req.Input))
	case *dto.GeminiChatRequest:
		if len(req.Requests) > 0 {
			for _, nested := range req.Requests {
				appendText("input", extractLastGeminiUserText(&nested))
			}
			break
		}
		appendText("input", extractLastGeminiUserText(req))
	default:
		_ = relayFormat
	}

	return texts
}

func buildForwardTextSnippet(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) <= 80 {
		return text
	}
	return text[:32] + "..." + text[len(text)-32:]
}

func extractOpenAISystemText(messages []dto.Message) string {
	texts := make([]string, 0, len(messages))
	for _, message := range messages {
		if message.Role != "system" {
			continue
		}
		if text := strings.TrimSpace(message.StringContent()); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func extractLastOpenAIUserText(messages []dto.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		return strings.TrimSpace(messages[i].StringContent())
	}
	return ""
}

func extractClaudeSystemText(request *dto.ClaudeRequest) string {
	if request == nil || request.System == nil {
		return ""
	}
	if request.IsStringSystem() {
		return strings.TrimSpace(request.GetStringSystem())
	}
	return extractClaudeMediaText(request.ParseSystem())
}

func extractClaudeMediaText(items []dto.ClaudeMediaMessage) string {
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(item.GetText()); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func extractLastClaudeUserText(messages []dto.ClaudeMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		return extractLastClaudeUserTextBlock(messages[i])
	}
	return ""
}

func extractLastClaudeUserTextBlock(message dto.ClaudeMessage) string {
	switch content := message.Content.(type) {
	case string:
		text := strings.TrimSpace(content)
		if isClaudeContextReminderBlock(text) {
			return ""
		}
		return text
	case []dto.ClaudeMediaMessage:
		for i := len(content) - 1; i >= 0; i-- {
			if strings.TrimSpace(content[i].Type) != dto.ContentTypeText {
				continue
			}
			text := strings.TrimSpace(content[i].GetText())
			if text == "" || isClaudeContextReminderBlock(text) {
				continue
			}
			return text
		}
	case []any:
		for i := len(content) - 1; i >= 0; i-- {
			item, ok := content[i].(map[string]any)
			if !ok || strings.TrimSpace(common.Interface2String(item["type"])) != dto.ContentTypeText {
				continue
			}
			text := strings.TrimSpace(common.Interface2String(item["text"]))
			if text == "" || isClaudeContextReminderBlock(text) {
				continue
			}
			return text
		}
	}
	return strings.TrimSpace(message.GetStringContent())
}

func isClaudeContextReminderBlock(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "<system-reminder>") && strings.HasSuffix(text, "</system-reminder>")
}

func extractGeminiSystemText(request *dto.GeminiChatRequest) string {
	if request == nil || request.SystemInstructions == nil {
		return ""
	}
	return extractGeminiPartsText(request.SystemInstructions.Parts)
}

func extractLastGeminiUserText(request *dto.GeminiChatRequest) string {
	if request == nil {
		return ""
	}
	for i := len(request.Contents) - 1; i >= 0; i-- {
		role := strings.TrimSpace(request.Contents[i].Role)
		if role != "" && role != "user" {
			continue
		}
		return extractGeminiPartsText(request.Contents[i].Parts)
	}
	return ""
}

func extractGeminiPartsText(parts []dto.GeminiPart) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part.Text); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func extractResponsesNewMessageText(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	switch common.GetJsonType(input) {
	case "string":
		return rawJSONToText(input)
	case "object":
		var item map[string]any
		if err := common.Unmarshal(input, &item); err != nil {
			return ""
		}
		if strings.EqualFold(common.Interface2String(item["role"]), "user") {
			return strings.TrimSpace(textFromAny(item["content"]))
		}
		return strings.TrimSpace(extractResponsesMediaItemsText([]any{item}))
	case "array":
		var items []any
		if err := common.Unmarshal(input, &items); err != nil {
			return ""
		}
		for i := len(items) - 1; i >= 0; i-- {
			itemMap, ok := items[i].(map[string]any)
			if !ok {
				continue
			}
			role := strings.TrimSpace(common.Interface2String(itemMap["role"]))
			if role == "user" {
				return strings.TrimSpace(textFromAny(itemMap["content"]))
			}
		}
		return strings.TrimSpace(extractResponsesMediaItemsText(items))
	default:
		return ""
	}
}

func extractResponsesMediaItemsText(items []any) string {
	texts := make([]string, 0, len(items))
	for _, itemAny := range items {
		item, ok := itemAny.(map[string]any)
		if !ok {
			continue
		}
		typeValue := strings.TrimSpace(common.Interface2String(item["type"]))
		switch typeValue {
		case "input_text":
			if text := strings.TrimSpace(common.Interface2String(item["text"])); text != "" {
				texts = append(texts, text)
			}
		case "message":
			if text := strings.TrimSpace(textFromAny(item["content"])); text != "" {
				texts = append(texts, text)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func rawJSONToText(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if common.GetJsonType(data) == "string" {
		var text string
		if err := common.Unmarshal(data, &text); err == nil {
			return strings.TrimSpace(text)
		}
	}
	return strings.TrimSpace(string(data))
}

func textFromAny(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []string:
		texts := make([]string, 0, len(v))
		for _, item := range v {
			if text := strings.TrimSpace(item); text != "" {
				texts = append(texts, text)
			}
		}
		return strings.Join(texts, "\n")
	case []any:
		texts := make([]string, 0, len(v))
		for _, item := range v {
			if text := textFromAny(item); text != "" {
				texts = append(texts, text)
			}
		}
		return strings.Join(texts, "\n")
	case map[string]any:
		texts := make([]string, 0, 2)
		if text := strings.TrimSpace(common.Interface2String(v["text"])); text != "" {
			texts = append(texts, text)
		}
		if content, ok := v["content"]; ok {
			if text := textFromAny(content); text != "" {
				texts = append(texts, text)
			}
		}
		return strings.Join(texts, "\n")
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}
