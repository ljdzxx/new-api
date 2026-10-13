package types

import (
	"fmt"
	"math"
	"strings"
)

const MaxServiceTierMultiplier = 1000

// ServiceTierBilling describes a channel's fixed upstream pricing. It never
// participates in constructing or overriding an upstream request.
type ServiceTierBilling struct {
	Enabled bool                     `json:"enabled"`
	Rules   []ServiceTierBillingRule `json:"rules"`
}

type ServiceTierBillingRule struct {
	Model      string  `json:"model"`
	Tier       string  `json:"tier"`
	TagColor   string  `json:"tag_color"`
	TagType    string  `json:"tag_type"`
	Multiplier float64 `json:"multiplier"`
}

func (r ServiceTierBillingRule) Validate() error {
	if r.Model == "" || r.Model != strings.TrimSpace(r.Model) || len(r.Model) > 255 || (strings.Contains(r.Model, "*") && r.Model != "*") {
		return fmt.Errorf("service tier model must be an exact model name or *")
	}
	if r.Tier == "" || r.Tier != strings.TrimSpace(r.Tier) || len(r.Tier) > 64 {
		return fmt.Errorf("service tier must contain 1-64 bytes without surrounding whitespace")
	}
	if r.Multiplier <= 0 || math.IsNaN(r.Multiplier) || math.IsInf(r.Multiplier, 0) || r.Multiplier > MaxServiceTierMultiplier {
		return fmt.Errorf("service tier multiplier must be > 0 and <= %d", MaxServiceTierMultiplier)
	}
	switch r.TagColor {
	case "grey", "red", "pink", "purple", "violet", "indigo", "blue", "light-blue", "cyan", "teal", "green", "light-green", "lime", "yellow", "amber", "orange", "white":
	default:
		return fmt.Errorf("invalid Semi Tag color: %s", r.TagColor)
	}
	switch r.TagType {
	case "light", "solid", "ghost":
	default:
		return fmt.Errorf("invalid Semi Tag type: %s", r.TagType)
	}
	return nil
}

func (s *ServiceTierBilling) Validate() error {
	if s == nil {
		return nil
	}
	if s.Enabled && len(s.Rules) == 0 {
		return fmt.Errorf("enabled service tier billing requires at least one rule")
	}
	seen := make(map[string]bool)
	for _, rule := range s.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		if seen[rule.Model] {
			return fmt.Errorf("duplicate service tier model: %s", rule.Model)
		}
		seen[rule.Model] = true
	}
	return nil
}

// Match returns a detached snapshot, with exact matches taking precedence over *.
func (s *ServiceTierBilling) Match(model string) (*ServiceTierBillingRule, error) {
	if s == nil || !s.Enabled {
		return nil, nil
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var fallback *ServiceTierBillingRule
	for _, rule := range s.Rules {
		if rule.Model == model {
			return &rule, nil
		}
		if rule.Model == "*" {
			fallback = &rule
		}
	}
	return fallback, nil
}

func (p *PriceData) ServiceTierMultiplier() float64 {
	if p.ServiceTier == nil {
		return 1
	}
	return p.ServiceTier.Multiplier
}
