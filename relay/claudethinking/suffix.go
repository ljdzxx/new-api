// Claude reasoning logic adapted from QuantumNous/new-api upstream 7d8b91268dab69d7150f345c448b5e250163146a.
// Kept independent of relaykit to support this branch's existing relay architecture.

package claudethinking

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/samber/lo"
)

// requestModelIntent handles the upstream Claude suffixes and reasoning-only
// @ modifiers. Other model modifiers remain opaque in this older relay stack.
func requestModelIntent(model string) (string, Intent, error) {
	if model_setting.ShouldPreserveThinkingSuffix(model) {
		return model, Intent{}, nil
	}
	spec := ParseModelModifiers(model)
	for _, modifier := range spec.Modifiers {
		if modifier.Key != "effort" && modifier.Key != "thinking" {
			return model, Intent{}, nil
		}
	}
	allowLegacy := model_setting.GetClaudeSettings().ThinkingAdapterEnabled && !model_setting.ShouldPreserveThinkingSuffix(spec.Base)
	base, legacy, _, err := ParseClaudeModelSuffix(spec.Base, allowLegacy)
	if err != nil {
		return model, Intent{}, err
	}
	if len(spec.Modifiers) == 0 {
		return base, legacy, nil
	}
	last := make(map[string]int)
	for i, modifier := range spec.Modifiers {
		last[modifier.Key] = i
	}
	var intent Intent
	for i, modifier := range spec.Modifiers {
		if last[modifier.Key] != i {
			continue
		}
		switch modifier.Key {
		case "effort":
			effort, err := ParseEffort(modifier.Value)
			if err != nil || effort == "" {
				return model, Intent{}, fmt.Errorf("invalid effort modifier %q", modifier.Value)
			}
			if effort == EffortNone {
				intent = Intent{Mode: ModeDisabled, Effort: EffortNone, Source: SourceSuffix}
			} else {
				if intent.Mode == ModeUnset || intent.Mode == ModeDisabled {
					intent.Mode = ModeEnabled
				}
				intent.Effort, intent.Source = effort, SourceSuffix
			}
		case "thinking":
			parsed, ok := ParseThinkingModifier(modifier.Value)
			if !ok {
				return model, Intent{}, fmt.Errorf("invalid thinking modifier %q", modifier.Value)
			}
			if !intent.IsEmpty() && parsed.Mode != ModeDisabled {
				intent.Mode, intent.Source = parsed.Mode, SourceSuffix
				if parsed.BudgetTokens != nil {
					intent.BudgetTokens, intent.BudgetSource = parsed.BudgetTokens, parsed.BudgetSource
				}
			} else {
				intent = parsed
			}
		}
	}
	return base, intent, nil
}

var legacyClaudeModelPattern = regexp.MustCompile(`^claude-[a-z0-9][a-z0-9._-]*$`)

type ModelModifier struct {
	Key   string
	Value string
}

type ModelModifierSpec struct {
	Raw       string
	Base      string
	Modifiers []ModelModifier
}

func (s ModelModifierSpec) HasModifiers() bool {
	return len(s.Modifiers) > 0
}

func ParseModelModifiers(modelName string) ModelModifierSpec {
	spec := ModelModifierSpec{Raw: modelName, Base: modelName}
	parts := strings.Split(modelName, "@")
	if len(parts) < 2 {
		return spec
	}

	firstModifier := len(parts)
	for i := len(parts) - 1; i > 0; i-- {
		key, value, ok := parseModelModifierSegment(parts[i])
		if !ok {
			break
		}
		firstModifier = i
		spec.Modifiers = append([]ModelModifier{{Key: key, Value: value}}, spec.Modifiers...)
	}
	if firstModifier == len(parts) {
		return spec
	}

	base := strings.Join(parts[:firstModifier], "@")
	if base == "" {
		return ModelModifierSpec{Raw: modelName, Base: modelName}
	}
	spec.Base = base
	return spec
}

func parseModelModifierSegment(segment string) (string, string, bool) {
	colon := strings.IndexByte(segment, ':')
	if colon <= 0 {
		return "", "", false
	}
	key := segment[:colon]
	for i, r := range key {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if i == 0 && !letter || i > 0 && !letter && (r < '0' || r > '9') && r != '_' && r != '-' {
			return "", "", false
		}
	}
	return strings.ToLower(key), segment[colon+1:], true
}

func ParseThinkingModifier(raw string) (Intent, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "on":
		return Intent{Mode: ModeEnabled, Source: SourceSuffix}, true
	case "adaptive":
		return Intent{Mode: ModeAdaptive, Source: SourceSuffix}, true
	case "off":
		return Intent{Mode: ModeDisabled, Effort: EffortNone, Source: SourceSuffix}, true
	}
	if budget, err := strconv.Atoi(value); err == nil {
		if budget < -1 {
			return Intent{}, false
		}
		if budget == 0 {
			return Intent{Mode: ModeDisabled, Effort: EffortNone, Source: SourceSuffix}, true
		}
		return Intent{
			Mode:         ModeEnabled,
			BudgetTokens: &budget,
			Source:       SourceSuffix,
			BudgetSource: SourceSuffix,
		}, true
	}
	return Intent{}, false
}

func TrimEffortSuffixWithSuffixes(modelName string, suffixes []string) (string, string, bool) {
	suffix, found := lo.Find(suffixes, func(s string) bool {
		return strings.HasSuffix(modelName, s)
	})
	if !found {
		return modelName, "", false
	}
	return strings.TrimSuffix(modelName, suffix), strings.TrimPrefix(suffix, "-"), true
}

func ParseClaudeModelSuffix(modelName string, allowSuffixAlias bool) (string, Intent, bool, error) {
	prefix, bare := splitModelNamespace(modelName)
	if !strings.HasPrefix(bare, "claude-") {
		return modelName, Intent{}, false, nil
	}
	base, intent, found, err := parseProviderModelSuffix(bare, "claude-", allowSuffixAlias, true)
	if err != nil || !found || !legacyClaudeModelPattern.MatchString(base) {
		return modelName, Intent{}, false, err
	}
	return prefix + base, intent, true, nil
}

func isKnownClaudeModel(modelName string) bool {
	baseModel, _, _ := TrimEffortSuffixWithSuffixes(capabilityModelName(modelName), []string{"-max", "-xhigh", "-high", "-medium", "-low", "-minimal", "-none"})
	if marker := strings.LastIndex(baseModel, "-thinking-"); marker >= 0 {
		baseModel = baseModel[:marker]
	} else {
		baseModel = strings.TrimSuffix(strings.TrimSuffix(baseModel, "-thinking"), "-nothinking")
	}
	knownPrefixes := []string{
		"claude-fable-5", "claude-mythos-5", "claude-mythos-preview",
		"claude-opus-5", "claude-sonnet-5", "claude-haiku-5", "claude-opus-4-8",
		"claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-4-6",
		"claude-opus-4-5", "claude-sonnet-4-5", "claude-haiku-4-5",
		"claude-opus-4-1", "claude-opus-4-", "claude-sonnet-4-",
		"claude-3-7-sonnet",
	}
	for _, prefix := range knownPrefixes {
		if strings.HasPrefix(baseModel, prefix) {
			return true
		}
	}
	return false
}

func splitModelNamespace(modelName string) (string, string) {
	if slash := strings.LastIndex(modelName, "/"); slash >= 0 {
		return modelName[:slash+1], modelName[slash+1:]
	}
	return "", modelName
}

func parseProviderModelSuffix(modelName string, requiredPrefix string, allowSuffixAlias bool, includeThoughts bool) (string, Intent, bool, error) {
	if !allowSuffixAlias {
		return modelName, Intent{}, false, nil
	}
	if marker := strings.LastIndex(modelName, "-thinking-"); marker >= 0 {
		baseModel := modelName[:marker]
		if !strings.HasPrefix(baseModel, requiredPrefix) {
			return modelName, Intent{}, false, nil
		}
		budget, err := strconv.Atoi(modelName[marker+len("-thinking-"):])
		if err != nil {
			return modelName, Intent{}, false, fmt.Errorf("invalid thinking budget suffix on model %q: %w", modelName, err)
		}
		intent := Intent{BudgetTokens: &budget, Source: SourceSuffix, BudgetSource: SourceSuffix}
		if includeThoughts {
			value := true
			intent.IncludeThoughts = &value
		}
		return baseModel, intent, true, nil
	}
	if baseModel, ok := strings.CutSuffix(modelName, "-nothinking"); ok {
		return baseModel, Intent{Mode: ModeDisabled, Effort: EffortNone, Source: SourceSuffix}, true, nil
	}
	if baseModel, ok := strings.CutSuffix(modelName, "-thinking"); ok {
		intent := Intent{Mode: ModeEnabled, Source: SourceSuffix}
		if includeThoughts {
			value := true
			intent.IncludeThoughts = &value
		}
		return baseModel, intent, true, nil
	}

	suffixes := []string{"-max", "-xhigh", "-high", "-medium", "-low", "-minimal", "-none"}
	baseModel, rawEffort, ok := TrimEffortSuffixWithSuffixes(modelName, suffixes)
	if !ok || !strings.HasPrefix(baseModel, requiredPrefix) {
		return modelName, Intent{}, false, nil
	}
	effort, err := ParseEffort(rawEffort)
	if err != nil {
		return modelName, Intent{}, false, err
	}
	intent := Intent{Effort: effort, Mode: ModeEnabled, Source: SourceSuffix}
	if effort == EffortNone {
		intent.Mode = ModeDisabled
	} else if includeThoughts {
		value := true
		intent.IncludeThoughts = &value
	}
	return baseModel, intent, true, nil
}
