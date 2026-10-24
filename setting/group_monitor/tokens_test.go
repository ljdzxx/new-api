package group_monitor

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestMonitorTokenBudgetValidation(t *testing.T) {
	for _, field := range []string{"messages_max_tokens", "svg_messages_max_tokens"} {
		for _, value := range []int{-1, 0, 1, 32768, common.MaxTokensLimit, common.MaxTokensLimit + 1} {
			cfg := Default()
			raw, err := common.Marshal(map[string]int{field: value})
			require.NoError(t, err)
			require.NoError(t, common.Unmarshal(raw, &cfg))
			cfg.Normalize()
			err = Validate(cfg)
			if value < 0 || value > common.MaxTokensLimit {
				require.ErrorContains(t, err, field)
			} else {
				require.NoError(t, err)
			}
		}
	}
	for _, raw := range []string{`{}`, `{"messages_max_tokens":null,"svg_messages_max_tokens":null}`, `{"max_output_tokens":123}`} {
		cfg := Default()
		require.NoError(t, common.UnmarshalJsonStr(raw, &cfg))
		cfg.Normalize()
		require.NoError(t, Validate(cfg))
		require.Zero(t, cfg.MessagesMaxTokens)
		require.Zero(t, cfg.SVGMessagesMaxTokens)
	}
	for _, raw := range []string{`{"messages_max_tokens":1.5}`, `{"svg_messages_max_tokens":18446744073686646784}`} {
		cfg := Default()
		require.Error(t, common.UnmarshalJsonStr(raw, &cfg))
	}
}
