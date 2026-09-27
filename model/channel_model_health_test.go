package model

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func setupModelHealth(t *testing.T, redisMode bool) *miniredis.Miniredis {
	t.Helper()
	original := *operation_setting.GetMonitorSetting()
	rdb, enabled := common.RDB, common.RedisEnabled
	setting := operation_setting.GetMonitorSetting()
	setting.ModelHealthEnabled, setting.ModelHealthStatusCodes, setting.ModelHealthThreshold = true, "429,502,503", 2
	modelHealthMemory.Lock()
	modelHealthMemory.states = make(map[string]ChannelModelHealth)
	modelHealthMemory.Unlock()
	common.RedisEnabled, common.RDB = false, nil
	var server *miniredis.Miniredis
	if redisMode {
		server = miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
		common.RDB, common.RedisEnabled = client, true
		t.Cleanup(func() { client.Close() })
	}
	t.Cleanup(func() {
		*operation_setting.GetMonitorSetting() = original
		common.RDB, common.RedisEnabled = rdb, enabled
	})
	return server
}

func TestChannelModelHealthTransitions(t *testing.T) {
	for _, redisMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("redis=%v", redisMode), func(t *testing.T) {
			setupModelHealth(t, redisMode)
			ch := &Channel{Id: 11, Type: constant.ChannelTypeOpenAI, Models: "a,b"}
			record := func(code int) {
				a, err := BeginModelHealthAttempt(ch, "a", false)
				require.NoError(t, err)
				require.NoError(t, a.Record(code))
			}
			for _, code := range []int{429, 502, 200, 502, 400, 429} {
				record(code)
			}
			allowed, err := FilterModelHealthChannels([]*Channel{ch}, "a")
			require.NoError(t, err)
			require.Len(t, allowed, 1)
			late, err := BeginModelHealthAttempt(ch, "a", false)
			require.NoError(t, err)
			record(429)
			_, err = BeginModelHealthAttempt(ch, "a", false)
			require.ErrorIs(t, err, ErrModelUnavailable)
			allowed, err = FilterModelHealthChannels([]*Channel{ch}, "b")
			require.NoError(t, err)
			require.Len(t, allowed, 1)
			other := &Channel{Id: 12, Type: constant.ChannelTypeOpenAI, Models: "a"}
			allowed, err = FilterModelHealthChannels([]*Channel{ch, other}, "a")
			require.NoError(t, err)
			require.Equal(t, []*Channel{other}, allowed)
			require.NoError(t, late.Record(200)) // late success must not unlock
			_, err = BeginModelHealthAttempt(ch, "a", false)
			require.ErrorIs(t, err, ErrModelUnavailable)
			stale, err := BeginModelHealthAttempt(ch, "a", true)
			require.NoError(t, err)
			restore, err := BeginModelHealthAttempt(ch, "a", true)
			require.NoError(t, err)
			require.NoError(t, restore.Recover(99))
			require.NoError(t, stale.Record(502))
			require.Error(t, stale.Recover(98))
			FillChannelModelHealth([]*Channel{ch})
			require.True(t, ch.ModelHealth.Models["a"].Available)
			require.Equal(t, 0, ch.ModelHealth.Models["a"].Count)
			require.Equal(t, 99, ch.ModelHealth.Models["a"].RecoveredBy)
			// New policy clears a partial streak, but never an already blocked state.
			record(502)
			operation_setting.GetMonitorSetting().ModelHealthThreshold = 3
			require.NoError(t, PublishModelHealthOption("monitor_setting.model_health_threshold", "3"))
			record(502)
			FillChannelModelHealth([]*Channel{ch})
			require.Equal(t, 1, ch.ModelHealth.Models["a"].Count)
			record(502)
			record(502)
			operation_setting.GetMonitorSetting().ModelHealthThreshold = 10
			require.NoError(t, PublishModelHealthOption("monitor_setting.model_health_threshold", "10"))
			_, err = BeginModelHealthAttempt(ch, "a", false)
			require.ErrorIs(t, err, ErrModelUnavailable)
			operation_setting.GetMonitorSetting().ModelHealthEnabled = false
			allowed, err = FilterModelHealthChannels([]*Channel{ch}, "a")
			require.NoError(t, err)
			require.Len(t, allowed, 1)
			operation_setting.GetMonitorSetting().ModelHealthEnabled = true
			_, err = BeginModelHealthAttempt(ch, "a", false)
			require.ErrorIs(t, err, ErrModelUnavailable)
		})
	}
}

func TestChannelModelHealthConcurrencyAndPruning(t *testing.T) {
	for _, redisMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("redis=%v", redisMode), func(t *testing.T) {
			setupModelHealth(t, redisMode)
			operation_setting.GetMonitorSetting().ModelHealthThreshold = 100
			ch := &Channel{Id: 1, Type: constant.ChannelTypeOpenAI, Models: "a,b"}
			attempts := make([]*ModelHealthAttempt, 40)
			for i := range attempts {
				var err error
				attempts[i], err = BeginModelHealthAttempt(ch, "a", false)
				require.NoError(t, err)
			}
			var wg sync.WaitGroup
			for _, a := range attempts {
				wg.Add(1)
				go func(a *ModelHealthAttempt) {
					defer wg.Done()
					if err := a.Record(503); err != nil {
						t.Error(err)
					}
					if err := a.Record(503); err != nil {
						t.Error(err)
					}
				}(a)
			}
			wg.Wait()
			FillChannelModelHealth([]*Channel{ch})
			require.Equal(t, 40, ch.ModelHealth.Models["a"].Count)
			if redisMode {
				// Simulate redis retrying an identical script after a lost response.
				a := attempts[0]
				_, err := mutateModelHealth("record", a.Key, a.Epoch, a.ID, ChannelModelHealth{Code: 503, Policy: a.Policy, Revision: a.Revision}, true, 100)
				require.NoError(t, err)
				FillChannelModelHealth([]*Channel{ch})
				require.Equal(t, 40, ch.ModelHealth.Models["a"].Count)
				ttl, err := common.RDB.TTL(context.Background(), a.Key).Result()
				require.NoError(t, err)
				require.Less(t, int64(ttl), int64(0))
			}
			late, err := BeginModelHealthAttempt(ch, "a", false)
			require.NoError(t, err)
			require.NoError(t, PruneChannelModelHealth(ch.Id, []string{"b"}))
			require.NoError(t, late.Record(503))
			FillChannelModelHealth([]*Channel{ch})
			require.False(t, ch.ModelHealth.Models["a"].Observed)
		})
	}
}

func TestChannelModelHealthScopeAndRedisFailure(t *testing.T) {
	server := setupModelHealth(t, true)
	for _, typ := range []int{constant.ChannelTypeKling, constant.ChannelTypeSunoAPI, constant.ChannelTypeSora} {
		a, err := BeginModelHealthAttempt(&Channel{Id: 1, Type: typ, Models: "a"}, "a", false)
		require.NoError(t, err)
		require.Nil(t, a)
	}
	for _, path := range []string{"/v1/videos", "/suno/submit/music", "/mj/submit/imagine", "/v1/realtime"} {
		require.False(t, ModelHealthRequestSupported(path))
	}
	require.True(t, ModelHealthRequestSupported("/v1/responses"))
	a, err := BeginModelHealthAttempt(&Channel{Id: 1, Type: 1, Models: "a"}, "not-configured", false)
	require.NoError(t, err)
	require.Nil(t, a)
	server.Close()
	_, err = BeginModelHealthAttempt(&Channel{Id: 1, Type: 1, Models: "a"}, "a", false)
	require.ErrorIs(t, err, ErrModelHealthStorage)
}

func TestChannelModelHealthSharedPolicy(t *testing.T) {
	setupModelHealth(t, true)
	ch := &Channel{Id: 100, Type: 1, Models: "a"}
	first, err := BeginModelHealthAttempt(ch, "a", false)
	require.NoError(t, err)
	require.NoError(t, first.Record(502))
	// A different node publishes a new threshold, while this node still has 2.
	require.NoError(t, PublishModelHealthOption("monitor_setting.model_health_threshold", "3"))
	for i := 0; i < 2; i++ {
		a, err := BeginModelHealthAttempt(ch, "a", false)
		require.NoError(t, err)
		require.NoError(t, a.Record(502))
	}
	FillChannelModelHealth([]*Channel{ch})
	require.Equal(t, 3, ch.ModelHealth.Threshold)
	require.Equal(t, 2, ch.ModelHealth.Models["a"].Count)
	_, err = mutateModelHealth("begin", first.Key, "", "", ChannelModelHealth{Available: true, Epoch: "stale", Policy: first.Policy, Revision: first.Revision}, false, 2)
	require.NoError(t, err)
	FillChannelModelHealth([]*Channel{ch})
	require.Equal(t, 2, ch.ModelHealth.Models["a"].Count)
	require.True(t, ch.ModelHealth.Models["a"].Available)
	a, err := BeginModelHealthAttempt(ch, "a", false)
	require.NoError(t, err)
	require.NoError(t, a.Record(502))
	_, err = BeginModelHealthAttempt(ch, "a", false)
	require.ErrorIs(t, err, ErrModelUnavailable)
	require.NoError(t, PublishModelHealthOption("monitor_setting.model_health_enabled", "false"))
	allowed, err := FilterModelHealthChannels([]*Channel{ch}, "a")
	require.NoError(t, err)
	require.Len(t, allowed, 1)
}
