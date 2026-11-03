package group_monitor

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestMonitorTimeoutMigration(t *testing.T) {
	for _, tc := range []struct {
		name, raw         string
		probe, logic, svg int
	}{
		{"defaults", `{}`, 120, 120, 120},
		{"legacy", `{"timeout_seconds":480}`, 480, 480, 480},
		{"partial migration", `{"timeout_seconds":480,"svg_timeout_seconds":3600}`, 480, 480, 3600},
		{"null inherits", `{"timeout_seconds":480,"probe_timeout_seconds":null}`, 480, 480, 480},
		{"independent", `{"probe_timeout_seconds":30,"logic_timeout_seconds":900,"svg_timeout_seconds":7200}`, 30, 900, 7200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			require.NoError(t, common.UnmarshalJsonStr(tc.raw, &cfg))
			cfg.Normalize()
			require.NoError(t, Validate(cfg))
			require.Equal(t, tc.probe, *cfg.ProbeTimeoutSeconds)
			require.Equal(t, tc.logic, *cfg.LogicTimeoutSeconds)
			require.Equal(t, tc.svg, *cfg.SVGTimeoutSeconds)
			raw, err := common.Marshal(cfg)
			require.NoError(t, err)
			require.NotContains(t, string(raw), `"timeout_seconds":`)
			reloaded := Default()
			require.NoError(t, common.Unmarshal(raw, &reloaded))
			reloaded.Normalize()
			reloaded.Normalize()
			for kind, want := range map[string]int{"probe": tc.probe, "logic": tc.logic, "svg": tc.svg} {
				require.Equal(t, time.Duration(want)*time.Second, reloaded.RequestTimeout(kind))
			}
		})
	}
}

func TestMonitorTimeoutValidation(t *testing.T) {
	for _, kind := range []string{"probe", "logic", "svg"} {
		for _, seconds := range []int{-1, 0, 4, 5, 600, 601, 3600, 86400, int(^uint(0) >> 1)} {
			t.Run(fmt.Sprintf("%s/%d", kind, seconds), func(t *testing.T) {
				cfg := Default()
				require.NoError(t, common.UnmarshalJsonStr(fmt.Sprintf(`{"%s_timeout_seconds":%d}`, kind, seconds), &cfg))
				cfg.Normalize()
				if seconds < 5 {
					require.ErrorContains(t, Validate(cfg), kind+"_timeout_seconds")
					return
				}
				require.NoError(t, Validate(cfg))
				timeout := cfg.RequestTimeout(kind)
				require.Positive(t, timeout)
				require.Greater(t, timeout+3*time.Minute, timeout)
				require.Greater(t, timeout+4*time.Minute, timeout+3*time.Minute)
			})
		}
		for _, raw := range []string{"1.5", `"3600"`} {
			cfg := Default()
			require.Error(t, common.UnmarshalJsonStr(fmt.Sprintf(`{"%s_timeout_seconds":%s}`, kind, raw), &cfg))
		}
	}
}
