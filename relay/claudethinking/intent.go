// Claude reasoning logic adapted from QuantumNous/new-api upstream 7d8b91268dab69d7150f345c448b5e250163146a.
// Kept independent of relaykit to support this branch's existing relay architecture.

package claudethinking

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

type Effort string

const (
	EffortNone    Effort = "none"
	EffortMinimal Effort = "minimal"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
	EffortMax     Effort = "max"
)

type Mode string

type Source string

// ClientError marks invalid user-supplied reasoning controls so host handlers
// can return a 4xx without classifying unrelated adapter failures as client
// errors.
type ClientError struct {
	err error
}

func (e *ClientError) Error() string { return e.err.Error() }
func (e *ClientError) Unwrap() error { return e.err }

func AsClientError(err error) error {
	if err == nil {
		return nil
	}
	var clientErr *ClientError
	if errors.As(err, &clientErr) {
		return err
	}
	return &ClientError{err: err}
}

func IsClientError(err error) bool {
	var clientErr *ClientError
	return errors.As(err, &clientErr)
}

const (
	ModeUnset    Mode = ""
	ModeEnabled  Mode = "enabled"
	ModeAdaptive Mode = "adaptive"
	ModeDisabled Mode = "disabled"
)

const (
	SourceExplicit Source = "explicit"
	SourceNative   Source = "native"
	SourceSuffix   Source = "suffix"
	SourcePivot    Source = "pivot"
)

var (
	ErrEffortConflict      = errors.New("reasoning settings conflict")
	ErrUnsupportedEffort   = errors.New("unsupported reasoning effort")
	ErrThinkingNotDisabled = errors.New("thinking cannot be disabled")
)

// Intent is the protocol-independent part of a request's reasoning controls.
// Summary visibility is intentionally independent from reasoning strength.
type Intent struct {
	Mode            Mode
	Effort          Effort
	BudgetTokens    *int
	IncludeThoughts *bool
	Source          Source
	BudgetSource    Source
}

func (i Intent) HasStrength() bool {
	return i.Mode != ModeUnset || i.Effort != "" || i.BudgetTokens != nil
}

func (i Intent) IsEmpty() bool {
	return !i.HasStrength() && i.IncludeThoughts == nil
}

func ParseEffort(value string) (Effort, error) {
	effort := Effort(strings.ToLower(strings.TrimSpace(value)))
	if effort == "" {
		return "", nil
	}
	switch effort {
	case EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return effort, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedEffort, value)
	}
}

func reasoningDiagnostic(code string, message string) types.ConversionDiagnostic {
	return types.ConversionDiagnostic{
		Code:     code,
		Path:     "reasoning",
		Message:  message,
		Severity: types.ConversionDiagnosticWarning,
	}
}

func normalizeIntent(intent Intent) (Intent, []types.ConversionDiagnostic, error) {
	effort, err := ParseEffort(string(intent.Effort))
	if err != nil {
		return Intent{}, nil, err
	}
	intent.Effort = effort

	switch intent.Mode {
	case ModeUnset, ModeEnabled, ModeAdaptive, ModeDisabled:
	default:
		return Intent{}, nil, fmt.Errorf("unsupported reasoning mode %q", intent.Mode)
	}

	var diagnostics []types.ConversionDiagnostic
	if intent.BudgetTokens != nil {
		budget := *intent.BudgetTokens
		if budget < -1 {
			diagnostics = append(diagnostics, reasoningDiagnostic(
				"reasoning_budget_adjusted",
				fmt.Sprintf("thinking budget %d is below -1; using the dynamic budget -1", budget),
			))
			budget = -1
			intent.BudgetTokens = &budget
		}
		if budget == 0 {
			if intent.Mode == ModeEnabled || intent.Mode == ModeAdaptive || (intent.Effort != "" && intent.Effort != EffortNone) {
				diagnostics = append(diagnostics, reasoningDiagnostic(
					"explicit_fields_conflict",
					"a zero thinking budget disables thinking; the enabling mode or effort was ignored",
				))
			}
			intent.Mode = ModeDisabled
			intent.Effort = EffortNone
		} else if intent.Mode == ModeDisabled || intent.Effort == EffortNone {
			diagnostics = append(diagnostics, reasoningDiagnostic(
				"explicit_fields_conflict",
				fmt.Sprintf("a non-zero thinking budget %d enables thinking; the disabling mode or effort was ignored", budget),
			))
			intent.Mode = ModeEnabled
			intent.Effort = ""
		} else if intent.Mode == ModeUnset {
			intent.Mode = ModeEnabled
		}
	}

	if intent.Effort == EffortNone {
		if intent.Mode == ModeEnabled || intent.Mode == ModeAdaptive {
			diagnostics = append(diagnostics, reasoningDiagnostic(
				"explicit_fields_conflict",
				"effort none disables thinking; the enabling mode was ignored",
			))
		}
		intent.Mode = ModeDisabled
	}

	return intent, diagnostics, nil
}

func MergeExplicitAndSuffix(explicit Intent, suffix Intent, model string) (Intent, []types.ConversionDiagnostic, error) {
	explicit, diagnostics, err := normalizeIntent(explicit)
	if err != nil {
		return Intent{}, nil, err
	}
	suffix, suffixDiagnostics, err := normalizeIntent(suffix)
	if err != nil {
		return Intent{}, nil, err
	}
	diagnostics = append(diagnostics, suffixDiagnostics...)

	if !explicit.HasStrength() {
		if explicit.IncludeThoughts != nil {
			suffix.IncludeThoughts = explicit.IncludeThoughts
		}
		return suffix, diagnostics, nil
	}
	if !suffix.HasStrength() {
		if explicit.IncludeThoughts == nil {
			explicit.IncludeThoughts = suffix.IncludeThoughts
		}
		return explicit, diagnostics, nil
	}

	merged := suffix
	if explicit.IncludeThoughts != nil {
		merged.IncludeThoughts = explicit.IncludeThoughts
	}
	explicitDisabled := explicit.Mode == ModeDisabled || explicit.Effort == EffortNone
	suffixDisabled := suffix.Mode == ModeDisabled || suffix.Effort == EffortNone
	if explicitDisabled != suffixDisabled {
		diagnostics = append(diagnostics, reasoningDiagnostic(
			"suffix_overrode_request",
			fmt.Sprintf("model %q: the model name and request fields disagree about whether thinking is enabled; the model name wins", model),
		))
		return merged, diagnostics, nil
	}
	if suffixDisabled {
		return merged, diagnostics, nil
	}

	if merged.Mode == ModeEnabled && explicit.Mode == ModeAdaptive {
		merged.Mode = ModeAdaptive
	}
	if suffix.Effort == "" && suffix.BudgetTokens == nil {
		// A bare enable says nothing about strength; request fields supply it.
		merged.Effort = explicit.Effort
		merged.BudgetTokens = explicit.BudgetTokens
		merged.BudgetSource = explicit.BudgetSource
		return merged, diagnostics, nil
	}
	if explicit.Effort != "" && explicit.Effort != suffix.Effort {
		diagnostics = append(diagnostics, reasoningDiagnostic(
			"suffix_overrode_request",
			fmt.Sprintf("model %q: request effort %q was replaced by the reasoning strength in the model name", model, explicit.Effort),
		))
	}
	if explicit.BudgetTokens != nil && (suffix.BudgetTokens == nil || *explicit.BudgetTokens != *suffix.BudgetTokens) {
		diagnostics = append(diagnostics, reasoningDiagnostic(
			"suffix_overrode_request",
			fmt.Sprintf("model %q: request thinking budget %d was replaced by the reasoning strength in the model name", model, *explicit.BudgetTokens),
		))
	}
	return merged, diagnostics, nil
}

func MergeExplicit(primary Intent, secondary Intent, model string) (Intent, []types.ConversionDiagnostic, error) {
	primary, diagnostics, err := normalizeIntent(primary)
	if err != nil {
		return Intent{}, nil, err
	}
	secondary, secondaryDiagnostics, err := normalizeIntent(secondary)
	if err != nil {
		return Intent{}, nil, err
	}
	diagnostics = append(diagnostics, secondaryDiagnostics...)

	if primary.IsEmpty() {
		return secondary, diagnostics, nil
	}
	if secondary.IsEmpty() {
		return primary, diagnostics, nil
	}

	primaryDisabled := primary.Mode == ModeDisabled || primary.Effort == EffortNone
	secondaryDisabled := secondary.Mode == ModeDisabled || secondary.Effort == EffortNone
	if primary.HasStrength() && secondary.HasStrength() && primaryDisabled != secondaryDisabled {
		diagnostics = append(diagnostics, reasoningDiagnostic(
			"explicit_fields_conflict",
			fmt.Sprintf("model %q: reasoning fields disagree about whether thinking is enabled; keeping the more specific field", model),
		))
		if primary.IncludeThoughts == nil {
			primary.IncludeThoughts = secondary.IncludeThoughts
		}
		return primary, diagnostics, nil
	}
	if primary.Effort != "" && secondary.Effort != "" && primary.Effort != secondary.Effort {
		diagnostics = append(diagnostics, reasoningDiagnostic(
			"explicit_fields_conflict",
			fmt.Sprintf("model %q: reasoning efforts %q and %q differ; keeping %q", model, primary.Effort, secondary.Effort, primary.Effort),
		))
	}
	if primary.BudgetTokens != nil && secondary.BudgetTokens != nil && *primary.BudgetTokens != *secondary.BudgetTokens {
		diagnostics = append(diagnostics, reasoningDiagnostic(
			"explicit_fields_conflict",
			fmt.Sprintf("model %q: thinking budgets %d and %d differ; keeping %d", model, *primary.BudgetTokens, *secondary.BudgetTokens, *primary.BudgetTokens),
		))
	}

	merged := secondary
	if primary.Mode != ModeUnset {
		merged.Mode = primary.Mode
	}
	if primary.Effort != "" {
		merged.Effort = primary.Effort
	}
	if primary.BudgetTokens != nil {
		merged.BudgetTokens = primary.BudgetTokens
		merged.BudgetSource = primary.BudgetSource
	}
	if primary.IncludeThoughts != nil {
		merged.IncludeThoughts = primary.IncludeThoughts
	}
	merged, mergedDiagnostics, err := normalizeIntent(merged)
	if err != nil {
		return Intent{}, nil, err
	}
	return merged, append(diagnostics, mergedDiagnostics...), nil
}

type openRouterReasoning struct {
	Enabled   *bool   `json:"enabled,omitempty"`
	Effort    *string `json:"effort,omitempty"`
	MaxTokens *int    `json:"max_tokens,omitempty"`
	Exclude   *bool   `json:"exclude,omitempty"`
}

func FromOpenAIChat(req *dto.GeneralOpenAIRequest) (Intent, []types.ConversionDiagnostic, error) {
	if req == nil {
		return Intent{}, nil, nil
	}

	var intent Intent
	intent.Source = SourceExplicit
	if req.ReasoningEffort != "" {
		effort, err := ParseEffort(req.ReasoningEffort)
		if err != nil {
			return Intent{}, nil, err
		}
		intent.Effort = effort
		if effort == EffortNone {
			intent.Mode = ModeDisabled
		} else {
			intent.Mode = ModeEnabled
		}
	}

	var diagnostics []types.ConversionDiagnostic
	if len(req.Reasoning) > 0 {
		var raw openRouterReasoning
		if err := common.Unmarshal(req.Reasoning, &raw); err != nil {
			return Intent{}, nil, fmt.Errorf("invalid reasoning config: %w", err)
		}
		nested := Intent{BudgetTokens: raw.MaxTokens, Source: SourceExplicit, BudgetSource: SourceExplicit}
		if raw.Enabled != nil {
			if *raw.Enabled {
				nested.Mode = ModeEnabled
			} else {
				nested.Mode = ModeDisabled
				nested.Effort = EffortNone
			}
		}
		if raw.Effort != nil {
			effort, err := ParseEffort(*raw.Effort)
			if err != nil {
				return Intent{}, nil, err
			}
			nested.Effort = effort
			if effort == EffortNone {
				nested.Mode = ModeDisabled
			} else if nested.Mode == ModeUnset {
				nested.Mode = ModeEnabled
			}
		}
		if raw.Exclude != nil {
			include := !*raw.Exclude
			nested.IncludeThoughts = &include
		}
		merged, mergeDiagnostics, err := MergeExplicit(intent, nested, req.Model)
		if err != nil {
			return Intent{}, nil, err
		}
		intent = merged
		diagnostics = append(diagnostics, mergeDiagnostics...)
	}

	normalized, normalizeDiagnostics, err := normalizeIntent(intent)
	return normalized, append(diagnostics, normalizeDiagnostics...), err
}

func FromClaude(req *dto.ClaudeRequest) (Intent, []types.ConversionDiagnostic, error) {
	if req == nil {
		return Intent{}, nil, nil
	}
	var intent Intent
	var diagnostics []types.ConversionDiagnostic
	intent.Source = SourceNative
	if req.Thinking != nil {
		switch req.Thinking.Type {
		case "", "enabled":
			intent.Mode = ModeEnabled
		case "adaptive":
			intent.Mode = ModeAdaptive
		case "disabled":
			intent.Mode = ModeDisabled
			intent.Effort = EffortNone
		default:
			diagnostics = append(diagnostics, types.ConversionDiagnostic{
				Code:     "claude_thinking_type_coerced",
				Path:     "thinking",
				Message:  fmt.Sprintf("unsupported Claude thinking type %q was treated as enabled", req.Thinking.Type),
				Severity: types.ConversionDiagnosticWarning,
			})
			intent.Mode = ModeEnabled
		}
		intent.BudgetTokens = req.Thinking.BudgetTokens
		if req.Thinking.BudgetTokens != nil {
			intent.BudgetSource = SourceNative
		}
		switch req.Thinking.Display {
		case "summarized":
			include := true
			intent.IncludeThoughts = &include
		case "omitted":
			include := false
			intent.IncludeThoughts = &include
		}
	}
	if len(req.OutputConfig) > 0 {
		var output dto.OutputConfigForEffort
		if err := common.Unmarshal(req.OutputConfig, &output); err != nil {
			return Intent{}, nil, fmt.Errorf("invalid Claude output_config: %w", err)
		}
		if output.Effort != "" {
			effort, err := ParseEffort(output.Effort)
			if err != nil {
				return Intent{}, nil, err
			}
			intent.Effort = effort
		}
	}
	if intent.Mode == ModeDisabled && intent.Effort != "" && intent.Effort != EffortNone {
		return intent, diagnostics, nil
	}
	normalized, normalizeDiagnostics, err := normalizeIntent(intent)
	if err != nil {
		return Intent{}, nil, err
	}
	return normalized, append(diagnostics, normalizeDiagnostics...), nil
}

func EffectiveEffort(intent Intent) Effort {
	intent, _, err := normalizeIntent(intent)
	if err != nil {
		return ""
	}
	if intent.Mode == ModeDisabled {
		return EffortNone
	}
	if intent.Effort != "" {
		return intent.Effort
	}
	if intent.BudgetTokens != nil {
		return EffortFromBudget(*intent.BudgetTokens)
	}
	if intent.Mode == ModeEnabled || intent.Mode == ModeAdaptive {
		return EffortHigh
	}
	return ""
}

func EffortFromBudget(budget int) Effort {
	if budget == 0 {
		return EffortNone
	}
	if budget < 0 {
		return EffortHigh
	}
	if budget <= 1024 {
		return EffortLow
	}
	if budget <= 8192 {
		return EffortMedium
	}
	return EffortHigh
}
