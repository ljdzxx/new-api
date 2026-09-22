package model

import (
	"context"
	"crypto/x509"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

func saveRegisterRiskChallenge(challenge *registerRiskChallenge) error {
	if !common.RedisEnabled {
		registerRiskChallengeMu.Lock()
		defer registerRiskChallengeMu.Unlock()
		cleanupRegisterRiskStoreLocked(time.Now().Unix())
		registerRiskChallenges[challenge.Id] = challenge
		return nil
	}
	if common.RDB == nil {
		return fmt.Errorf("registration risk Redis unavailable")
	}
	challenge.PrivateKeyDER = x509.MarshalPKCS1PrivateKey(challenge.PrivateKey)
	data, err := common.Marshal(challenge)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), registerRiskRedisTimeout)
	defer cancel()
	return common.RDB.Set(ctx, "register_risk:challenge:"+challenge.Id, data, registerRiskChallengeTTL).Err()
}

func loadRegisterRiskChallenge(id string) (*registerRiskChallenge, error) {
	if !common.RedisEnabled {
		registerRiskChallengeMu.Lock()
		defer registerRiskChallengeMu.Unlock()
		if challenge := registerRiskChallenges[id]; challenge != nil {
			copy := *challenge
			return &copy, nil
		}
		return nil, fmt.Errorf("register risk challenge not found")
	}
	if common.RDB == nil {
		return nil, fmt.Errorf("registration risk Redis unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), registerRiskRedisTimeout)
	defer cancel()
	data, err := common.RDB.Get(ctx, "register_risk:challenge:"+id).Bytes()
	if err != nil {
		return nil, err
	}
	var challenge registerRiskChallenge
	if err := common.Unmarshal(data, &challenge); err != nil {
		return nil, err
	}
	challenge.PrivateKey, err = x509.ParsePKCS1PrivateKey(challenge.PrivateKeyDER)
	return &challenge, err
}

var issueRegisterRiskTokenScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
redis.call('DEL', KEYS[1])
redis.call('SET', KEYS[2], ARGV[1], 'EX', ARGV[2])
return 1
`)

func saveRegisterRiskToken(challengeID string, record *registerRiskTokenRecord) error {
	if !common.RedisEnabled {
		registerRiskChallengeMu.Lock()
		defer registerRiskChallengeMu.Unlock()
		challenge := registerRiskChallenges[challengeID]
		if challenge == nil || challenge.Used || challenge.ExpiresAt <= time.Now().Unix() {
			return fmt.Errorf("register risk challenge already used or expired")
		}
		delete(registerRiskChallenges, challengeID)
		registerRiskTokens[record.TokenId] = record
		cleanupRegisterRiskStoreLocked(time.Now().Unix())
		return nil
	}
	if common.RDB == nil {
		return fmt.Errorf("registration risk Redis unavailable")
	}
	data, err := common.Marshal(record)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), registerRiskRedisTimeout)
	defer cancel()
	result, err := issueRegisterRiskTokenScript.Run(ctx, common.RDB, []string{"register_risk:challenge:" + challengeID, "register_risk:token:" + record.TokenId}, data, int(registerRiskTokenTTL/time.Second)).Int()
	if err != nil {
		return err
	}
	if result != 1 {
		return fmt.Errorf("register risk challenge already used or expired")
	}
	return nil
}

var consumeRegisterRiskTokenScript = redis.NewScript(`
local value = redis.call('GET', KEYS[1])
if not value then return '' end
if value == 'used' then return value end
local ttl = redis.call('PTTL', KEYS[1])
if ttl <= 0 then return '' end
redis.call('SET', KEYS[1], 'used', 'PX', ttl)
return value
`)

func takeRegisterRiskToken(id string) (*registerRiskTokenRecord, string) {
	if !common.RedisEnabled {
		registerRiskChallengeMu.Lock()
		defer registerRiskChallengeMu.Unlock()
		cleanupRegisterRiskStoreLocked(time.Now().Unix())
		record := registerRiskTokens[id]
		if record == nil {
			return nil, RegisterRiskReasonExpiredToken
		}
		if record.Used {
			return nil, RegisterRiskReasonReusedToken
		}
		record.Used = true
		copy := *record
		return &copy, ""
	}
	if common.RDB == nil {
		return nil, RegisterRiskReasonStoreUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), registerRiskRedisTimeout)
	defer cancel()
	data, err := consumeRegisterRiskTokenScript.Run(ctx, common.RDB, []string{"register_risk:token:" + id}).Text()
	if err != nil {
		common.SysError("registration risk token store unavailable: " + err.Error())
		return nil, RegisterRiskReasonStoreUnavailable
	}
	if data == "" {
		return nil, RegisterRiskReasonExpiredToken
	}
	if data == "used" {
		return nil, RegisterRiskReasonReusedToken
	}
	var record registerRiskTokenRecord
	if err := common.UnmarshalJsonStr(data, &record); err != nil {
		return nil, RegisterRiskReasonInvalidToken
	}
	return &record, ""
}
