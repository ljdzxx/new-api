package billing_policy

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestBuildLegacyModelPolicy(t *testing.T) {
	setters := []func(string) error{ratio_setting.UpdateModelPriceByJSONString, ratio_setting.UpdateModelRatioByJSONString, ratio_setting.UpdateCompletionRatioByJSONString, ratio_setting.UpdateCacheRatioByJSONString, ratio_setting.UpdateCreateCacheRatioByJSONString, ratio_setting.UpdateImageRatioByJSONString, ratio_setting.UpdateAudioRatioByJSONString, ratio_setting.UpdateAudioCompletionRatioByJSONString}
	backups := []string{ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString(), ratio_setting.CacheRatio2JSONString(), ratio_setting.CreateCacheRatio2JSONString(), ratio_setting.ImageRatio2JSONString(), ratio_setting.AudioRatio2JSONString(), ratio_setting.AudioCompletionRatio2JSONString()}
	t.Cleanup(func() {
		for i, set := range setters {
			require.NoError(t, set(backups[i]))
		}
	})
	sources := []string{
		`{"fixed":0.25,"free":0,"invalid-price":-1}`,
		`{"fixed":10,"token":1.5,"free-token":0,"bad":-1,"secondary":1,"claude-sync":1,"gpt-4-gizmo-*":2,"*-openai-compact":3}`,
		`{"token":4,"secondary":1,"gpt-4-gizmo-*":2}`,
		`{"token":0.5,"secondary":-0.5}`,
		`{"token":1.25,"claude-sync":1.25}`,
		`{"token":2}`,
		`{"token":3}`,
		`{"token":2}`,
	}
	for i, set := range setters {
		require.NoError(t, set(sources[i]))
	}
	t.Run("fixed price wins", func(t *testing.T) {
		p, e := BuildLegacyModelPolicy("fixed")
		require.NoError(t, e)
		require.Equal(t, "per_request", p.Mode)
		require.Equal(t, "0.25", p.Price)
	})
	t.Run("explicit zero", func(t *testing.T) {
		p, e := BuildLegacyModelPolicy("free")
		require.NoError(t, e)
		require.Equal(t, "0", p.Price)
		p, e = BuildLegacyModelPolicy("free-token")
		require.NoError(t, e)
		require.Equal(t, "0", p.Prices.Input)
		require.Equal(t, "0", p.Prices.Output)
	})
	t.Run("all token dimensions", func(t *testing.T) {
		p, e := BuildLegacyModelPolicy("token")
		require.NoError(t, e)
		require.Equal(t, Prices{Input: "3", Output: "12", CacheRead: "1.5", CacheWrite: "3.75", ImageInput: "6", AudioInput: "9", AudioOutput: "18"}, p.Prices)
		report, e := BuildLegacyMigrationCandidate(StateShadow)
		require.Error(t, e)
		require.Equal(t, report.Candidate.Policies["token"].Prices, p.Prices)
	})
	t.Run("anthropic cache durations", func(t *testing.T) {
		p, e := BuildLegacyModelPolicy("claude-sync")
		require.NoError(t, e)
		require.Equal(t, "2.5", p.Prices.CacheWrite5m)
		require.Equal(t, "4", p.Prices.CacheWrite1h)
	})
	t.Run("legacy matching", func(t *testing.T) {
		p, e := BuildLegacyModelPolicy("gpt-4-gizmo-example")
		require.NoError(t, e)
		require.Equal(t, "4", p.Prices.Input)
		require.Equal(t, "8", p.Prices.Output)
		p, e = BuildLegacyModelPolicy("custom-openai-compact")
		require.NoError(t, e)
		require.Equal(t, "6", p.Prices.Input)
	})
	for _, name := range []string{"missing", "bad", "secondary", "invalid-price"} {
		t.Run(name, func(t *testing.T) { _, err := BuildLegacyModelPolicy(name); require.Error(t, err) })
	}
}
