package billing_policy

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

const (
	SchemaVersion = 1
	StateLegacy   = "legacy"
	StateShadow   = "shadow"
	StatePrepared = "prepared"
	StateActive   = "active"
)

type Prices struct {
	Input        string `json:"input,omitempty"`
	Output       string `json:"output,omitempty"`
	CacheRead    string `json:"cache_read,omitempty"`
	CacheWrite5m string `json:"cache_write_5m,omitempty"`
	CacheWrite1h string `json:"cache_write_1h,omitempty"`
	ImageInput   string `json:"image_input,omitempty"`
	AudioInput   string `json:"audio_input,omitempty"`
	AudioOutput  string `json:"audio_output,omitempty"`
}

type Policy struct {
	Version  int    `json:"version"`
	Mode     string `json:"mode"`
	Currency string `json:"currency"`
	Unit     string `json:"unit"`
	Price    string `json:"price,omitempty"`
	Prices   Prices `json:"prices,omitempty"`
}

type MigrationMeta struct {
	Version        int    `json:"version"`
	SourceChecksum string `json:"source_checksum"`
	MigratedAt     int64  `json:"migrated_at,omitempty"`
}

type Config struct {
	SchemaVersion int               `json:"schema_version"`
	Revision      int64             `json:"revision"`
	State         string            `json:"state"`
	Migration     MigrationMeta     `json:"migration"`
	Policies      map[string]Policy `json:"policies"`
}

var store = struct {
	sync.RWMutex
	config Config
}{config: NewConfig()}

func NewConfig() Config {
	return Config{SchemaVersion: SchemaVersion, Revision: 1, State: StateLegacy, Policies: map[string]Policy{}}
}

func GetConfig() Config {
	store.RLock()
	defer store.RUnlock()
	return cloneConfig(store.config)
}

func GetState() string {
	store.RLock()
	defer store.RUnlock()
	return store.config.State
}

func IsShadow() bool { return GetState() == StateShadow }
func IsActive() bool { return GetState() == StateActive }

func Resolve(modelName string) (Policy, bool) {
	store.RLock()
	defer store.RUnlock()
	if policy, ok := store.config.Policies[modelName]; ok {
		return policy, true
	}
	bestKey := ""
	var best Policy
	for key, policy := range store.config.Policies {
		if !strings.Contains(key, "*") || !wildcardMatch(key, modelName) {
			continue
		}
		specificity := len(strings.ReplaceAll(key, "*", ""))
		bestSpecificity := len(strings.ReplaceAll(bestKey, "*", ""))
		if bestKey == "" || specificity > bestSpecificity || (specificity == bestSpecificity && key < bestKey) {
			bestKey = key
			best = policy
		}
	}
	return best, bestKey != ""
}

func wildcardMatch(pattern, value string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == value
	}
	position := 0
	for index, part := range parts {
		if part == "" {
			continue
		}
		found := strings.Index(value[position:], part)
		if found < 0 || (index == 0 && !strings.HasPrefix(value, part)) {
			return false
		}
		position += found + len(part)
	}
	last := parts[len(parts)-1]
	return last == "" || strings.HasSuffix(value, last)
}

type LegacyValues struct {
	UsePrice             bool
	ModelPrice           float64
	ModelRatio           float64
	CompletionRatio      float64
	CacheRatio           float64
	CacheCreationRatio   float64
	ImageRatio           float64
	AudioRatio           float64
	AudioCompletionRatio float64
}

// ToLegacyValues lets the existing settlement paths consume a migrated policy
// without reading any of the old option maps. It is intentionally limited to
// the modes supported by the first migration version.
func ToLegacyValues(policy Policy) (LegacyValues, error) {
	if err := ValidatePolicy(policy); err != nil {
		return LegacyValues{}, err
	}
	if policy.Mode == "per_request" {
		price, err := decimal.NewFromString(policy.Price)
		if err != nil || price.IsNegative() {
			return LegacyValues{}, fmt.Errorf("invalid per-request price")
		}
		value, _ := price.Float64()
		return LegacyValues{UsePrice: true, ModelPrice: value}, nil
	}
	input, err := decimal.NewFromString(policy.Prices.Input)
	if err != nil || input.IsNegative() {
		return LegacyValues{}, fmt.Errorf("invalid input price")
	}
	modelRatio := input.Div(decimal.NewFromInt(2))
	values := LegacyValues{CompletionRatio: 1}
	values.ModelRatio, _ = modelRatio.Float64()
	values.CompletionRatio, err = relativePrice(policy.Prices.Output, input, 1)
	if err != nil {
		return LegacyValues{}, err
	}
	values.CacheRatio, err = relativePrice(policy.Prices.CacheRead, input, 0)
	if err != nil {
		return LegacyValues{}, err
	}
	values.CacheCreationRatio, err = relativePrice(policy.Prices.CacheWrite5m, input, 0)
	if err != nil {
		return LegacyValues{}, err
	}
	values.ImageRatio, err = relativePrice(policy.Prices.ImageInput, input, 0)
	if err != nil {
		return LegacyValues{}, err
	}
	values.AudioRatio, err = relativePrice(policy.Prices.AudioInput, input, 0)
	if err != nil {
		return LegacyValues{}, err
	}
	if values.AudioRatio != 0 {
		audioInput, _ := decimal.NewFromString(policy.Prices.AudioInput)
		values.AudioCompletionRatio, err = relativePrice(policy.Prices.AudioOutput, audioInput, 0)
	}
	return values, err
}

func relativePrice(raw string, base decimal.Decimal, missing float64) (float64, error) {
	if strings.TrimSpace(raw) == "" {
		return missing, nil
	}
	value, err := decimal.NewFromString(raw)
	if err != nil || value.IsNegative() || base.IsZero() {
		return 0, fmt.Errorf("invalid relative policy price")
	}
	ratio, _ := value.Div(base).Float64()
	return ratio, nil
}

func MarshalConfig() string {
	data, err := common.Marshal(GetConfig())
	if err != nil {
		common.SysError("failed to marshal model billing policy: " + err.Error())
		return "{}"
	}
	return string(data)
}

func UpdateFromJSON(value string) error {
	var next Config
	if err := common.UnmarshalJsonStr(value, &next); err != nil {
		return err
	}
	if err := ValidateConfig(&next); err != nil {
		return err
	}
	store.Lock()
	store.config = cloneConfig(next)
	store.Unlock()
	return nil
}

func ValidateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("billing policy config is nil")
	}
	if config.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported billing policy schema version: %d", config.SchemaVersion)
	}
	switch config.State {
	case StateLegacy, StateShadow, StatePrepared, StateActive:
	default:
		return fmt.Errorf("invalid billing policy state: %s", config.State)
	}
	if config.Revision <= 0 {
		return fmt.Errorf("billing policy revision must be positive")
	}
	if config.Policies == nil {
		config.Policies = map[string]Policy{}
	}
	for name, policy := range config.Policies {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("billing policy contains an empty model name")
		}
		if err := ValidatePolicy(policy); err != nil {
			return fmt.Errorf("model %s: %w", name, err)
		}
	}
	if config.State != StateLegacy && len(config.Policies) == 0 {
		return fmt.Errorf("billing policy state %s requires migrated policies", config.State)
	}
	return nil
}

func ValidatePolicy(policy Policy) error {
	if policy.Version != SchemaVersion {
		return fmt.Errorf("unsupported policy version: %d", policy.Version)
	}
	if policy.Currency != "USD" {
		return fmt.Errorf("unsupported currency: %s", policy.Currency)
	}
	switch policy.Mode {
	case "per_request":
		if strings.TrimSpace(policy.Price) == "" {
			return fmt.Errorf("per-request policy requires price")
		}
	case "per_token":
		if strings.TrimSpace(policy.Prices.Input) == "" {
			return fmt.Errorf("per-token policy requires input price")
		}
	default:
		return fmt.Errorf("unsupported policy mode: %s", policy.Mode)
	}
	return nil
}

func SourceChecksum(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, key := range keys {
		_, _ = h.Write([]byte(key))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(values[key]))
		_, _ = h.Write([]byte{0})
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

func MarkMigrated(config *Config, sourceChecksum string) {
	config.Migration = MigrationMeta{Version: SchemaVersion, SourceChecksum: sourceChecksum, MigratedAt: time.Now().Unix()}
}

func cloneConfig(config Config) Config {
	copyConfig := config
	copyConfig.Policies = make(map[string]Policy, len(config.Policies))
	for name, policy := range config.Policies {
		copyConfig.Policies[name] = policy
	}
	return copyConfig
}
