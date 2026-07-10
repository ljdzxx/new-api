package billing_policy

import (
	"fmt"
	"math"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/shopspring/decimal"
)

type MigrationIssue struct {
	Level   string `json:"level"`
	Model   string `json:"model,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type MigrationReport struct {
	SourceChecksum string           `json:"source_checksum"`
	Total          int              `json:"total"`
	PerToken       int              `json:"per_token"`
	PerRequest     int              `json:"per_request"`
	Issues         []MigrationIssue `json:"issues"`
	Candidate      Config           `json:"candidate"`
	Verified       int              `json:"verified"`
}

func LegacySourceValues() map[string]string {
	return map[string]string{
		"ModelPrice":           ratio_setting.ModelPrice2JSONString(),
		"ModelRatio":           ratio_setting.ModelRatio2JSONString(),
		"CompletionRatio":      ratio_setting.CompletionRatio2JSONString(),
		"CacheRatio":           ratio_setting.CacheRatio2JSONString(),
		"CreateCacheRatio":     ratio_setting.CreateCacheRatio2JSONString(),
		"ImageRatio":           ratio_setting.ImageRatio2JSONString(),
		"AudioRatio":           ratio_setting.AudioRatio2JSONString(),
		"AudioCompletionRatio": ratio_setting.AudioCompletionRatio2JSONString(),
	}
}

func BuildLegacyMigrationCandidate(targetState string) (MigrationReport, error) {
	sources := LegacySourceValues()
	report := MigrationReport{SourceChecksum: SourceChecksum(sources)}
	config := NewConfig()
	config.State = targetState

	prices := ratio_setting.GetModelPriceCopy()
	ratios := ratio_setting.GetModelRatioCopy()
	completion := ratio_setting.GetCompletionRatioCopy()
	cache := ratio_setting.GetCacheRatioCopy()
	cacheCreate := ratio_setting.GetCreateCacheRatioCopy()
	image := ratio_setting.GetImageRatioCopy()
	audio := ratio_setting.GetAudioRatioCopy()
	audioOutput := ratio_setting.GetAudioCompletionRatioCopy()

	names := map[string]struct{}{}
	for _, values := range []map[string]float64{prices, ratios, completion, cache, cacheCreate, image, audio, audioOutput} {
		for name := range values {
			names[name] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	for _, name := range ordered {
		if price, ok := prices[name]; ok {
			if _, conflict := ratios[name]; conflict {
				report.Issues = append(report.Issues, MigrationIssue{Level: "warning", Model: name, Code: "PRICE_RATIO_CONFLICT", Message: "fixed price takes precedence over the legacy token ratio"})
			}
			if err := validateLegacyNumber(price); err != nil {
				report.Issues = append(report.Issues, MigrationIssue{Level: "error", Model: name, Code: "INVALID_PRICE", Message: err.Error()})
				continue
			}
			config.Policies[name] = Policy{Version: SchemaVersion, Mode: "per_request", Currency: "USD", Unit: "per_request", Price: decimal.NewFromFloat(price).String()}
			if values, verifyErr := ToLegacyValues(config.Policies[name]); verifyErr != nil || math.Abs(values.ModelPrice-price) > 1e-12 {
				report.Issues = append(report.Issues, MigrationIssue{Level: "error", Model: name, Code: "VERIFY_FAILED", Message: "fixed price changed during migration"})
			} else {
				report.Verified++
			}
			report.PerRequest++
			continue
		}

		ratio, ok := ratios[name]
		if !ok {
			report.Issues = append(report.Issues, MigrationIssue{Level: "warning", Model: name, Code: "ORPHAN_RATIO", Message: "secondary ratio exists without a model ratio and was not migrated"})
			continue
		}
		if err := validateLegacyNumber(ratio); err != nil {
			report.Issues = append(report.Issues, MigrationIssue{Level: "error", Model: name, Code: "INVALID_RATIO", Message: err.Error()})
			continue
		}
		input := decimal.NewFromFloat(ratio).Mul(decimal.NewFromInt(2))
		policy := Policy{Version: SchemaVersion, Mode: "per_token", Currency: "USD", Unit: "per_million_tokens"}
		policy.Prices.Input = input.String()
		policy.Prices.Output = multiplyPrice(input, ratio_setting.GetCompletionRatio(name), true)
		cacheValue, hasCache := cache[name]
		policy.Prices.CacheRead = multiplyPrice(input, cacheValue, hasCache)
		cacheCreateValue, hasCacheCreate := cacheCreate[name]
		policy.Prices.CacheWrite5m = multiplyPrice(input, cacheCreateValue, hasCacheCreate)
		if policy.Prices.CacheWrite5m != "" {
			policy.Prices.CacheWrite1h = decimal.RequireFromString(policy.Prices.CacheWrite5m).Mul(decimal.NewFromFloat(6 / 3.75)).String()
		}
		imageValue, hasImage := image[name]
		policy.Prices.ImageInput = multiplyPrice(input, imageValue, hasImage)
		audioValue, hasAudio := audio[name]
		policy.Prices.AudioInput = multiplyPrice(input, audioValue, hasAudio)
		audioOutputValue, hasAudioOutput := audioOutput[name]
		if policy.Prices.AudioInput != "" {
			policy.Prices.AudioOutput = multiplyPrice(decimal.RequireFromString(policy.Prices.AudioInput), audioOutputValue, hasAudioOutput)
		}
		config.Policies[name] = policy
		values, verifyErr := ToLegacyValues(policy)
		if verifyErr != nil || !legacyRatiosEquivalent(values, ratio, ratio_setting.GetCompletionRatio(name), cacheValue, hasCache, cacheCreateValue, hasCacheCreate, imageValue, hasImage, audioValue, hasAudio, audioOutputValue, hasAudioOutput) {
			report.Issues = append(report.Issues, MigrationIssue{Level: "error", Model: name, Code: "VERIFY_FAILED", Message: "token ratios changed during migration"})
		} else {
			report.Verified++
		}
		report.PerToken++
	}

	report.Total = len(config.Policies)
	MarkMigrated(&config, report.SourceChecksum)
	report.Candidate = config
	for _, issue := range report.Issues {
		if issue.Level == "error" {
			return report, fmt.Errorf("legacy billing migration contains validation errors")
		}
	}
	if err := ValidateConfig(&config); err != nil {
		return report, err
	}
	return report, nil
}

func legacyRatiosEquivalent(values LegacyValues, modelRatio, completion, cache float64, hasCache bool, cacheCreate float64, hasCacheCreate bool, image float64, hasImage bool, audio float64, hasAudio bool, audioOutput float64, hasAudioOutput bool) bool {
	equal := func(left, right float64) bool { return math.Abs(left-right) <= 1e-9 }
	if modelRatio == 0 {
		return values.ModelRatio == 0
	}
	if !equal(values.ModelRatio, modelRatio) || !equal(values.CompletionRatio, completion) {
		return false
	}
	checks := []struct {
		actual   float64
		expected float64
		present  bool
	}{{values.CacheRatio, cache, hasCache}, {values.CacheCreationRatio, cacheCreate, hasCacheCreate}, {values.ImageRatio, image, hasImage}, {values.AudioRatio, audio, hasAudio}, {values.AudioCompletionRatio, audioOutput, hasAudioOutput}}
	for _, check := range checks {
		if check.present && !equal(check.actual, check.expected) {
			return false
		}
		if !check.present && check.actual != 0 {
			return false
		}
	}
	return true
}

func validateLegacyNumber(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fmt.Errorf("legacy price multiplier must be finite and non-negative")
	}
	return nil
}

func multiplyPrice(base decimal.Decimal, value float64, present bool) string {
	if !present {
		return ""
	}
	if err := validateLegacyNumber(value); err != nil {
		common.SysError(err.Error())
		return ""
	}
	return base.Mul(decimal.NewFromFloat(value)).String()
}
