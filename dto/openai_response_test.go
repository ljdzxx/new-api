package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestInputTokenDetailsUnmarshalCacheWriteTokens(t *testing.T) {
	var details InputTokenDetails

	err := common.Unmarshal([]byte(`{"cached_tokens":10,"cache_write_tokens":25}`), &details)

	require.NoError(t, err)
	require.Equal(t, 10, details.CachedTokens)
	require.Equal(t, 25, details.CacheWriteTokens)
}

func TestInputTokenDetailsUnmarshalLegacyCachedCreationTokens(t *testing.T) {
	var details InputTokenDetails

	err := common.Unmarshal([]byte(`{"cached_tokens":10,"cached_creation_tokens":25}`), &details)

	require.NoError(t, err)
	require.Equal(t, 10, details.CachedTokens)
	require.Equal(t, 25, details.CacheWriteTokens)
}

func TestInputTokenDetailsMarshalUsesCacheWriteTokens(t *testing.T) {
	data, err := common.Marshal(InputTokenDetails{CachedTokens: 10, CacheWriteTokens: 25})

	require.NoError(t, err)
	require.Contains(t, string(data), `"cache_write_tokens":25`)
	require.NotContains(t, string(data), "cached_creation_tokens")
}

func TestInputTokenDetailsClampsNegativeCacheWriteTokens(t *testing.T) {
	var details InputTokenDetails

	err := common.Unmarshal([]byte(`{"cache_write_tokens":-25}`), &details)

	require.NoError(t, err)
	require.Zero(t, details.CacheWriteTokens)
	require.Zero(t, (InputTokenDetails{CacheWriteTokens: -25}).CacheCreationTokensTotal())
}
