package group_monitor

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"testing"
)

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
