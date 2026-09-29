package groupmonitor

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestMonitorWindowRetentionAndWeighting(t *testing.T) {
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	defer client.Close()
	old, enabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RDB, common.RedisEnabled = old, enabled })
	ctx := context.Background()
	now := time.Now()
	fast, slow, cacheA, cacheB, zero := 100.0, 700.0, .5, 1.0, 0.0
	samples := []Sample{
		{ID: "expired", At: now.Add(-3 * time.Hour).UnixMilli(), Success: true, TTFT: &slow},
		{ID: "outside-window", At: now.Add(-61 * time.Minute).UnixMilli(), Success: true, TTFT: &slow},
		{ID: "a", At: now.Add(-time.Minute).UnixMilli(), Success: true, TTFT: &fast, Cache: &cacheA},
		{ID: "b", At: now.Add(-time.Minute).UnixMilli(), Success: false, TTFT: &slow, Cache: &cacheB},
		{ID: "c", At: now.UnixMilli(), Success: true, Cache: &zero},
	}
	for _, s := range samples {
		require.NoError(t, Write(ctx, "g", "m", "requests", s))
	}
	got, err := Read(ctx, "g", "m", "requests", now.Add(-time.Hour).UnixMilli(), now.UnixMilli())
	require.NoError(t, err)
	m := Aggregate(got)
	require.Equal(t, 3, m.Total)
	require.Equal(t, 2, m.Successes)
	require.Equal(t, 400.0, *m.TTFT)
	require.Equal(t, .75, *m.Cache)
	require.InDelta(t, 2.0/3, *m.SuccessRate, .00001)
	require.Equal(t, int64(4), client.ZCard(ctx, Key("g", "m", "requests")).Val())
	for _, key := range r.Keys() {
		require.Positive(t, r.TTL(key), key)
	}
	trend, err := Trend(ctx, "g", []string{"m"}, now, 1)
	require.NoError(t, err)
	require.Len(t, trend, 1)
	require.Equal(t, 400.0, trend[0].TTFT)
	r.FastForward(26 * time.Hour)
	require.Empty(t, r.Keys())
}

func TestMonitorEmptyMetrics(t *testing.T) {
	m := Aggregate(nil)
	require.Nil(t, m.TTFT)
	require.Nil(t, m.Cache)
	require.Nil(t, m.SuccessRate)
}
