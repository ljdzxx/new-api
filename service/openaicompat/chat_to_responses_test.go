package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChatCompletionsRequestToResponsesRequestServiceTier(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "absent"},
		{name: "null", raw: `null`},
		{name: "fast", raw: `"fast"`, want: "fast"},
		{name: "priority", raw: `"priority"`, want: "priority"},
		{name: "auto", raw: `"auto"`, want: "auto"},
		{name: "default", raw: `"default"`, want: "default"},
		{name: "flex", raw: `"flex"`, want: "flex"},
		{name: "number", raw: `1`, wantErr: true},
		{name: "boolean", raw: `false`, wantErr: true},
		{name: "object", raw: `{}`, wantErr: true},
		{name: "array", raw: `[]`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"model":"test-model","messages":[{"role":"user","content":"hello"}]`
			if tc.raw != "" {
				body += `,"service_tier":` + tc.raw
			}
			body += `}`
			var req dto.GeneralOpenAIRequest
			require.NoError(t, common.UnmarshalJsonStr(body, &req))

			out, err := ChatCompletionsRequestToResponsesRequest(&req)
			if tc.wantErr {
				require.ErrorContains(t, err, "service_tier")
				require.Nil(t, out)
				return
			}
			require.NoError(t, err)

			encoded, err := common.Marshal(out)
			require.NoError(t, err)
			var upstream map[string]any
			require.NoError(t, common.Unmarshal(encoded, &upstream))
			if tc.want == "" {
				require.NotContains(t, upstream, "service_tier")
			} else {
				require.Equal(t, tc.want, upstream["service_tier"])
			}
		})
	}
}
