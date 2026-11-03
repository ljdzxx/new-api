package group_monitor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestMonitorGroupEffortRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, raw, logic, svg string
	}{
		{"legacy", `{}`, "", ""},
		{"null", `{"logic_effort":null,"svg_effort":null}`, "", ""},
		{"empty", `{"logic_effort":" ","svg_effort":""}`, "", ""},
		{"independent", `{"logic_effort":"high","svg_effort":"medium"}`, "high", "medium"},
		{"logic only", `{"logic_effort":" low "}`, "low", ""},
		{"svg only", `{"svg_effort":"xhigh"}`, "", "xhigh"},
		{"provider value", `{"svg_effort":"ultra"}`, "", "ultra"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			var g Group
			require.NoError(t, common.UnmarshalJsonStr(tc.raw, &g))
			g.Models, g.Key = []string{"model"}, "test"
			cfg.Groups["g"] = g
			cfg.Normalize()
			require.NoError(t, Validate(cfg))
			raw, err := common.Marshal(cfg)
			require.NoError(t, err)
			reloaded := Default()
			require.NoError(t, common.Unmarshal(raw, &reloaded))
			reloaded.Normalize()
			require.Equal(t, tc.logic, reloaded.Groups["g"].TestEffort("logic"))
			require.Equal(t, tc.svg, reloaded.Groups["g"].TestEffort("svg"))
			require.Empty(t, reloaded.Groups["g"].TestEffort("probe"))
			if tc.logic == "" {
				require.NotContains(t, string(raw), `"logic_effort"`)
			}
			if tc.svg == "" {
				require.NotContains(t, string(raw), `"svg_effort"`)
			}
		})
	}
	for _, field := range []string{"logic_effort", "svg_effort"} {
		cfg := Default()
		var g Group
		require.NoError(t, common.UnmarshalJsonStr(fmt.Sprintf(`{"%s":%q}`, field, strings.Repeat("x", 65)), &g))
		g.Models, g.Key = []string{"model"}, "test"
		cfg.Groups["g"] = g
		cfg.Normalize()
		require.ErrorContains(t, Validate(cfg), field)
		for _, value := range []string{"1", "true", "{}"} {
			require.Error(t, common.UnmarshalJsonStr(fmt.Sprintf(`{"%s":%s}`, field, value), &g))
		}
	}
}
