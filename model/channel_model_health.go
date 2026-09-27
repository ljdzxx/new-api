package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/go-redis/redis/v8"
)

var ErrModelUnavailable = errors.New("该渠道的模型已暂停使用，请管理员测试通过后恢复")
var ErrModelHealthStorage = errors.New("模型可用状态检查失败，请稍后重试")

// Runtime state only: never migrate or persist this type with GORM.
type ChannelModelHealth struct {
	Available   bool   `json:"available"`
	Observed    bool   `json:"observed"`
	Code        int    `json:"code"`
	Count       int    `json:"count"`
	BlockedAt   int64  `json:"blocked_at"`
	UpdatedAt   int64  `json:"updated_at"`
	RecoveredAt int64  `json:"recovered_at"`
	RecoveredBy int    `json:"recovered_by"`
	Epoch       string `json:"epoch"`
	Policy      string `json:"policy"`
	Revision    int64  `json:"revision"`
}

type ModelHealthAttempt struct {
	Revision  int64
	Key       string
	Epoch     string
	Policy    string
	ID        string
	Codes     map[int]bool
	Threshold int
	once      sync.Once
}

var modelHealthMemory = struct {
	sync.Mutex
	states map[string]ChannelModelHealth
}{states: make(map[string]ChannelModelHealth)}

func ModelHealthChannelSupported(channelType int) bool {
	switch channelType {
	case constant.ChannelTypeMidjourney, constant.ChannelTypeMidjourneyPlus,
		constant.ChannelTypeSunoAPI, constant.ChannelTypeKling, constant.ChannelTypeJimeng,
		constant.ChannelTypeDoubaoVideo, constant.ChannelTypeVidu, constant.ChannelTypeSora:
		return false
	}
	return true
}

// Positive endpoint list: video, task submission/polling and realtime are excluded.
func ModelHealthRequestSupported(path string) bool {
	switch path {
	case "/v1/chat/completions", "/pg/chat/completions", "/v1/completions", "/v1/messages",
		"/v1/responses", "/v1/responses/compact", "/v1/embeddings", "/v1/rerank",
		"/v1/images/generations", "/v1/images/edits", "/v1/edits", "/v1/moderations",
		"/v1/audio/speech", "/v1/audio/transcriptions", "/v1/audio/translations":
		return true
	}
	return (strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/")) &&
		(strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent") || strings.HasSuffix(path, ":embedContent"))
}

// Resolve to the configured routing name, before provider model mapping.
func ChannelModelHealthName(ch *Channel, name string) string {
	name = strings.TrimSpace(name)
	normalized := ratio_setting.FormatMatchingModelName(name)
	for _, configured := range ch.GetModels() {
		if strings.TrimSpace(configured) == name {
			return name
		}
	}
	for _, configured := range ch.GetModels() {
		if strings.TrimSpace(configured) == normalized {
			return normalized
		}
	}
	return ""
}

func modelHealthKey(channelID int, name string) string {
	return fmt.Sprintf("new-api:model_health:v1:{%d}:%x", channelID, sha256.Sum256([]byte(name)))
}

func modelHealthRedis() bool { return common.RedisEnabled && common.RDB != nil }

func modelHealthError(err error) error {
	common.SysError("model health cache failed: " + err.Error())
	return ErrModelHealthStorage
}

// Every mutation is atomic. Receipts prevent a Redis transport retry from counting
// one attempt twice. State has no TTL; only short-lived deduplication receipts expire.
var modelHealthScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
local op = ARGV[1]
local initial = cjson.decode(ARGV[2])
local state = raw and cjson.decode(raw) or initial
if op == 'begin' then
  if tonumber(redis.call('HGET', KEYS[3], 'revision') or '0') ~= initial.revision then return '' end
  if state.available and (state.policy ~= initial.policy or (state.revision or 0) ~= initial.revision) then
    state.epoch = initial.epoch
    state.policy = initial.policy
    state.revision = initial.revision
    state.code = 0
    state.count = 0
  end
  redis.call('SET', KEYS[1], cjson.encode(state))
elseif op == 'recover' then
  if raw and state.epoch == initial.epoch then return raw end
  if not raw or state.epoch ~= ARGV[3] then return '' end
  state = initial
  redis.call('SET', KEYS[1], cjson.encode(state))
elseif op == 'record' then
  if redis.call('HGET', KEYS[3], 'enabled') ~= 'true' or tonumber(redis.call('HGET', KEYS[3], 'revision') or '0') ~= initial.revision then return '' end
  if not raw or state.epoch ~= ARGV[3] or state.policy ~= initial.policy or not state.available then return '' end
  if not redis.call('SET', KEYS[2], '1', 'NX', 'EX', 300) then return '' end
  state.observed = true
  state.updated_at = initial.updated_at
  if tonumber(ARGV[4]) == 1 then
    if state.code == initial.code then state.count = state.count + 1 else state.count = 1 end
    state.code = initial.code
    if state.count >= tonumber(ARGV[5]) then
      state.available = false
      state.blocked_at = initial.updated_at
    end
  else
    state.code = 0
    state.count = 0
  end
  redis.call('SET', KEYS[1], cjson.encode(state))
end
return cjson.encode(state)
`)

func mutateModelHealth(op, key, expectedEpoch, receipt string, initial ChannelModelHealth, hit bool, threshold int) (*ChannelModelHealth, error) {
	if modelHealthRedis() {
		payload, err := common.Marshal(initial)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		matched := 0
		if hit {
			matched = 1
		}
		raw, err := modelHealthScript.Run(ctx, common.RDB, []string{key, key + ":receipt:" + receipt, modelHealthConfigKey}, op, string(payload), expectedEpoch, matched, threshold).Text()
		if err != nil {
			return nil, modelHealthError(err)
		}
		if raw == "" {
			return nil, nil
		}
		var state ChannelModelHealth
		if err = common.UnmarshalJsonStr(raw, &state); err != nil {
			return nil, modelHealthError(err)
		}
		return &state, nil
	}
	modelHealthMemory.Lock()
	defer modelHealthMemory.Unlock()
	state, exists := modelHealthMemory.states[key]
	switch op {
	case "begin":
		if initial.Revision != modelHealthMemoryRevision.Load() {
			return nil, nil
		}
		if !exists {
			state = initial
		}
		if state.Available && (state.Policy != initial.Policy || state.Revision != initial.Revision) {
			state.Epoch, state.Policy, state.Code, state.Count = initial.Epoch, initial.Policy, 0, 0
			state.Revision = initial.Revision
		}
	case "recover":
		if !exists || state.Epoch != expectedEpoch {
			return nil, nil
		}
		state = initial
	case "record":
		if initial.Revision != modelHealthMemoryRevision.Load() {
			return nil, nil
		}
		if !exists || state.Epoch != expectedEpoch || state.Policy != initial.Policy || !state.Available {
			return nil, nil
		}
		state.Observed, state.UpdatedAt = true, initial.UpdatedAt
		if hit {
			if state.Code == initial.Code {
				state.Count++
			} else {
				state.Count = 1
			}
			state.Code = initial.Code
			if state.Count >= threshold {
				state.Available, state.BlockedAt = false, initial.UpdatedAt
			}
		} else {
			state.Code, state.Count = 0, 0
		}
	}
	modelHealthMemory.states[key] = state
	return &state, nil
}

func BeginModelHealthAttempt(ch *Channel, name string, testing bool) (*ModelHealthAttempt, error) {
	if (!testing && !operation_setting.GetModelHealthSettings().Enabled) || !ModelHealthChannelSupported(ch.Type) {
		return nil, nil
	}
	name = ChannelModelHealthName(ch, name)
	if name == "" {
		return nil, nil
	}
	config, err := loadModelHealthConfig()
	if err != nil {
		return nil, err
	}
	if !testing && !config.Enabled {
		return nil, nil
	}
	policy, codes, threshold := config.Policy, config.Codes, config.Threshold
	key := modelHealthKey(ch.Id, name)
	state, err := mutateModelHealth("begin", key, "", "", ChannelModelHealth{Available: true, Epoch: common.GetUUID(), Policy: policy, Revision: config.Revision}, false, threshold)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, modelHealthError(errors.New("monitoring rules changed during selection"))
	}
	if !testing && !state.Available {
		return nil, ErrModelUnavailable
	}
	return &ModelHealthAttempt{Revision: config.Revision, Key: key, Epoch: state.Epoch, Policy: policy, ID: common.GetUUID(), Codes: codes, Threshold: threshold}, nil
}

func (attempt *ModelHealthAttempt) Record(code int) error {
	if attempt == nil || code < 100 || code > 599 || !operation_setting.GetModelHealthSettings().Enabled {
		return nil
	}
	config, configErr := loadModelHealthConfig()
	if configErr != nil {
		return configErr
	}
	policy := config.Policy
	if !config.Enabled || policy != attempt.Policy || config.Revision != attempt.Revision {
		return nil
	}
	var err error
	attempt.once.Do(func() {
		_, err = mutateModelHealth("record", attempt.Key, attempt.Epoch, attempt.ID,
			ChannelModelHealth{Code: code, Policy: policy, Revision: config.Revision, UpdatedAt: time.Now().Unix()}, attempt.Codes[code], attempt.Threshold)
	})
	return err
}

func (attempt *ModelHealthAttempt) Recover(adminID int) error {
	if attempt == nil {
		return nil
	}
	config, err := loadModelHealthConfig()
	if err != nil {
		return err
	}
	policy := config.Policy
	state, err := mutateModelHealth("recover", attempt.Key, attempt.Epoch, "",
		ChannelModelHealth{Available: true, Observed: true, Epoch: common.GetUUID(), Policy: policy, Revision: config.Revision,
			UpdatedAt: time.Now().Unix(), RecoveredAt: time.Now().Unix(), RecoveredBy: adminID}, false, 0)
	if err != nil {
		return err
	}
	if state == nil {
		return errors.New("模型状态已变化，请重新测试恢复")
	}
	return nil
}

func readModelHealth(keys []string) (map[string]ChannelModelHealth, error) {
	states := make(map[string]ChannelModelHealth, len(keys))
	if len(keys) == 0 {
		return states, nil
	}
	if modelHealthRedis() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		values, err := common.RDB.MGet(ctx, keys...).Result()
		if err != nil {
			return nil, modelHealthError(err)
		}
		for i, value := range values {
			if value == nil {
				continue
			}
			var state ChannelModelHealth
			raw, ok := value.(string)
			if !ok {
				return nil, modelHealthError(errors.New("invalid state type"))
			}
			if err := common.UnmarshalJsonStr(raw, &state); err != nil {
				return nil, modelHealthError(err)
			}
			states[keys[i]] = state
		}
	} else {
		modelHealthMemory.Lock()
		defer modelHealthMemory.Unlock()
		for _, key := range keys {
			if state, ok := modelHealthMemory.states[key]; ok {
				states[key] = state
			}
		}
	}
	return states, nil
}

func FilterModelHealthChannels(channels []*Channel, name string) ([]*Channel, error) {
	if !operation_setting.GetModelHealthSettings().Enabled {
		return channels, nil
	}
	config, err := loadModelHealthConfig()
	if err != nil {
		return nil, err
	}
	if !config.Enabled {
		return channels, nil
	}
	keys := make([]string, 0, len(channels))
	for _, ch := range channels {
		if ModelHealthChannelSupported(ch.Type) {
			if model := ChannelModelHealthName(ch, name); model != "" {
				keys = append(keys, modelHealthKey(ch.Id, model))
			}
		}
	}
	states, err := readModelHealth(keys)
	if err != nil {
		return nil, err
	}
	filtered := make([]*Channel, 0, len(channels))
	for _, ch := range channels {
		state, found := states[modelHealthKey(ch.Id, ChannelModelHealthName(ch, name))]
		if !found || state.Available {
			filtered = append(filtered, ch)
		}
	}
	return filtered, nil
}

type ChannelModelHealthView struct {
	Enabled   bool                          `json:"enabled"`
	Supported bool                          `json:"supported"`
	Storage   string                        `json:"storage"`
	Threshold int                           `json:"threshold"`
	ReadAt    int64                         `json:"read_at"`
	Error     string                        `json:"error,omitempty"`
	Models    map[string]ChannelModelHealth `json:"models"`
}

func FillChannelModelHealth(channels []*Channel) {
	keys := make([]string, 0)
	for _, ch := range channels {
		if ModelHealthChannelSupported(ch.Type) {
			for _, name := range ch.GetModels() {
				keys = append(keys, modelHealthKey(ch.Id, strings.TrimSpace(name)))
			}
		}
	}
	states, err := readModelHealth(keys)
	config, configErr := loadModelHealthConfig()
	if err == nil {
		err = configErr
	}
	storage := "memory"
	if modelHealthRedis() {
		storage = "redis"
	}
	for _, ch := range channels {
		view := &ChannelModelHealthView{Enabled: config.Enabled,
			Supported: ModelHealthChannelSupported(ch.Type), Storage: storage, Threshold: config.Threshold,
			ReadAt: time.Now().Unix(), Models: make(map[string]ChannelModelHealth)}
		if err != nil && view.Supported {
			view.Error = ErrModelHealthStorage.Error()
		}
		if view.Supported && err == nil {
			for _, name := range ch.GetModels() {
				name = strings.TrimSpace(name)
				state, found := states[modelHealthKey(ch.Id, name)]
				if !found {
					state.Available = true
				}
				if state.Available && (state.Policy != config.Policy || state.Revision != config.Revision) {
					state.Code, state.Count = 0, 0
				}
				state.Epoch, state.Policy = "", ""
				view.Models[name] = state
			}
		}
		ch.ModelHealth = view
	}
}

// Prune against the saved configuration; completion never recreates deleted keys.
func PruneChannelModelHealth(channelID int, models []string) error {
	prefix := fmt.Sprintf("new-api:model_health:v1:{%d}:", channelID)
	keep := make(map[string]bool)
	for _, name := range models {
		keep[modelHealthKey(channelID, strings.TrimSpace(name))] = true
	}
	if !modelHealthRedis() {
		modelHealthMemory.Lock()
		defer modelHealthMemory.Unlock()
		for key := range modelHealthMemory.states {
			if strings.HasPrefix(key, prefix) && !keep[key] {
				delete(modelHealthMemory.states, key)
			}
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cursor uint64
	for {
		keys, next, err := common.RDB.Scan(ctx, cursor, prefix+"*", 200).Result()
		if err != nil {
			return modelHealthError(err)
		}
		remove := make([]string, 0)
		for _, key := range keys {
			if !keep[key] && !strings.Contains(key, ":receipt:") {
				remove = append(remove, key)
			}
		}
		if len(remove) > 0 {
			if err := common.RDB.Del(ctx, remove...).Err(); err != nil {
				return modelHealthError(err)
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}
