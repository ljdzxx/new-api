package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/groupmonitor"
	monitorconfig "github.com/QuantumNous/new-api/setting/group_monitor"
	"github.com/go-redis/redis/v8"
)

const monitorRecordTTL = 8 * 24 * time.Hour

func monitorLogicMatches(answer, expected, mode string) bool {
	answer, expected = strings.TrimSpace(answer), strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	if mode == "contains" {
		return strings.Contains(answer, expected)
	}
	return answer == expected
}

func monitorSummary(result MonitorResult) MonitorResult {
	result.Prompt, result.Answer, result.Expected = "", "", ""
	return result
}

// Lists contain small summaries; full replies are fetched only when selected.
func saveMonitorTestResult(ctx context.Context, group string, result MonitorResult, cfg monitorconfig.Config) error {
	raw, err := common.Marshal(result)
	if err != nil {
		return err
	}
	summary, err := common.Marshal(monitorSummary(result))
	if err != nil {
		return err
	}
	ttl := time.Duration(cfg.HistoryDays) * 24 * time.Hour
	historyKey := groupmonitor.Key(group, "", result.Kind+":history")
	_, err = common.RDB.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, groupmonitor.Key(group, result.ID, result.Kind+":record"), raw, ttl)
		p.Set(ctx, groupmonitor.Key(group, result.Model, result.Kind+":latest"), summary, ttl)
		p.ZAdd(ctx, historyKey, &redis.Z{Score: float64(result.At), Member: string(summary)})
		p.Expire(ctx, historyKey, ttl)
		return nil
	})
	if err != nil {
		return err
	}
	return trimMonitorHistory(ctx, group, result.Kind, cfg)
}

func GetMonitorHistory(ctx context.Context, group, kind string, cfg monitorconfig.Config) ([]MonitorResult, error) {
	if err := trimMonitorHistory(ctx, group, kind, cfg); err != nil {
		return nil, err
	}
	key := groupmonitor.Key(group, "", kind+":history")
	values, err := common.RDB.ZRangeByScore(ctx, key, &redis.ZRangeBy{Min: "(" + strconv.FormatInt(time.Now().Add(-time.Duration(cfg.HistoryDays)*24*time.Hour).UnixMilli(), 10), Max: "+inf"}).Result()
	if err != nil {
		return nil, err
	}
	results := make([]MonitorResult, 0, len(values))
	for _, raw := range values {
		var result MonitorResult
		if err := common.UnmarshalJsonStr(raw, &result); err != nil {
			return nil, err
		}
		results = append(results, monitorSummary(result))
	}
	return results, nil
}

func GetMonitorRecord(ctx context.Context, group, kind, id string, cfg monitorconfig.Config) (*MonitorResult, error) {
	if err := trimMonitorHistory(ctx, group, kind, cfg); err != nil {
		return nil, err
	}
	raw, err := common.RDB.Get(ctx, groupmonitor.Key(group, id, kind+":record")).Result()
	if err != nil {
		return nil, err
	}
	var result MonitorResult
	if err := common.UnmarshalJsonStr(raw, &result); err != nil {
		return nil, err
	}
	if result.At <= time.Now().Add(-time.Duration(cfg.HistoryDays)*24*time.Hour).UnixMilli() {
		return nil, redis.Nil
	}
	return &result, nil
}

// Atomically trim by both age and count, then remove the corresponding replies.
var trimMonitorHistoryScript = redis.NewScript(`
local expired = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
local excess = redis.call('ZRANGE', KEYS[1], 0, -tonumber(ARGV[2])-1)
redis.call('ZREMRANGEBYRANK', KEYS[1], 0, -tonumber(ARGV[2])-1)
for _, value in ipairs(excess) do table.insert(expired, value) end
return expired
`)

func trimMonitorHistory(ctx context.Context, group, kind string, cfg monitorconfig.Config) error {
	cutoff := time.Now().Add(-time.Duration(cfg.HistoryDays) * 24 * time.Hour).UnixMilli()
	removed, err := trimMonitorHistoryScript.Run(ctx, common.RDB, []string{groupmonitor.Key(group, "", kind+":history")}, cutoff, cfg.HistoryKeep).StringSlice()
	if err != nil {
		return err
	}
	keys := []string{}
	for _, raw := range removed {
		var result MonitorResult
		if common.UnmarshalJsonStr(raw, &result) == nil {
			keys = append(keys, groupmonitor.Key(group, result.ID, kind+":record"))
		}
	}
	if len(keys) != 0 {
		return common.RDB.Del(ctx, keys...).Err()
	}
	return nil
}
