package claude

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// Keep protocol-specific content until the final Claude block is constructed.
// In particular, tool results must not stringify image/document payloads.
type claudeRequestConversion struct {
	c    *gin.Context
	info *relaycommon.RelayInfo
	err  error
}

func (p *claudeRequestConversion) loss(path, message string, semantic bool) {
	severity := types.ConversionDiagnosticWarning
	if semantic {
		severity = types.ConversionDiagnosticError
	}
	d := types.ConversionDiagnostic{Code: "unsupported_claude_conversion", Path: path, Message: message, Severity: severity, From: p.info.RelayFormat, To: types.RelayFormatClaude}
	p.info.RecordConversionDiagnostics(p.c, []types.ConversionDiagnostic{d})
	policy := p.info.ChannelOtherSettings.ToolLossPolicy
	if policy == "" {
		policy = types.ConversionLossPolicyAllow
	}
	if p.err == nil {
		p.err = types.RejectConversionLoss(policy, []types.ConversionDiagnostic{d})
	}
}

func newClaudeConversion(c *gin.Context, info *relaycommon.RelayInfo) (*claudeRequestConversion, error) {
	if info == nil || info.ChannelMeta == nil {
		return nil, fmt.Errorf("missing relay metadata")
	}
	switch info.ChannelOtherSettings.ToolLossPolicy {
	case "", types.ConversionLossPolicyAllow, types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict:
	default:
		return nil, fmt.Errorf("invalid tool_loss_policy %q", info.ChannelOtherSettings.ToolLossPolicy)
	}
	return &claudeRequestConversion{c: c, info: info}, nil
}

func rawPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && strings.TrimSpace(string(raw)) != "null"
}

func validateClaudeMaxTokens(tokens *uint) error {
	if tokens != nil && *tokens > uint(helper.MaxTokensLimit) {
		return fmt.Errorf("max output tokens exceeds %d", helper.MaxTokensLimit)
	}
	return nil
}

func finishClaudeRequest(req *dto.ClaudeRequest) error {
	if req.MaxTokens == nil || *req.MaxTokens == 0 {
		value := model_setting.GetClaudeSettings().GetDefaultMaxTokens(req.Model)
		if value <= 0 {
			return fmt.Errorf("max_tokens is required for Claude")
		}
		req.MaxTokens = common.GetPointer(uint(value))
	}
	if err := validateClaudeMaxTokens(req.MaxTokens); err != nil {
		return err
	}
	if len(req.Messages) == 0 || req.Messages[0].Role != "user" {
		req.Messages = append([]dto.ClaudeMessage{{Role: "user", Content: "..."}}, req.Messages...)
	}
	return nil
}

func appendClaudeBlocks(req *dto.ClaudeRequest, role string, blocks ...dto.ClaudeMediaMessage) {
	if len(blocks) == 0 {
		return
	}
	if role == "system" || role == "developer" {
		current, _ := req.System.([]dto.ClaudeMediaMessage)
		req.System = append(current, blocks...)
		return
	}
	if len(req.Messages) > 0 && req.Messages[len(req.Messages)-1].Role == role {
		last := &req.Messages[len(req.Messages)-1]
		current, _ := last.Content.([]dto.ClaudeMediaMessage)
		last.Content = append(current, blocks...)
		return
	}
	req.Messages = append(req.Messages, dto.ClaudeMessage{Role: role, Content: blocks})
}

func (p *claudeRequestConversion) media(source types.FileSource, path string) (*dto.ClaudeMediaMessage, error) {
	if source == nil {
		return nil, fmt.Errorf("%s requires inline data or a resolvable URL", path)
	}
	data, mime, err := service.GetBase64Data(p.c, source, "formatting input for Claude")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	mime = strings.ToLower(strings.Split(mime, ";")[0])
	if mime == "image/jpg" {
		mime = "image/jpeg"
	}
	if strings.HasPrefix(mime, "text/") {
		text, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, err
		}
		return &dto.ClaudeMediaMessage{Type: "text", Text: common.GetPointer(string(text))}, nil
	}
	kind := "image"
	switch mime {
	case "application/pdf":
		kind = "document"
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		p.loss(path, "Claude cannot carry media type "+mime+"; the part was omitted", true)
		return nil, nil
	}
	return &dto.ClaudeMediaMessage{Type: kind, Source: &dto.ClaudeMessageSource{Type: "base64", MediaType: mime, Data: data}}, nil
}

func claudeFunctionSchema(value any) (json.RawMessage, error) {
	raw, err := common.Marshal(value)
	if err != nil {
		return nil, err
	}
	if !rawPresent(raw) {
		return json.RawMessage(`{"type":"object","properties":{}}`), nil
	}
	if common.GetJsonType(raw) != "object" {
		return nil, fmt.Errorf("Claude input_schema must be an object")
	}
	return raw, nil
}
