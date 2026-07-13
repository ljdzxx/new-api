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
	CacheWrite   string `json:"cache_write,omitempty"`
	CacheWrite5m string `json:"cache_write_5m,omitempty"`
	CacheWrite1h string `json:"cache_write_1h,omitempty"`
	ImageInput   string `json:"image_input,omitempty"`
	AudioInput   string `json:"audio_input,omitempty"`
	AudioOutput  string `json:"audio_output,omitempty"`
}

func (p *Prices) UnmarshalJSON(data []byte) error {
	type pricesWire Prices
	var wire struct {
		pricesWire
		LegacyCacheRead string `json:"cache_hits_refreshes"`
	}
	if err := common.Unmarshal(data, &wire); err != nil {
		return err
	}
	*p = Prices(wire.pricesWire)
	if strings.TrimSpace(p.CacheRead) == "" {
		p.CacheRead = wire.LegacyCacheRead
	}
	return nil
}

type TierCondition struct {
	Metric   string `json:"metric"`
	Operator string `json:"operator"`
	Value    int64  `json:"value"`
}

type Tier struct {
	ID         string          `json:"id"`
	Priority   int             `json:"priority"`
	Fallback   bool            `json:"fallback,omitempty"`
	Conditions []TierCondition `json:"conditions,omitempty"`
	Prices     Prices          `json:"prices"`
}

type Usage struct {
	InputTotalTokens  int64
	OutputTotalTokens int64
}

type AdjustmentCondition struct {
	Source   string `json:"source"`
	Path     string `json:"path,omitempty"`
	Operator string `json:"operator"`
	Value    string `json:"value,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

type Adjustment struct {
	ID         string                `json:"id"`
	Conditions []AdjustmentCondition `json:"conditions"`
	Multiplier string                `json:"multiplier"`
}

type Policy struct {
	Version     int                  `json:"version"`
	Mode        string               `json:"mode"`
	Currency    string               `json:"currency"`
	Unit        string               `json:"unit"`
	Price       string               `json:"price,omitempty"`
	Prices      Prices               `json:"prices,omitempty"`
	Tiers       []Tier               `json:"tiers,omitempty"`
	Adjustments []Adjustment         `json:"adjustments,omitempty"`
	Tools       map[string]ToolPrice `json:"tools,omitempty"`
}

type ToolPrice struct {
	Unit  string `json:"unit"`
	Price string `json:"price"`
}

const (
	ToolWebSearchStandard = "web_search.standard"
	ToolWebSearchPremium  = "web_search.premium"
	ToolClaudeWebSearch   = "claude_web_search"
	ToolFileSearch        = "file_search"
	ToolImagePrefix       = "image_generation."
)

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

// Snapshot freezes every pricing input that is allowed to change while a
// request is in flight. Settlement and logging must use this value instead of
// resolving the live policy again.
type Snapshot struct {
	SchemaVersion        int                 `json:"schema_version"`
	Revision             int64               `json:"revision"`
	Model                string              `json:"model"`
	Policy               Policy              `json:"policy"`
	AdjustmentMultiplier string              `json:"adjustment_multiplier"`
	AppliedAdjustments   []AppliedAdjustment `json:"applied_adjustments,omitempty"`
	EvaluatedAtUnixNano  int64               `json:"evaluated_at_unix_nano"`
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
	return resolvePolicy(store.config.Policies, modelName)
}

func resolvePolicy(policies map[string]Policy, modelName string) (Policy, bool) {
	if policy, ok := policies[modelName]; ok {
		return clonePolicy(policy), true
	}
	bestKey := ""
	var best Policy
	for key, policy := range policies {
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
	return clonePolicy(best), bestKey != ""
}

func CaptureSnapshot(modelName string, requestCtx RequestContext) (*Snapshot, bool) {
	store.RLock()
	defer store.RUnlock()
	config := store.config
	if config.State != StateActive {
		return nil, false
	}
	policy, ok := resolvePolicy(config.Policies, modelName)
	if !ok {
		return nil, false
	}
	if requestCtx.Now.IsZero() {
		requestCtx.Now = time.Now()
	}
	multiplier, applied := EvaluateAdjustments(policy, requestCtx)
	return &Snapshot{
		SchemaVersion:        config.SchemaVersion,
		Revision:             config.Revision,
		Model:                modelName,
		Policy:               policy,
		AdjustmentMultiplier: multiplier.String(),
		AppliedAdjustments:   append([]AppliedAdjustment(nil), applied...),
		EvaluatedAtUnixNano:  requestCtx.Now.UnixNano(),
	}, true
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
	CacheCreation5mRatio float64
	CacheCreation1hRatio float64
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
	if policy.Mode == "tiered" {
		return LegacyValues{}, fmt.Errorf("tiered policy requires usage context")
	}
	return legacyValuesFromPrices(policy.Prices)
}

func ToLegacyValuesForUsage(policy Policy, usage Usage) (LegacyValues, string, error) {
	if err := ValidatePolicy(policy); err != nil {
		return LegacyValues{}, "", err
	}
	if policy.Mode != "tiered" {
		values, err := ToLegacyValues(policy)
		return values, "", err
	}
	tier, ok := MatchTier(policy, usage)
	if !ok {
		return LegacyValues{}, "", fmt.Errorf("tiered policy has no matching tier")
	}
	values, err := legacyValuesFromPrices(tier.Prices)
	return values, tier.ID, err
}

// EffectivePricesForUsage resolves the concrete price table used by a request.
// Tiered policies return the matched tier ID; other modes return an empty ID.
func EffectivePricesForUsage(policy Policy, usage Usage) (Prices, string, error) {
	if err := ValidatePolicy(policy); err != nil {
		return Prices{}, "", err
	}
	if policy.Mode == "per_request" {
		return Prices{}, "", nil
	}
	if policy.Mode == "per_token" {
		return policy.Prices, "", nil
	}
	tier, ok := MatchTier(policy, usage)
	if !ok {
		return Prices{}, "", fmt.Errorf("tiered policy has no matching tier")
	}
	return tier.Prices, tier.ID, nil
}

func MatchTier(policy Policy, usage Usage) (Tier, bool) {
	tiers := append([]Tier(nil), policy.Tiers...)
	sort.SliceStable(tiers, func(i, j int) bool { return tiers[i].Priority < tiers[j].Priority })
	var fallback *Tier
	for index := range tiers {
		tier := tiers[index]
		if tier.Fallback {
			copyTier := tier
			fallback = &copyTier
			continue
		}
		matched := true
		for _, condition := range tier.Conditions {
			if !matchTierCondition(condition, usage) {
				matched = false
				break
			}
		}
		if matched {
			return tier, true
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return Tier{}, false
}

func matchTierCondition(condition TierCondition, usage Usage) bool {
	actual := usage.InputTotalTokens
	if condition.Metric == "output_total_tokens" {
		actual = usage.OutputTotalTokens
	}
	switch condition.Operator {
	case "lt":
		return actual < condition.Value
	case "lte":
		return actual <= condition.Value
	case "gt":
		return actual > condition.Value
	case "gte":
		return actual >= condition.Value
	default:
		return false
	}
}

func legacyValuesFromPrices(prices Prices) (LegacyValues, error) {
	input, err := decimal.NewFromString(prices.Input)
	if err != nil || input.IsNegative() {
		return LegacyValues{}, fmt.Errorf("invalid input price")
	}
	modelRatio := input.Div(decimal.NewFromInt(2))
	values := LegacyValues{CompletionRatio: 1}
	values.ModelRatio, _ = modelRatio.Float64()
	if input.IsZero() {
		return values, nil
	}
	values.CompletionRatio, err = relativePrice(prices.Output, input, 1)
	if err != nil {
		return LegacyValues{}, err
	}
	values.CacheRatio, err = relativePrice(prices.CacheRead, input, 0)
	if err != nil {
		return LegacyValues{}, err
	}
	cacheWrite := prices.CacheWrite
	if strings.TrimSpace(cacheWrite) == "" {
		cacheWrite = prices.CacheWrite5m
	}
	values.CacheCreationRatio, err = relativePrice(cacheWrite, input, 0)
	if err != nil {
		return LegacyValues{}, err
	}
	if strings.TrimSpace(prices.CacheWrite5m) == "" {
		values.CacheCreation5mRatio = values.CacheCreationRatio
	} else {
		values.CacheCreation5mRatio, err = relativePrice(prices.CacheWrite5m, input, 0)
		if err != nil {
			return LegacyValues{}, err
		}
	}
	values.CacheCreation1hRatio, err = relativePrice(prices.CacheWrite1h, input, 0)
	if err != nil {
		return LegacyValues{}, err
	}
	values.ImageRatio, err = relativePrice(prices.ImageInput, input, 0)
	if err != nil {
		return LegacyValues{}, err
	}
	values.AudioRatio, err = relativePrice(prices.AudioInput, input, 0)
	if err != nil {
		return LegacyValues{}, err
	}
	if values.AudioRatio != 0 {
		audioInput, _ := decimal.NewFromString(prices.AudioInput)
		values.AudioCompletionRatio, err = relativePrice(prices.AudioOutput, audioInput, 0)
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
	if common.DebugTraceEnabled {
		common.SysLog("[billing-policy][config-parse] raw_json=" + value)
	}
	var next Config
	if err := common.UnmarshalJsonStr(value, &next); err != nil {
		if common.DebugTraceEnabled {
			common.SysError("[billing-policy][config-parse] decode_failed=" + err.Error())
		}
		return err
	}
	if err := ValidateConfig(&next); err != nil {
		if common.DebugTraceEnabled {
			common.SysError("[billing-policy][config-parse] validation_failed=" + err.Error())
		}
		return err
	}
	store.Lock()
	store.config = cloneConfig(next)
	store.Unlock()
	if common.DebugTraceEnabled {
		common.SysLog(fmt.Sprintf("[billing-policy][config-parse] loaded schema_version=%d revision=%d state=%s policies=%d", next.SchemaVersion, next.Revision, next.State, len(next.Policies)))
	}
	return nil
}

// TraceCurrentConfig emits the complete in-memory policy JSON when debug trace
// is enabled at runtime, including when the switch is turned on after startup.
func TraceCurrentConfig(reason string) {
	if !common.DebugTraceEnabled {
		return
	}
	common.SysLog(fmt.Sprintf("[billing-policy][config-snapshot] reason=%s raw_json=%s", reason, MarshalConfig()))
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
		ensureDefaultToolPrices(&policy)
		if err := ValidatePolicy(policy); err != nil {
			return fmt.Errorf("model %s: %w", name, err)
		}
		config.Policies[name] = policy
	}
	if config.State != StateLegacy && len(config.Policies) == 0 {
		return fmt.Errorf("billing policy state %s requires migrated policies", config.State)
	}
	return nil
}

func ensureDefaultToolPrices(policy *Policy) {
	if policy.Tools == nil {
		policy.Tools = make(map[string]ToolPrice)
	}
	defaults := map[string]ToolPrice{
		ToolWebSearchStandard:                {Unit: "per_thousand_calls", Price: "10"},
		ToolWebSearchPremium:                 {Unit: "per_thousand_calls", Price: "25"},
		ToolClaudeWebSearch:                  {Unit: "per_thousand_calls", Price: "10"},
		ToolFileSearch:                       {Unit: "per_thousand_calls", Price: "2.5"},
		ToolImagePrefix + "low.1024x1024":    {Unit: "per_request", Price: "0.011"},
		ToolImagePrefix + "low.1024x1536":    {Unit: "per_request", Price: "0.016"},
		ToolImagePrefix + "low.1536x1024":    {Unit: "per_request", Price: "0.016"},
		ToolImagePrefix + "medium.1024x1024": {Unit: "per_request", Price: "0.042"},
		ToolImagePrefix + "medium.1024x1536": {Unit: "per_request", Price: "0.063"},
		ToolImagePrefix + "medium.1536x1024": {Unit: "per_request", Price: "0.063"},
		ToolImagePrefix + "high.1024x1024":   {Unit: "per_request", Price: "0.167"},
		ToolImagePrefix + "high.1024x1536":   {Unit: "per_request", Price: "0.25"},
		ToolImagePrefix + "high.1536x1024":   {Unit: "per_request", Price: "0.25"},
	}
	for name, price := range defaults {
		if _, exists := policy.Tools[name]; !exists {
			policy.Tools[name] = price
		}
	}
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
		if policy.Unit != "per_request" {
			return fmt.Errorf("per-request policy requires per_request unit")
		}
		if strings.TrimSpace(policy.Price) == "" {
			return fmt.Errorf("per-request policy requires price")
		}
		price, err := decimal.NewFromString(policy.Price)
		if err != nil || price.IsNegative() {
			return fmt.Errorf("per-request price must be a non-negative decimal")
		}
	case "per_token":
		if policy.Unit != "per_million_tokens" {
			return fmt.Errorf("per-token policy requires per_million_tokens unit")
		}
		if strings.TrimSpace(policy.Prices.Input) == "" {
			return fmt.Errorf("per-token policy requires input price")
		}
		if err := validatePrices(policy.Prices); err != nil {
			return err
		}
	case "tiered":
		if policy.Unit != "per_million_tokens" {
			return fmt.Errorf("tiered policy requires per_million_tokens unit")
		}
		if len(policy.Tiers) == 0 {
			return fmt.Errorf("tiered policy requires tiers")
		}
		fallbackCount := 0
		priorities := map[int]struct{}{}
		ids := map[string]struct{}{}
		for index, tier := range policy.Tiers {
			if strings.TrimSpace(tier.ID) == "" {
				return fmt.Errorf("tier %d requires id", index)
			}
			if _, exists := ids[tier.ID]; exists {
				return fmt.Errorf("duplicate tier id: %s", tier.ID)
			}
			ids[tier.ID] = struct{}{}
			if _, exists := priorities[tier.Priority]; exists {
				return fmt.Errorf("duplicate tier priority: %d", tier.Priority)
			}
			priorities[tier.Priority] = struct{}{}
			if tier.Fallback {
				fallbackCount++
				if len(tier.Conditions) != 0 {
					return fmt.Errorf("fallback tier %s cannot have conditions", tier.ID)
				}
			} else if len(tier.Conditions) == 0 {
				return fmt.Errorf("non-fallback tier %s requires conditions", tier.ID)
			}
			for _, condition := range tier.Conditions {
				if err := validateTierCondition(condition); err != nil {
					return fmt.Errorf("tier %s: %w", tier.ID, err)
				}
			}
			if err := validatePrices(tier.Prices); err != nil {
				return fmt.Errorf("tier %s: %w", tier.ID, err)
			}
		}
		if fallbackCount != 1 {
			return fmt.Errorf("tiered policy requires exactly one fallback tier")
		}
	default:
		return fmt.Errorf("unsupported policy mode: %s", policy.Mode)
	}
	for index, adjustment := range policy.Adjustments {
		if strings.TrimSpace(adjustment.ID) == "" || len(adjustment.Conditions) == 0 {
			return fmt.Errorf("adjustment %d requires id and conditions", index)
		}
		multiplier, err := decimal.NewFromString(adjustment.Multiplier)
		if err != nil || !multiplier.IsPositive() {
			return fmt.Errorf("adjustment %s multiplier must be positive", adjustment.ID)
		}
		for _, condition := range adjustment.Conditions {
			if err := validateAdjustmentCondition(condition); err != nil {
				return fmt.Errorf("adjustment %s: %w", adjustment.ID, err)
			}
		}
	}
	for name, tool := range policy.Tools {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("tool price contains an empty name")
		}
		switch tool.Unit {
		case "per_thousand_calls", "per_request":
		default:
			return fmt.Errorf("tool %s has unsupported unit %s", name, tool.Unit)
		}
		price, err := decimal.NewFromString(tool.Price)
		if err != nil || price.IsNegative() {
			return fmt.Errorf("tool %s price must be a non-negative decimal", name)
		}
	}
	return nil
}

func validateAdjustmentCondition(condition AdjustmentCondition) error {
	switch condition.Source {
	case "header", "param":
		if strings.TrimSpace(condition.Path) == "" {
			return fmt.Errorf("%s condition requires path", condition.Source)
		}
	case "hour", "weekday":
		if strings.TrimSpace(condition.Timezone) == "" {
			return fmt.Errorf("time condition requires timezone")
		}
	default:
		return fmt.Errorf("unsupported adjustment source: %s", condition.Source)
	}
	switch condition.Operator {
	case "eq", "contains", "exists", "lt", "lte", "gt", "gte":
		return nil
	default:
		return fmt.Errorf("unsupported adjustment operator: %s", condition.Operator)
	}
}

func validatePrices(prices Prices) error {
	if strings.TrimSpace(prices.Input) == "" {
		return fmt.Errorf("input price is required")
	}
	for name, raw := range map[string]string{
		"input": prices.Input, "output": prices.Output, "cache_read": prices.CacheRead,
		"cache_write": prices.CacheWrite, "cache_write_5m": prices.CacheWrite5m, "cache_write_1h": prices.CacheWrite1h,
		"image_input": prices.ImageInput, "audio_input": prices.AudioInput, "audio_output": prices.AudioOutput,
	} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		value, err := decimal.NewFromString(raw)
		if err != nil || value.IsNegative() {
			return fmt.Errorf("%s price must be a non-negative decimal", name)
		}
	}
	return nil
}

func validateTierCondition(condition TierCondition) error {
	switch condition.Metric {
	case "input_total_tokens", "output_total_tokens":
	default:
		return fmt.Errorf("unsupported tier metric: %s", condition.Metric)
	}
	switch condition.Operator {
	case "lt", "lte", "gt", "gte":
	default:
		return fmt.Errorf("unsupported tier operator: %s", condition.Operator)
	}
	if condition.Value < 0 {
		return fmt.Errorf("tier condition value cannot be negative")
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
		copyConfig.Policies[name] = clonePolicy(policy)
	}
	return copyConfig
}

func clonePolicy(policy Policy) Policy {
	copyPolicy := policy
	if policy.Tiers != nil {
		copyPolicy.Tiers = make([]Tier, len(policy.Tiers))
		for i, tier := range policy.Tiers {
			copyPolicy.Tiers[i] = tier
			copyPolicy.Tiers[i].Conditions = append([]TierCondition(nil), tier.Conditions...)
		}
	}
	if policy.Adjustments != nil {
		copyPolicy.Adjustments = make([]Adjustment, len(policy.Adjustments))
		for i, adjustment := range policy.Adjustments {
			copyPolicy.Adjustments[i] = adjustment
			copyPolicy.Adjustments[i].Conditions = append([]AdjustmentCondition(nil), adjustment.Conditions...)
		}
	}
	copyPolicy.Tools = make(map[string]ToolPrice, len(policy.Tools))
	for name, price := range policy.Tools {
		copyPolicy.Tools[name] = price
	}
	return copyPolicy
}
