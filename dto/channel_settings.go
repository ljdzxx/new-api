package dto

import (
	"fmt"
	"strings"
)

type ChannelForwardMetricRule struct {
	Key   string  `json:"key,omitempty"`
	Op    string  `json:"op,omitempty"`
	Value float64 `json:"value,omitempty"`
}

type ChannelForwardModelTarget struct {
	Model           string `json:"model,omitempty"`
	TargetChannelID int    `json:"target_channel_id,omitempty"`
}

type ChannelSettings struct {
	ForceFormat            bool                        `json:"force_format,omitempty"`
	ThinkingToContent      bool                        `json:"thinking_to_content,omitempty"`
	MockTest               bool                        `json:"mock_test,omitempty"`
	Proxy                  string                      `json:"proxy"`
	PassThroughBodyEnabled bool                        `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string                      `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool                        `json:"system_prompt_override,omitempty"`
	ForwardEnabled         bool                        `json:"forward_enabled,omitempty"`
	ForwardTargetChannelID int                         `json:"forward_target_channel_id,omitempty"`
	ForwardMatchRegex      string                      `json:"forward_match_regex,omitempty"`
	ForwardPrecheckModel   string                      `json:"forward_precheck_model,omitempty"`
	ForwardPrecheckPrompt  string                      `json:"forward_precheck_system_prompt,omitempty"`
	ForwardMaxMessageChars int                         `json:"forward_max_message_chars,omitempty"`
	ForwardMetricLogic     string                      `json:"forward_metric_logic,omitempty"`
	ForwardMetricRules     []ChannelForwardMetricRule  `json:"forward_metric_rules,omitempty"`
	ForwardModelTargets    []ChannelForwardModelTarget `json:"forward_model_targets,omitempty"`
	ErrorInterceptEnabled  bool                        `json:"error_intercept_enabled,omitempty"`
	ErrorInterceptMessage  string                      `json:"error_intercept_message,omitempty"`
}

func (s ChannelSettings) GetForwardRegexList() []string {
	if s.ForwardMatchRegex == "" {
		return nil
	}
	lines := strings.Split(s.ForwardMatchRegex, "\n")
	regexList := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		regexList = append(regexList, line)
	}
	return regexList
}

func (s ChannelSettings) Validate() error {
	if !s.ForwardEnabled {
		return nil
	}
	if strings.TrimSpace(s.ForwardPrecheckModel) == "" {
		return fmt.Errorf("forward_precheck_model is required")
	}
	if strings.TrimSpace(s.ForwardPrecheckPrompt) == "" {
		return fmt.Errorf("forward_precheck_system_prompt is required")
	}
	if s.ForwardMaxMessageChars < 0 {
		return fmt.Errorf("forward_max_message_chars must be greater than or equal to 0")
	}
	logic := strings.ToLower(strings.TrimSpace(s.ForwardMetricLogic))
	if logic == "" {
		logic = "or"
	}
	if logic != "and" && logic != "or" {
		return fmt.Errorf("forward_metric_logic must be AND or OR")
	}
	if len(s.ForwardMetricRules) == 0 {
		return fmt.Errorf("forward_metric_rules is required")
	}
	for _, rule := range s.ForwardMetricRules {
		if strings.TrimSpace(rule.Key) == "" {
			return fmt.Errorf("forward metric key is required")
		}
		switch strings.TrimSpace(rule.Op) {
		case ">=", ">", "<=", "<", "==", "!=":
		default:
			return fmt.Errorf("unsupported forward metric operator %q", rule.Op)
		}
	}
	if len(s.ForwardModelTargets) == 0 {
		return fmt.Errorf("forward_model_targets is required")
	}
	for _, target := range s.ForwardModelTargets {
		if strings.TrimSpace(target.Model) == "" {
			return fmt.Errorf("forward target model is required")
		}
		if target.TargetChannelID <= 0 {
			return fmt.Errorf("forward target channel id is required")
		}
	}
	return nil
}

func NormalizeForwardRegexPattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", fmt.Errorf("forward regex is empty")
	}
	if !strings.HasPrefix(pattern, "/") {
		return pattern, nil
	}
	body, flags, ok := splitJavaScriptRegexPattern(pattern)
	if !ok {
		return pattern, nil
	}
	modifiers := make([]byte, 0, len(flags))
	for i := 0; i < len(flags); i++ {
		flag := flags[i]
		if flag >= 'A' && flag <= 'Z' {
			flag = flag - 'A' + 'a'
		}
		switch flag {
		case 'i', 'm', 's':
			if !strings.ContainsRune(string(modifiers), rune(flag)) {
				modifiers = append(modifiers, flag)
			}
		case 'g', 'u', 'y', 'd':
			// Accepted for compatibility. They do not affect Go-side matching here.
		default:
			return "", fmt.Errorf("unsupported forward regex flag %q", string(flags[i]))
		}
	}
	body = strings.ReplaceAll(body, `\/`, `/`)
	if len(modifiers) == 0 {
		return body, nil
	}
	return "(?" + string(modifiers) + ")" + body, nil
}

func splitJavaScriptRegexPattern(pattern string) (body string, flags string, ok bool) {
	if len(pattern) < 2 || pattern[0] != '/' {
		return "", "", false
	}
	lastSlash := -1
	for i := len(pattern) - 1; i > 0; i-- {
		if pattern[i] != '/' || isEscapedRegexDelimiter(pattern, i) {
			continue
		}
		lastSlash = i
		break
	}
	if lastSlash <= 0 {
		return "", "", false
	}
	flags = pattern[lastSlash+1:]
	for i := 0; i < len(flags); i++ {
		ch := flags[i]
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') {
			return "", "", false
		}
	}
	return pattern[1:lastSlash], flags, true
}

func isEscapedRegexDelimiter(pattern string, index int) bool {
	backslashCount := 0
	for i := index - 1; i >= 0 && pattern[i] == '\\'; i-- {
		backslashCount++
	}
	return backslashCount%2 == 1
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string        `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType `json:"vertex_key_type,omitempty"` // "json" or "api_key"
	OpenRouterEnterprise                  *bool         `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool          `json:"claude_beta_query,omitempty"`         // Claude 渠道是否强制追加 ?beta=true
	AllowServiceTier                      bool          `json:"allow_service_tier,omitempty"`        // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo                     bool          `json:"allow_inference_geo,omitempty"`       // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed                            bool          `json:"allow_speed,omitempty"`               // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier                 bool          `json:"allow_safety_identifier,omitempty"`   // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore                          bool          `json:"disable_store,omitempty"`             // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation               bool          `json:"allow_include_obfuscation,omitempty"` // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	AwsKeyType                            AwsKeyType    `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool          `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled    bool          `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime      int64         `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels []string      `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels  []string      `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels      []string      `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}
