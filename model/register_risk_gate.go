package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

const registerRiskRedisTimeout = 3 * time.Second

type RegisterRiskError struct {
	Reason  string
	Message string
}

func (e *RegisterRiskError) Error() string { return e.Message }

func registerRiskUnavailable(err error) error {
	common.SysError("registration risk unavailable: " + err.Error())
	return &RegisterRiskError{Reason: "unavailable", Message: "注册服务暂时不可用，请稍后重试"}
}

// Fixed field order and domain-separated HMAC reuse the invitation profile hashes.
// Account identifiers and one-time tokens must never participate in this key.
func registrationRiskKey(ip string, fp RegistrationFingerprint) (string, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil || fp.Missing {
		return "", fmt.Errorf("missing registration profile")
	}
	parts := []string{fp.CanvasHash, fp.WebGLHash, fp.AudioHash, fp.FontsHash, fp.UAHash, fp.LocaleHash, fp.ScreenHash, fp.HardwareHash}
	available := 0
	emptyHash := sha256.Sum256(nil)
	for i, part := range parts {
		if part == "" && i < 4 {
			continue // Explicitly unavailable optional browser capabilities.
		}
		decoded, err := hex.DecodeString(part)
		if err != nil || len(decoded) != sha256.Size || part != strings.ToLower(part) {
			return "", fmt.Errorf("invalid registration fingerprint component")
		}
		if i < 4 && part != hex.EncodeToString(emptyHash[:]) {
			available++
		}
	}
	expected := sha256.Sum256([]byte(strings.Join(parts, "|")))
	if available == 0 || fp.FingerprintHash != hex.EncodeToString(expected[:]) {
		return "", fmt.Errorf("invalid registration fingerprint")
	}
	profile := BuildUserRegistrationProfile(addr.Unmap().String(), fp)
	values := []string{profile.IPHash, profile.FingerprintHash, profile.CanvasHash, profile.WebGLHash, profile.AudioHash, profile.FontsHash, profile.UAHash, profile.LocaleHash, profile.ScreenHash, profile.HardwareHash}
	return "register_risk:v1:{" + common.GenerateHMAC("register_risk:profile:v1:"+strings.Join(values, ":")) + "}", nil
}

var reserveRegisterRiskScript = redis.NewScript(`
local receipt = redis.call('HMGET', KEYS[3], 'count', 'epoch')
if receipt[1] then return receipt end
local value = redis.call('GET', KEYS[1])
local epoch
if not value then
  value = 0
  epoch = ARGV[2]
  redis.call('SET', KEYS[1], value, 'EX', ARGV[1])
  redis.call('SET', KEYS[2], epoch, 'EX', ARGV[1])
else
  local count = tonumber(value)
  if not count or count < 0 or count > 2147483647 or count ~= math.floor(count) then
    return redis.error_reply('invalid registration risk counter')
  end
  epoch = redis.call('GET', KEYS[2])
  if not epoch then return redis.error_reply('registration risk epoch missing') end
  if tonumber(value) < 2147483647 then value = redis.call('INCR', KEYS[1]) end
  redis.call('EXPIRE', KEYS[1], ARGV[1])
  redis.call('EXPIRE', KEYS[2], ARGV[1])
end
redis.call('HSET', KEYS[3], 'count', value, 'epoch', epoch)
redis.call('EXPIRE', KEYS[3], ARGV[1])
return {tostring(value), epoch}
`)

var rollbackRegisterRiskScript = redis.NewScript(`
local epoch = redis.call('HGET', KEYS[3], 'epoch')
if not epoch then return 0 end
redis.call('DEL', KEYS[3])
if redis.call('GET', KEYS[2]) ~= epoch then return 0 end
local value = tonumber(redis.call('GET', KEYS[1]))
if not value then return 0 end
if value == 0 then
  redis.call('DEL', KEYS[1], KEYS[2])
elseif value > 0 then
  redis.call('DECR', KEYS[1])
end
return 1
`)

// A receipt belongs to one reservation and one cooldown generation. Compensation
// cannot decrement another request or a new generation after the key expires.
// If the process dies or a commit result is uncertain, keep the reservation until
// cooldown expiry: removing it without proof of rollback could admit extra users.
type RegisterRiskReservation struct {
	keys []string
	rdb  *redis.Client
}

func ReserveRegisterRisk(ctx context.Context, user *User) (*RegisterRiskReservation, error) {
	config := common.GetRegisterRiskConfig()
	if !config.Enabled || !common.RedisEnabled {
		return nil, nil
	}
	if common.RDB == nil {
		return nil, registerRiskUnavailable(fmt.Errorf("Redis client unavailable"))
	}
	if user != nil && user.RegistrationRiskReason == RegisterRiskReasonStoreUnavailable {
		return nil, registerRiskUnavailable(fmt.Errorf("registration token store unavailable"))
	}
	if user == nil || user.RegistrationRiskScore != nil {
		return nil, &RegisterRiskError{Reason: "invalid_profile", Message: "注册环境验证失败，请刷新页面后重试"}
	}
	key, err := registrationRiskKey(user.RegistrationIP, user.RegistrationFingerprint)
	if err != nil {
		return nil, &RegisterRiskError{Reason: "invalid_profile", Message: "注册环境验证失败，请刷新页面后重试"}
	}
	if config.CooldownHours < 1 || config.CooldownHours > 720 || config.HitThreshold < 1 || config.HitThreshold > 100000 {
		return nil, registerRiskUnavailable(fmt.Errorf("invalid registration risk settings"))
	}
	reservation := &RegisterRiskReservation{
		keys: []string{key, key + ":epoch", key + ":receipt:" + common.GetUUID()},
		rdb:  common.RDB,
	}
	ctx, cancel := context.WithTimeout(ctx, registerRiskRedisTimeout)
	defer cancel()
	result, err := reserveRegisterRiskScript.Run(ctx, reservation.rdb, reservation.keys, config.CooldownHours*3600, common.GetUUID()).StringSlice()
	if err != nil || len(result) != 2 {
		return nil, registerRiskUnavailable(fmt.Errorf("reserve profile: %v", err))
	}
	hits, err := strconv.ParseInt(result[0], 10, 64)
	if err != nil {
		return nil, registerRiskUnavailable(err)
	}
	if hits >= int64(config.HitThreshold) {
		reservation.Finish(true) // Rejected hits remain counted and extend cooldown.
		return nil, &RegisterRiskError{Reason: "hit_threshold", Message: config.RejectMessage}
	}
	return reservation, nil
}

// Finish is safe on a nil reservation, and uses a fresh context even if the
// client has disconnected. committed refers to account creation, not delivery
// of the HTTP response or subsequent default-token/reward creation.
func (r *RegisterRiskReservation) Finish(committed bool) {
	if r == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), registerRiskRedisTimeout)
	defer cancel()
	var err error
	if committed {
		err = r.rdb.Del(ctx, r.keys[2]).Err()
	} else {
		err = rollbackRegisterRiskScript.Run(ctx, r.rdb, r.keys).Err()
	}
	if err != nil {
		common.SysError(fmt.Sprintf("registration risk finalization failed (committed=%t, key=%s): %v", committed, r.keys[0], err))
	}
}
