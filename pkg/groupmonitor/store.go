// Package groupmonitor stores monitoring samples exclusively in expiring Redis keys.
package groupmonitor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

const SuccessKey = "group_monitor_success"
const CacheKey = "group_monitor_cache"
const ModelKey = "group_monitor_model"
const GroupKey = "group_monitor_group"

func Ready() bool { return common.RedisEnabled && common.RDB != nil }
func Key(group, model, kind string) string {
	h := sha256.Sum256([]byte(group + "\x00" + model))
	return fmt.Sprintf("group-monitor:v1:%x:%s", h[:16], kind)
}

type Sample struct {
	ID      string   `json:"id"`
	At      int64    `json:"at"`
	Success bool     `json:"success"`
	TTFT    *float64 `json:"ttft,omitempty"`
	Cache   *float64 `json:"cache,omitempty"`
}

var writeSample = redis.NewScript(`
local added = redis.call('ZADD', KEYS[1], ARGV[1], ARGV[2])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[3])
redis.call('EXPIRE', KEYS[1], ARGV[4])
if added == 1 and ARGV[5] ~= '' then
  redis.call('HINCRBYFLOAT', KEYS[2], 'sum', ARGV[5])
  redis.call('HINCRBY', KEYS[2], 'count', 1)
  redis.call('EXPIRE', KEYS[2], 90000)
end
return 1`)

func Write(ctx context.Context, group, model, kind string, s Sample) error {
	if !Ready() {
		return nil
	}
	b, err := common.Marshal(s)
	if err != nil {
		return err
	}
	retention := 2 * time.Hour
	if kind == "probe" {
		retention = 25 * time.Hour
	}
	// Store the sample and its trend contribution atomically, including TTLs.
	var ttft string
	if kind == "requests" && s.TTFT != nil {
		ttft = strconv.FormatFloat(*s.TTFT, 'f', -1, 64)
	}
	bucket := Key(group, model, "minute:"+strconv.FormatInt(s.At/60000, 10))
	return writeSample.Run(ctx, common.RDB, []string{Key(group, model, kind), bucket}, s.At, string(b), time.Now().Add(-retention).UnixMilli(), int(retention.Seconds()), ttft).Err()
}

func Read(ctx context.Context, group, model, kind string, since, until int64) ([]Sample, error) {
	values, err := common.RDB.ZRangeByScore(ctx, Key(group, model, kind), &redis.ZRangeBy{Min: strconv.FormatInt(since, 10), Max: strconv.FormatInt(until, 10)}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Sample, 0, len(values))
	for _, raw := range values {
		var s Sample
		if err := common.UnmarshalJsonStr(raw, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

type Metrics struct {
	Total       int      `json:"total"`
	Successes   int      `json:"successes"`
	TTFTCount   int      `json:"ttft_count"`
	CacheCount  int      `json:"cache_count"`
	TTFT        *float64 `json:"ttft"`
	Cache       *float64 `json:"cache"`
	SuccessRate *float64 `json:"success_rate"`
}

func Aggregate(samples []Sample) Metrics {
	m := Metrics{Total: len(samples)}
	var ttft, cache float64
	for _, s := range samples {
		if s.Success {
			m.Successes++
		}
		if s.TTFT != nil {
			ttft += *s.TTFT
			m.TTFTCount++
		}
		if s.Cache != nil && *s.Cache > 0 && *s.Cache <= 1 {
			cache += *s.Cache
			m.CacheCount++
		}
	}
	if m.Total > 0 {
		v := float64(m.Successes) / float64(m.Total)
		m.SuccessRate = &v
	}
	if m.TTFTCount > 0 {
		v := ttft / float64(m.TTFTCount)
		m.TTFT = &v
	}
	if m.CacheCount > 0 {
		v := cache / float64(m.CacheCount)
		m.Cache = &v
	}
	return m
}

type Point struct {
	At    int64   `json:"at"`
	TTFT  float64 `json:"ttft"`
	Count int64   `json:"count"`
}

func Trend(ctx context.Context, group string, models []string, now time.Time, hours int) ([]Point, error) {
	start, end := now.Add(-time.Duration(hours)*time.Hour).Unix()/60, now.Unix()/60
	p := common.RDB.Pipeline()
	cmds := make([]*redis.SliceCmd, 0)
	for minute := start; minute <= end; minute++ {
		for _, m := range models {
			cmds = append(cmds, p.HMGet(ctx, Key(group, m, "minute:"+strconv.FormatInt(minute, 10)), "sum", "count"))
		}
	}
	if _, err := p.Exec(ctx); err != nil {
		return nil, err
	}
	out := []Point{}
	index := 0
	for minute := start; minute <= end; minute++ {
		var sum float64
		var count int64
		for range models {
			vals := cmds[index].Val()
			index++
			if len(vals) == 2 && vals[0] != nil && vals[1] != nil {
				s, _ := strconv.ParseFloat(fmt.Sprint(vals[0]), 64)
				n, _ := strconv.ParseInt(fmt.Sprint(vals[1]), 10, 64)
				sum += s
				count += n
			}
		}
		if count > 0 {
			out = append(out, Point{At: minute * 60000, TTFT: sum / float64(count), Count: count})
		}
	}
	return out, nil
}
