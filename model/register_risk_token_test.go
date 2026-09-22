package model

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func encryptedRiskPayload(t *testing.T, challenge *RegisterRiskChallengeResponse, fp RegistrationFingerprint, invalidIV bool) string {
	t.Helper()
	publicKeyBytes, err := base64.StdEncoding.DecodeString(challenge.PublicKey)
	require.NoError(t, err)
	parsed, err := x509.ParsePKIXPublicKey(publicKeyBytes)
	require.NoError(t, err)
	key := make([]byte, 32)
	_, err = rand.Read(key)
	require.NoError(t, err)
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	iv := make([]byte, gcm.NonceSize())
	_, err = rand.Read(iv)
	require.NoError(t, err)
	data, err := common.Marshal(registerRiskCollectPayload{ChallengeId: challenge.ChallengeId, Nonce: challenge.Nonce, Fingerprint: fp})
	require.NoError(t, err)
	ciphertext := gcm.Seal(nil, iv, data, []byte(challenge.ChallengeId))
	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, parsed.(*rsa.PublicKey), key, []byte(challenge.ChallengeId))
	require.NoError(t, err)
	if invalidIV {
		iv = []byte{1}
	}
	envelope, err := common.Marshal(registerRiskEncryptedEnvelope{
		Key: base64.RawURLEncoding.EncodeToString(encryptedKey),
		IV:  base64.RawURLEncoding.EncodeToString(iv), Data: base64.RawURLEncoding.EncodeToString(ciphertext),
	})
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(envelope)
}

func TestRegisterRiskTokenAcrossInstancesAndReplay(t *testing.T) {
	server := setupRegisterRiskRedis(t)
	user := validRiskUser()
	challenge, err := CreateRegisterRiskChallenge(user.RegistrationIP, "browser")
	require.NoError(t, err)
	require.Empty(t, registerRiskChallenges)
	// A separate Redis connection simulates the next request reaching another instance.
	otherClient := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	defer otherClient.Close()
	common.RDB = otherClient
	token, err := CollectRegisterRiskToken(challenge.ChallengeId, encryptedRiskPayload(t, challenge, user.RegistrationFingerprint, false), user.RegistrationIP, "browser")
	require.NoError(t, err)
	_, err = CollectRegisterRiskToken(challenge.ChallengeId, "replay", user.RegistrationIP, "browser")
	require.Error(t, err)
	var accepted, rejected atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, score, _ := ConsumeRegisterRiskToken(token, user.RegistrationIP, "browser")
			if score == nil {
				accepted.Add(1)
			} else {
				rejected.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, accepted.Load())
	require.EqualValues(t, 11, rejected.Load())
}

func TestRegisterRiskTokenExpiryAndMismatch(t *testing.T) {
	server := setupRegisterRiskRedis(t)
	user := validRiskUser()
	challenge, err := CreateRegisterRiskChallenge(user.RegistrationIP, "browser")
	require.NoError(t, err)
	payload := encryptedRiskPayload(t, challenge, user.RegistrationFingerprint, false)
	_, err = CollectRegisterRiskToken(challenge.ChallengeId, payload, "203.0.113.99", "browser")
	require.Error(t, err)
	var token string
	require.NotPanics(t, func() {
		_, err = CollectRegisterRiskToken(challenge.ChallengeId, encryptedRiskPayload(t, challenge, user.RegistrationFingerprint, true), user.RegistrationIP, "browser")
	})
	require.Error(t, err)
	token, err = CollectRegisterRiskToken(challenge.ChallengeId, payload, user.RegistrationIP, "browser")
	require.NoError(t, err)
	_, score, reason := ConsumeRegisterRiskToken(token, user.RegistrationIP, "different browser")
	require.NotNil(t, score)
	require.Equal(t, RegisterRiskReasonIPUAMismatch, reason)
	challenge, err = CreateRegisterRiskChallenge(user.RegistrationIP, "browser")
	require.NoError(t, err)
	token, err = CollectRegisterRiskToken(challenge.ChallengeId, encryptedRiskPayload(t, challenge, user.RegistrationFingerprint, false), user.RegistrationIP, "browser")
	require.NoError(t, err)
	server.FastForward(registerRiskTokenTTL)
	_, score, reason = ConsumeRegisterRiskToken(token, user.RegistrationIP, "browser")
	require.NotNil(t, score)
	require.Equal(t, RegisterRiskReasonExpiredToken, reason)
}

func TestRegisterRiskTokenMemoryFallback(t *testing.T) {
	setupRegisterRiskRedis(t)
	common.RedisEnabled = false
	user := validRiskUser()
	challenge, err := CreateRegisterRiskChallenge(user.RegistrationIP, "browser")
	require.NoError(t, err)
	token, err := CollectRegisterRiskToken(challenge.ChallengeId, encryptedRiskPayload(t, challenge, user.RegistrationFingerprint, false), user.RegistrationIP, "browser")
	require.NoError(t, err)
	fp, score, reason := ConsumeRegisterRiskToken(token, user.RegistrationIP, "browser")
	require.Nil(t, score)
	require.Empty(t, reason)
	require.Equal(t, user.RegistrationFingerprint, fp)
	_, score, reason = ConsumeRegisterRiskToken(token, user.RegistrationIP, "browser")
	require.NotNil(t, score)
	require.Equal(t, RegisterRiskReasonReusedToken, reason)
}

func TestWeChatRegistrationDoesNotSaveProfile(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[]`)
	user := &User{Username: "wechat_excluded", WeChatId: "wechat_excluded", Role: common.RoleCommonUser}
	require.NoError(t, user.Insert(0))
	var count int64
	require.NoError(t, DB.Model(&UserRegistrationProfile{}).Where("user_id = ?", user.Id).Count(&count).Error)
	require.Zero(t, count)
}
