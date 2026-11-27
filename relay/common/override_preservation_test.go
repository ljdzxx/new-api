package common

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOverridePreservesOpaqueJSON(t *testing.T) {
	const schema = `{"properties":{"z":{"const":9007199254740993},"a":{"type":"string"}}}`
	const body = `{"tools":[{"input_schema":` + schema + `}],"values":[9007199254740993,18446744073709551615,1e-25,false,null,"a\nb"]}`
	for _, mode := range []string{"copy", "move", "append", "prepend"} {
		t.Run(mode, func(t *testing.T) {
			for _, source := range []string{"tools", "values"} {
				op := map[string]any{"mode": mode, "from": source, "to": "backup"}
				path := "backup"
				if mode == "append" || mode == "prepend" {
					op = map[string]any{"mode": mode, "path": source, "value": []any{"added"}}
					path = source
				}
				out, err := ApplyParamOverride([]byte(body), map[string]any{"operations": []any{op}}, nil)
				require.NoError(t, err)
				items := gjson.GetBytes(out, path).Array()
				if mode == "prepend" {
					require.Equal(t, "added", items[0].String())
					items = items[1:]
				} else if mode == "append" {
					require.Equal(t, "added", items[len(items)-1].String())
					items = items[:len(items)-1]
				}
				original := gjson.Get(body, source).Array()
				require.Len(t, items, len(original))
				for i := range original {
					require.Equal(t, original[i].Raw, items[i].Raw)
				}
				if mode == "move" {
					require.False(t, gjson.GetBytes(out, source).Exists())
				}
			}
		})
	}
}

func TestOverrideCopiesScalarJSONWithoutChangingTypes(t *testing.T) {
	for _, value := range []string{`null`, `false`, `0`, `9007199254740993`, `1e-25`, `"a\nb"`} {
		for _, mode := range []string{"copy", "move"} {
			out, err := ApplyParamOverride([]byte(`{"source":`+value+`}`), map[string]any{"operations": []any{map[string]any{"mode": mode, "from": "source", "to": "target"}}}, nil)
			require.NoError(t, err)
			require.Equal(t, value, gjson.GetBytes(out, "target").Raw)
		}
	}
}

func TestOverrideCopyComputedArrayLength(t *testing.T) {
	out, err := ApplyParamOverride([]byte(`{"items":[1,2,3]}`), map[string]any{"operations": []any{map[string]any{"mode": "copy", "from": "items.#", "to": "count"}}}, nil)
	require.NoError(t, err)
	require.True(t, gjson.ValidBytes(out))
	require.EqualValues(t, 3, gjson.GetBytes(out, "count").Int())
}
