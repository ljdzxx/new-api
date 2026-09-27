package model

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const modelHealthConfigKey = "new-api:model_health:config:v1"

var modelHealthMemoryRevision atomic.Int64

type modelHealthConfig struct {
	Revision  int64
	Enabled   bool
	Policy    string
	Codes     map[int]bool
	Threshold int
}

func modelHealthConfigDefaults() map[string]string {
	s := operation_setting.GetModelHealthSettings()
	return map[string]string{
		"enabled":      strconv.FormatBool(s.Enabled),
		"status_codes": s.StatusCodes,
		"threshold":    strconv.Itoa(s.Threshold),
		"revision":     strconv.FormatInt(modelHealthMemoryRevision.Load(), 10),
	}
}

// A shared policy prevents nodes awaiting the normal option sync from repeatedly
// resetting each other's streaks with different thresholds. Configuration itself
// still lives in the existing options table; runtime health never does.
func loadModelHealthConfig() (modelHealthConfig, error) {
	values := modelHealthConfigDefaults()
	if modelHealthRedis() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		stored, err := common.RDB.HGetAll(ctx, modelHealthConfigKey).Result()
		if err != nil {
			return modelHealthConfig{}, modelHealthError(err)
		}
		if len(stored) < 3 {
			pipe := common.RDB.TxPipeline()
			for key, value := range values {
				pipe.HSetNX(ctx, modelHealthConfigKey, key, value)
			}
			result := pipe.HGetAll(ctx, modelHealthConfigKey)
			if _, err := pipe.Exec(ctx); err != nil {
				return modelHealthConfig{}, modelHealthError(err)
			}
			stored = result.Val()
		}
		values = stored
	}
	enabled, err := strconv.ParseBool(values["enabled"])
	if err != nil {
		return modelHealthConfig{}, modelHealthError(err)
	}
	codes, err := operation_setting.ParseModelHealthStatusCodes(values["status_codes"])
	if err != nil {
		return modelHealthConfig{}, modelHealthError(err)
	}
	threshold, err := strconv.Atoi(values["threshold"])
	if err != nil || threshold < 1 || threshold > 1000 {
		return modelHealthConfig{}, modelHealthError(fmt.Errorf("invalid threshold"))
	}
	list := make([]int, 0, len(codes))
	for code := range codes {
		list = append(list, code)
	}
	sort.Ints(list)
	revision, _ := strconv.ParseInt(values["revision"], 10, 64)
	return modelHealthConfig{Revision: revision, Enabled: enabled, Codes: codes, Threshold: threshold, Policy: fmt.Sprintf("%v/%d", list, threshold)}, nil
}

// Only explicit admin changes publish fields; background option syncing must
// never overwrite a newer shared policy with a stale per-node snapshot.
func PublishModelHealthOption(key, value string) error {
	if !strings.HasPrefix(key, "monitor_setting.model_health_") {
		return nil
	}
	field := strings.TrimPrefix(key, "monitor_setting.model_health_")
	if field != "enabled" && field != "status_codes" && field != "threshold" {
		return nil
	}
	if !modelHealthRedis() {
		if field != "enabled" {
			modelHealthMemoryRevision.Add(1)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pipe := common.RDB.TxPipeline()
	for k, v := range modelHealthConfigDefaults() {
		pipe.HSetNX(ctx, modelHealthConfigKey, k, v)
	}
	pipe.HSet(ctx, modelHealthConfigKey, field, value)
	if field != "enabled" {
		pipe.HIncrBy(ctx, modelHealthConfigKey, "revision", 1)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return modelHealthError(err)
	}
	return nil
}
