// Claude request application adapted from QuantumNous/new-api upstream
// 7d8b91268dab69d7150f345c448b5e250163146a, using this branch's DTOs and relay metadata.
package claudethinking

import (
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// capabilityModelName recognizes dotted version aliases without changing the
// provider's wire model ID or this branch's billing/model-mapping identity.
func capabilityModelName(model string) string {
	model = strings.ToLower(model)
	if strings.HasPrefix(model, "claude-") {
		return strings.ReplaceAll(model, ".", "-")
	}
	return model
}

func recordDiagnostics(c *gin.Context, info *relaycommon.RelayInfo, diagnostics ...types.ConversionDiagnostic) {
	for i := range diagnostics {
		diagnostics[i].To = types.RelayFormatClaude
		if info != nil {
			diagnostics[i].From = info.RelayFormat
		}
	}
	info.RecordConversionDiagnostics(c, diagnostics)
}

// ApplyNative preserves native thinking controls unless a model suffix explicitly
// requests conversion. Raw-body passthrough bypasses normalization altogether.
func ApplyNative(c *gin.Context, req *dto.ClaudeRequest, info *relaycommon.RelayInfo) error {
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled ||
		info != nil && info.ChannelMeta != nil && info.ChannelSetting.PassThroughBodyEnabled {
		return nil
	}
	return AsClientError(apply(c, req, info, Intent{}, false))
}

func ApplyOpenAI(c *gin.Context, req *dto.ClaudeRequest, source *dto.GeneralOpenAIRequest, info *relaycommon.RelayInfo) error {
	intent, diagnostics, err := FromOpenAIChat(source)
	recordDiagnostics(c, info, diagnostics...)
	if err != nil {
		return AsClientError(err)
	}
	return AsClientError(apply(c, req, info, intent, true))
}

func apply(c *gin.Context, req *dto.ClaudeRequest, info *relaycommon.RelayInfo, source Intent, crossProtocol bool) error {
	if req == nil {
		return nil
	}
	settings := model_setting.GetClaudeSettings()
	baseModel, suffix, err := requestModelIntent(req.Model)
	if err != nil {
		return err
	}
	if info != nil && info.OriginModelName != req.Model && !model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) {
		_, originSuffix, parseErr := requestModelIntent(info.OriginModelName)
		if parseErr != nil {
			return parseErr
		}
		if suffix.IsEmpty() {
			suffix = originSuffix
		}
	}
	capabilityModel := baseModel
	// Native Claude-compatible providers own the meaning of their request
	// fields. As upstream does, only read effort for metadata in this case.
	if !crossProtocol && source.IsEmpty() && suffix.IsEmpty() {
		if info != nil {
			effort := req.GetEfforts()
			if effort == "" && req.Thinking != nil {
				switch {
				case req.Thinking.Type == "disabled":
					effort = string(EffortNone)
				case req.Thinking.BudgetTokens != nil:
					effort = string(EffortFromBudget(*req.Thinking.BudgetTokens))
				case req.Thinking.Type == "enabled" || req.Thinking.Type == "adaptive":
					effort = string(ClaudeDefaultEffort(req.Model))
				}
			}
			info.ReasoningEffort = effort
		}
		return nil
	}
	native, diagnostics, err := FromClaude(req)
	if err != nil {
		return err
	}
	recordDiagnostics(c, info, diagnostics...)
	explicit, diagnostics, err := MergeExplicit(native, source, req.Model)
	if err != nil {
		return err
	}
	recordDiagnostics(c, info, diagnostics...)
	if info != nil && !IsKnownClaudeModel(capabilityModel) && IsKnownClaudeModel(info.OriginModelName) {
		capabilityModel = info.OriginModelName
	}
	intent, diagnostics, err := MergeExplicitAndSuffix(explicit, suffix, req.Model)
	if err != nil {
		return err
	}
	recordDiagnostics(c, info, diagnostics...)
	knownClaudeModel := IsKnownClaudeModel(capabilityModel)
	if !knownClaudeModel && intent.Mode == ModeAdaptive {
		// Cross-protocol pivots cannot safely assume that an unknown
		// Claude-compatible model implements Anthropic's adaptive mode. Render
		// the broadly supported manual form while retaining the requested
		// strength. Native Claude requests took the passthrough path above.
		intent.Mode = ModeEnabled
		if intent.Effort == "" {
			intent.Effort = EffortHigh
		}
	}
	requestEffort, err := ParseEffort(req.GetEfforts())
	if err != nil {
		return err
	}
	if knownClaudeModel && intent.Mode == ModeDisabled && (intent.Effort == "" || intent.Effort == EffortNone) &&
		requestEffort != "" && requestEffort != EffortNone {
		// Effort works without thinking, so the request's own effort is
		// rendered for the thinking-off state rather than left unchecked.
		intent.Effort = requestEffort
	}
	if req.MaxTokens == nil && intent.HasStrength() {
		// Adapter-provided defaults may be raised to accommodate an exact
		// cross-protocol budget. Values too small for manual thinking are
		// raised again by the renderer, which reports the change.
		minimum := uint(1280)
		if configuredDefault := settings.GetDefaultMaxTokens(capabilityModel); configuredDefault > 0 {
			minimum = uint(configuredDefault)
		}
		if ClaudeUsesManualThinking(capabilityModel, intent) && *intent.BudgetTokens >= 0 {
			if *intent.BudgetTokens == math.MaxInt {
				return fmt.Errorf("thinking budget is too large to derive max_tokens")
			}
			required := uint(*intent.BudgetTokens) + 1
			const maxDerivedTokens = uint(helper.MaxTokensLimit)
			if required > maxDerivedTokens {
				return fmt.Errorf("thinking budget %d exceeds the supported conversion limit", *intent.BudgetTokens)
			}
			if minimum < required {
				minimum = required
			}
		}
		req.MaxTokens = &minimum
	}

	rendered, err := RenderClaude(capabilityModel, intent, req.MaxTokens, settings.ThinkingAdapterBudgetTokensPercentage)
	if err != nil {
		return err
	}
	recordDiagnostics(c, info, rendered.Diagnostics...)
	req.Model = baseModel
	if rendered.MaxTokens != nil {
		req.MaxTokens = rendered.MaxTokens
	}
	if rendered.Thinking != nil {
		if req.Thinking != nil && rendered.Thinking.Type == "adaptive" {
			rendered.Thinking.BlockBinding = req.Thinking.BlockBinding
		}
		req.Thinking = rendered.Thinking
	}
	outputEffort := rendered.OutputEffort
	// An effort the request still carries reaches a known model only at a
	// level it accepts, and is removed where the model takes none.
	rewriteRequestEffort := outputEffort == "" && knownClaudeModel && requestEffort != ""
	if rewriteRequestEffort {
		outputEffort = ClaudeEffort(capabilityModel, requestEffort)
	}
	if outputEffort != "" || rewriteRequestEffort {
		outputConfig := make(map[string]any)
		if len(req.OutputConfig) > 0 {
			if common.GetJsonType(req.OutputConfig) != "object" {
				return fmt.Errorf("Claude output_config must be a JSON object")
			}
			if err := common.Unmarshal(req.OutputConfig, &outputConfig); err != nil {
				return fmt.Errorf("invalid Claude output_config: %w", err)
			}
			if outputConfig == nil {
				outputConfig = make(map[string]any)
			}
		}
		if outputEffort != "" {
			outputConfig["effort"] = string(outputEffort)
		} else {
			delete(outputConfig, "effort")
		}
		if len(outputConfig) == 0 {
			req.OutputConfig = nil
		} else {
			encoded, err := common.Marshal(outputConfig)
			if err != nil {
				return fmt.Errorf("failed to marshal Claude output_config: %w", err)
			}
			req.OutputConfig = encoded
		}
	}
	if rendered.ClearSampling {
		if req.Temperature != nil || req.TopP != nil || req.TopK != nil {
			recordDiagnostics(c, info, types.ConversionDiagnostic{
				Code:     "claude_sampling_removed",
				Path:     "temperature/top_p/top_k",
				Message:  fmt.Sprintf("model %q does not accept sampling controls with the selected thinking mode", capabilityModel),
				Severity: types.ConversionDiagnosticWarning,
				To:       types.RelayFormatClaude,
			})
		}
		req.Temperature = nil
		req.TopP = nil
		req.TopK = nil
	} else if rendered.ConstrainThinkingSampling {
		removedSampling := req.Temperature != nil || req.TopK != nil || req.TopP != nil && (*req.TopP < 0.95 || *req.TopP > 1)
		req.Temperature = nil
		req.TopK = nil
		if req.TopP != nil && (*req.TopP < 0.95 || *req.TopP > 1) {
			req.TopP = nil
		}
		if removedSampling {
			recordDiagnostics(c, info, types.ConversionDiagnostic{
				Code:     "claude_sampling_constrained",
				Path:     "temperature/top_p/top_k",
				Message:  fmt.Sprintf("model %q accepts only top_p between 0.95 and 1 with manual thinking", capabilityModel),
				Severity: types.ConversionDiagnosticWarning,
				To:       types.RelayFormatClaude,
			})
		}
	}
	if info != nil && rendered.EffectiveEffort != "" {
		info.ReasoningEffort = string(rendered.EffectiveEffort)
	}
	return nil
}
