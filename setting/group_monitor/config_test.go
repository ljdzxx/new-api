package group_monitor

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestMonitorLogicOverridesRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, group, prompt, answer, mode string
	}{
		{"omitted", `{}`, "global question", "21", "contains"},
		{"null", `{"logic_prompt":null,"logic_answer":null,"logic_match_mode":null}`, "global question", "21", "contains"},
		{"empty", `{"logic_prompt":"","logic_answer":"","logic_match_mode":""}`, "global question", "21", "contains"},
		{"whitespace", `{"logic_prompt":" \n","logic_answer":"\t","logic_match_mode":"　 "}`, "global question", "21", "contains"},
		{"prompt only", `{"logic_prompt":"group question"}`, "group question", "21", "contains"},
		{"answer only", `{"logic_answer":"32"}`, "global question", "32", "contains"},
		{"mode only", `{"logic_match_mode":"exact"}`, "global question", "21", "exact"},
		{"all", `{"logic_prompt":" group question\n","logic_answer":" 32 ","logic_match_mode":"exact"}`, " group question\n", " 32 ", "exact"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.LogicPrompt, cfg.LogicAnswer, cfg.LogicMatchMode = "global question", "21", "contains"
			var group Group
			require.NoError(t, common.UnmarshalJsonStr(tc.group, &group))
			cfg.Groups["g"] = group
			cfg.Normalize()
			prompt, answer, mode := cfg.ResolveLogic(cfg.Groups["g"])
			require.Equal(t, tc.prompt, prompt)
			require.Equal(t, tc.answer, answer)
			require.Equal(t, tc.mode, mode)
			raw, err := common.Marshal(cfg)
			require.NoError(t, err)
			reloaded := Default()
			require.NoError(t, common.Unmarshal(raw, &reloaded))
			reloaded.Normalize()
			require.Equal(t, group.LogicPrompt, reloaded.Groups["g"].LogicPrompt)
			require.Equal(t, group.LogicAnswer, reloaded.Groups["g"].LogicAnswer)
			require.Equal(t, group.LogicMatchMode, reloaded.Groups["g"].LogicMatchMode)
			reloaded.LogicPrompt, reloaded.LogicAnswer, reloaded.LogicMatchMode = "new global question", "99", "exact"
			prompt, answer, mode = reloaded.ResolveLogic(reloaded.Groups["g"])
			if tc.prompt == "global question" {
				require.Equal(t, "new global question", prompt)
			} else {
				require.Equal(t, tc.prompt, prompt)
			}
			if tc.answer == "21" {
				require.Equal(t, "99", answer)
			} else {
				require.Equal(t, tc.answer, answer)
			}
			require.Equal(t, "exact", mode)
		})
	}
	for _, mode := range []string{"", " \t"} {
		cfg := Default()
		cfg.LogicMatchMode = mode
		_, _, resolved := cfg.ResolveLogic(Group{})
		require.Equal(t, "exact", resolved)
	}
}

func TestMonitorLogicOverrideValidation(t *testing.T) {
	for _, tc := range []struct {
		name, prompt, answer, mode, errorText string
		enabled                               bool
	}{
		{"standalone", "question", "32", "exact", "", true},
		{"default mode", "question", "32", "", "", true},
		{"missing prompt", " \n", "32", "exact", "题目和预期答案", true},
		{"missing answer", "question", "", "exact", "题目和预期答案", true},
		{"invalid mode", "question", "32", "regex", "exact 或 contains", true},
		{"prompt limit", strings.Repeat("q", 32000), "32", "contains", "", true},
		{"prompt too long", strings.Repeat("q", 32001), "32", "exact", "逻辑题目过长", true},
		{"answer limit", "question", strings.Repeat("a", 16000), "exact", "", true},
		{"answer too long", "question", strings.Repeat("a", 16001), "exact", "预期答案过长", true},
		{"disabled missing values", "", "", "", "", false},
		{"disabled invalid mode", "", "", "regex", "exact 或 contains", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.Groups["custom-group"] = Group{Models: []string{"probe"}, Key: "key", LogicModel: "reasoning",
				LogicTest: &tc.enabled, LogicPrompt: &tc.prompt, LogicAnswer: &tc.answer, LogicMatchMode: &tc.mode}
			cfg.Normalize()
			err := Validate(cfg)
			if tc.errorText == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "custom-group")
				require.ErrorContains(t, err, tc.errorText)
			}
		})
	}
}

func TestMonitorConfigBounds(t *testing.T) {
	valid := Default()
	valid.Enabled = true
	valid.BaseURL = "http://localhost:3000/v1"
	valid.Groups = map[string]Group{"test": {Models: []string{"model"}, Key: "test-key"}}
	require.NoError(t, Validate(valid))
	for _, mutate := range []func(*Config){func(c *Config) { c.ProbeMinutes = 0 }, func(c *Config) { c.SVGURLHours = 169 }, func(c *Config) { c.SVGPrefix = "../images" }, func(c *Config) { c.BaseURL = "file:///tmp" }, func(c *Config) { c.Concurrency = 11 }, func(c *Config) { c.SVGEnabled = true }} {
		c := valid
		mutate(&c)
		require.Error(t, Validate(c))
	}
	require.True(t, valid.Contains("test", "model"))
	require.False(t, valid.Contains("test", "other"))
}

func TestMonitorGroupMigrationPreservesExplicitFalse(t *testing.T) {
	c := Default()
	require.NoError(t, common.UnmarshalJsonStr(`{"svg_enabled":true,"svg_model":"old-svg","logic_enabled":true,"groups":{"old":{"model":["a","b"],"key":"k"},"new":{"model":["a"],"key":"k","protocol":"messages","svg_model":"new-svg","logic_model":"new-logic","svg_test":false,"logic_test":false,"active":false}}}`, &c))
	c.Normalize()
	old := c.Groups["old"]
	require.True(t, old.Visible())
	require.True(t, old.TestSVG())
	require.True(t, old.TestLogic())
	require.Equal(t, "responses", old.Protocol)
	require.Equal(t, "old-svg", old.SVGModel)
	require.Equal(t, "a", old.LogicModel)
	g := c.Groups["new"]
	require.False(t, g.Visible())
	require.False(t, g.TestSVG())
	require.False(t, g.TestLogic())
	require.Equal(t, "new-svg", g.SVGModel)
	require.Equal(t, "new-logic", g.LogicModel)
	raw, err := common.Marshal(c)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"active":false`)
	require.NotContains(t, string(raw), "svg_enabled")
}

func TestMonitorPerGroupValidation(t *testing.T) {
	c := Default()
	c.SVGPrompt = "draw"
	c.LogicPrompt = "question"
	c.LogicAnswer = "42"
	require.NoError(t, common.UnmarshalJsonStr(`{"groups":{"g":{"model":["probe"],"key":"k","protocol":"messages","svg_model":"drawing","logic_model":"reasoning","svg_test":true,"logic_test":true,"active":true}}}`, &c))
	c.Normalize()
	require.NoError(t, Validate(c))
	for _, mutate := range []func(*Group){func(g *Group) { g.Protocol = "responses|messages" }, func(g *Group) { g.SVGModel = "" }, func(g *Group) { g.LogicModel = "" }} {
		g := c.Groups["g"]
		backup := g
		mutate(&g)
		c.Groups["g"] = g
		require.Error(t, Validate(c))
		c.Groups["g"] = backup
	}
}

func TestMonitorIgnoresLegacyOutputLimit(t *testing.T) {
	cfg := Default()
	require.NoError(t, common.UnmarshalJsonStr(`{"max_output_tokens":4096,"svg_max_output_tokens":16384}`, &cfg))
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "max_output_tokens")
}

func TestMonitorHTMLConfigMigration(t *testing.T) {
	cfg := Default()
	require.NoError(t, common.UnmarshalJsonStr(`{"screenshot_url":"http://old/screenshot","logic_match_mode":"","svg_output_dir":""}`, &cfg))
	cfg.Normalize()
	require.Equal(t, DefaultSVGOutputDir, cfg.SVGOutputDir)
	require.Equal(t, "exact", cfg.LogicMatchMode)
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "screenshot_url")
	cfg.LogicMatchMode = "contains"
	require.NoError(t, Validate(cfg))
	cfg.LogicMatchMode = "regex"
	require.Error(t, Validate(cfg))
	cfg.LogicMatchMode = "exact"
	cfg.SVGOutputDir = "bad\x00path"
	require.Error(t, Validate(cfg))
}

func TestMonitorRetentionAndPublicDomainConfig(t *testing.T) {
	cfg := Default()
	require.NoError(t, common.UnmarshalJsonStr(`{"history_keep":0,"history_days":0}`, &cfg))
	cfg.Normalize()
	require.Equal(t, 48, cfg.HistoryKeep)
	require.Equal(t, 8, cfg.HistoryDays)
	for _, address := range []string{"https://preview.example.com", "https://cdn.example.com/assets"} {
		cfg.SVGPublicBaseURL = address
		require.NoError(t, Validate(cfg))
	}
	for _, address := range []string{"http://cdn.example.com", "https://user:pass@cdn.example.com", "https://cdn.example.com?signature=abc"} {
		cfg.SVGPublicBaseURL = address
		require.Error(t, Validate(cfg))
	}
	cfg.SVGPublicBaseURL = ""
	cfg.HistoryKeep = 1001
	require.Error(t, Validate(cfg))
	cfg.HistoryKeep, cfg.HistoryDays = 48, 91
	require.Error(t, Validate(cfg))
}
