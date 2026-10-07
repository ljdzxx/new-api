package group_monitor

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const OptionKey = "GroupMonitorConfigSecret"
const DefaultSVGOutputDir = "data/monitor-svg"

type Group struct {
	Models         []string `json:"model"`
	Key            string   `json:"key"`
	SVGModel       string   `json:"svg_model"`
	LogicModel     string   `json:"logic_model"`
	LogicPrompt    *string  `json:"logic_prompt,omitempty"`
	LogicAnswer    *string  `json:"logic_answer,omitempty"`
	LogicMatchMode *string  `json:"logic_match_mode,omitempty"`
	Protocol       string   `json:"protocol"`
	SVGTest        *bool    `json:"svg_test"`
	LogicTest      *bool    `json:"logic_test"`
	Active         *bool    `json:"active"`
	Order          int      `json:"order"`
}

func (g Group) Visible() bool   { return g.Active == nil || *g.Active }
func (g Group) TestSVG() bool   { return g.SVGTest != nil && *g.SVGTest }
func (g Group) TestLogic() bool { return g.LogicTest != nil && *g.LogicTest }
func boolPointer(v bool) *bool  { return &v }

type Config struct {
	Enabled          bool             `json:"enabled"`
	Groups           map[string]Group `json:"groups"`
	BaseURL          string           `json:"base_url"`
	ProbeMinutes     int              `json:"probe_minutes"`
	TimeoutSeconds   int              `json:"timeout_seconds"`
	Concurrency      int              `json:"concurrency"`
	HistoryKeep      int              `json:"history_keep"`
	HistoryDays      int              `json:"history_days"`
	SVGEnabled       bool             `json:"svg_enabled,omitempty"` // legacy configuration migration only
	SVGModel         string           `json:"svg_model,omitempty"`
	SVGPrompt        string           `json:"svg_prompt"`
	SVGMinutes       int              `json:"svg_minutes"`
	SVGKeep          int              `json:"svg_keep"`
	SVGPrefix        string           `json:"svg_prefix"`
	SVGURLHours      int              `json:"svg_url_hours"`
	SVGPublicBaseURL string           `json:"svg_public_base_url"`
	SVGOutputDir     string           `json:"svg_output_dir"`
	LogicEnabled     bool             `json:"logic_enabled,omitempty"`
	LogicPrompt      string           `json:"logic_prompt"`
	LogicAnswer      string           `json:"logic_answer"`
	LogicMatchMode   string           `json:"logic_match_mode"`
	LogicMinutes     int              `json:"logic_minutes"`
}

func Default() Config {
	return Config{Groups: map[string]Group{}, ProbeMinutes: 5, TimeoutSeconds: 120, Concurrency: 2, HistoryKeep: 48, HistoryDays: 8,
		SVGMinutes: 60, SVGKeep: 10, SVGPrefix: "monitor-svg/", SVGURLHours: 24,
		SVGOutputDir: DefaultSVGOutputDir, LogicMatchMode: "exact",
		LogicMinutes: 60}
}

// ResolveLogic applies per-field overrides without persisting inherited values.
// Empty and whitespace-only overrides inherit the current global setting.
func (c Config) ResolveLogic(g Group) (prompt, answer, matchMode string) {
	prompt, answer, matchMode = c.LogicPrompt, c.LogicAnswer, c.LogicMatchMode
	if g.LogicPrompt != nil && strings.TrimSpace(*g.LogicPrompt) != "" {
		prompt = *g.LogicPrompt
	}
	if g.LogicAnswer != nil && strings.TrimSpace(*g.LogicAnswer) != "" {
		answer = *g.LogicAnswer
	}
	if g.LogicMatchMode != nil && strings.TrimSpace(*g.LogicMatchMode) != "" {
		matchMode = *g.LogicMatchMode
	}
	if strings.TrimSpace(matchMode) == "" {
		matchMode = "exact"
	}
	return
}

func Get() Config {
	c := Default()
	common.OptionMapRWMutex.RLock()
	raw := common.OptionMap[OptionKey]
	common.OptionMapRWMutex.RUnlock()
	if raw != "" {
		_ = common.UnmarshalJsonStr(raw, &c)
	}

	c.Normalize()
	return c
}

// Explicit false values override legacy global switches. Old logic tests use
// the first configured model when migrating to one dedicated model per group.
func (c *Config) Normalize() {
	if c.HistoryKeep == 0 {
		c.HistoryKeep = 48
	}
	if c.HistoryDays == 0 {
		c.HistoryDays = 8
	}
	if strings.TrimSpace(c.SVGOutputDir) == "" {
		c.SVGOutputDir = DefaultSVGOutputDir
	}
	if strings.TrimSpace(c.LogicMatchMode) == "" {
		c.LogicMatchMode = "exact"
	}
	for name, g := range c.Groups {
		if g.Protocol == "" {
			g.Protocol = "responses"
		}
		if g.Active == nil {
			g.Active = boolPointer(true)
		}
		if g.SVGTest == nil {
			g.SVGTest = boolPointer(c.SVGEnabled)
			if g.SVGModel == "" {
				g.SVGModel = c.SVGModel
			}
		}
		if g.LogicTest == nil {
			g.LogicTest = boolPointer(c.LogicEnabled)
			if g.LogicModel == "" && c.LogicEnabled && len(g.Models) > 0 {
				g.LogicModel = g.Models[0]
			}
		}
		c.Groups[name] = g
	}
	c.SVGEnabled = false
	c.SVGModel = ""
	c.LogicEnabled = false
}

func ValidURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func Validate(c Config) error {
	if c.SVGPublicBaseURL != "" && (!ValidURL(c.SVGPublicBaseURL) || !strings.HasPrefix(c.SVGPublicBaseURL, "https://")) {
		return fmt.Errorf("SVG 公开访问地址必须为 HTTPS 地址，不能包含查询参数")
	}
	if c.HistoryKeep < 1 || c.HistoryKeep > 1000 || c.HistoryDays < 1 || c.HistoryDays > 90 {
		return fmt.Errorf("测试历史数量为 1–1000，保留天数为 1–90")
	}
	if strings.TrimSpace(c.LogicMatchMode) != "" && c.LogicMatchMode != "exact" && c.LogicMatchMode != "contains" {
		return fmt.Errorf("逻辑题匹配方式必须为 exact 或 contains")
	}
	if strings.TrimSpace(c.SVGOutputDir) == "" || strings.ContainsAny(c.SVGOutputDir, "\x00\r\n") {
		return fmt.Errorf("SVG 本地输出目录无效")
	}
	if len(c.Groups) > 100 {
		return fmt.Errorf("最多配置 100 个监控分组")
	}
	if c.Enabled && (!ValidURL(c.BaseURL) || len(c.Groups) == 0) {
		return fmt.Errorf("启用监控需要有效的探测地址和分组")
	}
	for name, g := range c.Groups {
		if g.Protocol != "" && g.Protocol != "responses" && g.Protocol != "messages" {
			return fmt.Errorf("分组 %s 的 protocol 必须为 responses 或 messages", name)
		}
		if len(g.SVGModel) > 200 || len(g.LogicModel) > 200 {
			return fmt.Errorf("测试模型名称过长")
		}
		if g.TestSVG() && (strings.TrimSpace(g.SVGModel) == "" || strings.TrimSpace(c.SVGPrompt) == "") {
			return fmt.Errorf("分组 %s 的 SVG 测试需要 svg_model、题目", name)
		}
		if g.LogicPrompt != nil && len(*g.LogicPrompt) > 32000 {
			return fmt.Errorf("分组 %s 的逻辑题目过长", name)
		}
		if g.LogicAnswer != nil && len(*g.LogicAnswer) > 16000 {
			return fmt.Errorf("分组 %s 的预期答案过长", name)
		}
		logicPrompt, logicAnswer, logicMatchMode := c.ResolveLogic(g)
		if logicMatchMode != "exact" && logicMatchMode != "contains" {
			return fmt.Errorf("分组 %s 的逻辑题匹配方式必须为 exact 或 contains", name)
		}
		if g.TestLogic() && (strings.TrimSpace(g.LogicModel) == "" || strings.TrimSpace(logicPrompt) == "" || strings.TrimSpace(logicAnswer) == "") {
			return fmt.Errorf("分组 %s 的逻辑测试需要 logic_model、题目和预期答案", name)
		}
		if strings.TrimSpace(name) == "" || len(name) > 128 || len(g.Models) == 0 || len(g.Models) > 50 || strings.TrimSpace(g.Key) == "" {
			return fmt.Errorf("分组名称、模型列表或密钥无效")
		}
		seen := map[string]bool{}
		for _, m := range g.Models {
			if strings.TrimSpace(m) == "" || len(m) > 200 || seen[m] {
				return fmt.Errorf("模型名称为空、重复或过长")
			}
			seen[m] = true
		}
	}
	for _, n := range []int{c.ProbeMinutes, c.SVGMinutes, c.LogicMinutes} {
		if n < 1 || n > 10080 {
			return fmt.Errorf("检测频率必须为 1–10080 分钟")
		}
	}
	if c.TimeoutSeconds < 5 || c.TimeoutSeconds > 600 || c.Concurrency < 1 || c.Concurrency > 10 {
		return fmt.Errorf("超时或并发数超出允许范围")
	}
	if c.SVGKeep < 1 || c.SVGKeep > 100 || c.SVGURLHours < 1 || c.SVGURLHours > 168 {
		return fmt.Errorf("作品保留数量为 1–100，URL 有效期为 1–168 小时")
	}
	if len(c.SVGPrefix) > 200 || strings.Trim(c.SVGPrefix, "/ ") == "" || strings.Contains(c.SVGPrefix, "..") || strings.ContainsAny(c.SVGPrefix, "\\\r\n") {
		return fmt.Errorf("R2 对象前缀无效")
	}
	if len(c.SVGPrompt) > 32000 || len(c.LogicPrompt) > 32000 || len(c.LogicAnswer) > 16000 {
		return fmt.Errorf("测试题目或答案过长")
	}
	if c.SVGEnabled && (strings.TrimSpace(c.SVGModel) == "" || len(c.SVGModel) > 200 || strings.TrimSpace(c.SVGPrompt) == "") {
		return fmt.Errorf("SVG 检测需要专用模型、题目")
	}
	if c.LogicEnabled && (strings.TrimSpace(c.LogicPrompt) == "" || strings.TrimSpace(c.LogicAnswer) == "") {
		return fmt.Errorf("逻辑检测需要题目和预期答案")
	}
	return nil
}

func (c Config) Contains(group, model string) bool {
	if !c.Enabled {
		return false
	}
	g, ok := c.Groups[group]
	if !ok {
		return false
	}
	for _, m := range g.Models {
		if m == model {
			return true
		}
	}
	return false
}
