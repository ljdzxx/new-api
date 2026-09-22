package model

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	RegisterRiskScoreAbnormal = 100

	RegisterRiskReasonMissingToken     = "risk_token_missing"
	RegisterRiskReasonInvalidToken     = "risk_token_invalid"
	RegisterRiskReasonExpiredToken     = "risk_token_expired"
	RegisterRiskReasonReusedToken      = "risk_token_reused"
	RegisterRiskReasonIPUAMismatch     = "risk_token_ip_ua_mismatch"
	RegisterRiskReasonStoreUnavailable = "risk_token_store_unavailable"
)

const (
	registerRiskChallengeTTL = 5 * time.Minute
	registerRiskTokenTTL     = 15 * time.Minute
)

type RegisterRiskChallengeResponse struct {
	ChallengeId string `json:"challenge_id"`
	Nonce       string `json:"nonce"`
	PublicKey   string `json:"public_key"`
	ExpiresAt   int64  `json:"expires_at"`
}

type registerRiskChallenge struct {
	Id            string
	Nonce         string
	IPHash        string
	UAHash        string
	PrivateKey    *rsa.PrivateKey `json:"-"`
	PrivateKeyDER []byte
	ExpiresAt     int64
	Used          bool
}

type registerRiskTokenRecord struct {
	TokenId     string
	ChallengeId string
	IPHash      string
	UAHash      string
	Fingerprint RegistrationFingerprint
	ExpiresAt   int64
	Attempts    int
	Used        bool
}

type registerRiskEncryptedEnvelope struct {
	Key  string `json:"key"`
	IV   string `json:"iv"`
	Data string `json:"data"`
}

type registerRiskCollectPayload struct {
	ChallengeId string                  `json:"challenge_id"`
	Nonce       string                  `json:"nonce"`
	Fingerprint RegistrationFingerprint `json:"fingerprint"`
}

var (
	registerRiskChallengeMu sync.Mutex
	registerRiskChallenges  = map[string]*registerRiskChallenge{}
	registerRiskTokens      = map[string]*registerRiskTokenRecord{}
)

func registerRiskHash(kind string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return common.GenerateHMAC("register_risk:" + kind + ":" + value)
}

func decodeRegisterRiskBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("empty base64 value")
	}
	if data, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return data, nil
	}
	if data, err := base64.URLEncoding.DecodeString(value); err == nil {
		return data, nil
	}
	return base64.StdEncoding.DecodeString(value)
}

func cleanupRegisterRiskStoreLocked(now int64) {
	for id, challenge := range registerRiskChallenges {
		if challenge.ExpiresAt <= now || challenge.Used {
			delete(registerRiskChallenges, id)
		}
	}
	for id, token := range registerRiskTokens {
		if token.ExpiresAt <= now {
			delete(registerRiskTokens, id)
		}
	}
}

func CreateRegisterRiskChallenge(ip string, ua string) (*RegisterRiskChallengeResponse, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, err
	}
	challenge := &registerRiskChallenge{
		Id:         common.GetUUID(),
		Nonce:      common.GetUUID(),
		IPHash:     registerRiskHash("ip", ip),
		UAHash:     registerRiskHash("ua", ua),
		PrivateKey: privateKey,
		ExpiresAt:  time.Now().Add(registerRiskChallengeTTL).Unix(),
	}
	if err := saveRegisterRiskChallenge(challenge); err != nil {
		return nil, err
	}
	return &RegisterRiskChallengeResponse{
		ChallengeId: challenge.Id,
		Nonce:       challenge.Nonce,
		PublicKey:   base64.StdEncoding.EncodeToString(publicKeyBytes),
		ExpiresAt:   challenge.ExpiresAt,
	}, nil
}

func decryptRegisterRiskEnvelope(challenge *registerRiskChallenge, encryptedEnvelope string) (*registerRiskCollectPayload, error) {
	envelopeBytes, err := decodeRegisterRiskBase64(encryptedEnvelope)
	if err != nil {
		return nil, err
	}
	var envelope registerRiskEncryptedEnvelope
	if err = common.Unmarshal(envelopeBytes, &envelope); err != nil {
		return nil, err
	}
	encryptedKey, err := decodeRegisterRiskBase64(envelope.Key)
	if err != nil {
		return nil, err
	}
	iv, err := decodeRegisterRiskBase64(envelope.IV)
	if err != nil {
		return nil, err
	}
	ciphertext, err := decodeRegisterRiskBase64(envelope.Data)
	if err != nil {
		return nil, err
	}
	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, challenge.PrivateKey, encryptedKey, []byte(challenge.Id))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(iv) != gcm.NonceSize() {
		return nil, errors.New("invalid register risk nonce size")
	}
	plaintext, err := gcm.Open(nil, iv, ciphertext, []byte(challenge.Id))
	if err != nil {
		return nil, err
	}
	var payload registerRiskCollectPayload
	if err = common.Unmarshal(plaintext, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func CollectRegisterRiskToken(challengeId string, encryptedEnvelope string, ip string, ua string) (string, error) {
	now := time.Now().Unix()
	challenge, err := loadRegisterRiskChallenge(challengeId)
	if err != nil {
		return "", err
	}
	if challenge.Used || challenge.ExpiresAt <= now {
		return "", errors.New("register risk challenge expired")
	}
	if challenge.IPHash != registerRiskHash("ip", ip) || challenge.UAHash != registerRiskHash("ua", ua) {
		return "", errors.New("register risk challenge client mismatch")
	}

	payload, err := decryptRegisterRiskEnvelope(challenge, encryptedEnvelope)
	if err != nil {
		return "", err
	}
	if payload.ChallengeId != challenge.Id || payload.Nonce != challenge.Nonce {
		return "", errors.New("register risk challenge payload mismatch")
	}

	tokenId := common.GetUUID()
	token := tokenId + "." + common.GenerateHMAC("register_risk_token:"+tokenId)
	record := &registerRiskTokenRecord{
		TokenId:     tokenId,
		ChallengeId: challenge.Id,
		IPHash:      registerRiskHash("ip", ip),
		UAHash:      registerRiskHash("ua", ua),
		Fingerprint: payload.Fingerprint,
		ExpiresAt:   time.Now().Add(registerRiskTokenTTL).Unix(),
	}

	if err := saveRegisterRiskToken(challengeId, record); err != nil {
		return "", err
	}
	return token, nil
}

func ConsumeRegisterRiskToken(token string, ip string, ua string) (RegistrationFingerprint, *int, string) {
	token = strings.TrimSpace(token)
	score := RegisterRiskScoreAbnormal
	if token == "" {
		return RegistrationFingerprint{Missing: true}, &score, RegisterRiskReasonMissingToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return RegistrationFingerprint{Missing: true}, &score, RegisterRiskReasonInvalidToken
	}
	tokenId := parts[0]
	expectedSignature := common.GenerateHMAC("register_risk_token:" + tokenId)
	if !hmacEqual(parts[1], expectedSignature) {
		return RegistrationFingerprint{Missing: true}, &score, RegisterRiskReasonInvalidToken
	}

	now := time.Now().Unix()
	record, reason := takeRegisterRiskToken(tokenId)
	if record == nil {
		return RegistrationFingerprint{Missing: true}, &score, reason
	}
	if record.ExpiresAt <= now {
		return RegistrationFingerprint{Missing: true}, &score, RegisterRiskReasonExpiredToken
	}
	if record.IPHash != registerRiskHash("ip", ip) || record.UAHash != registerRiskHash("ua", ua) {
		return RegistrationFingerprint{Missing: true}, &score, RegisterRiskReasonIPUAMismatch
	}
	return record.Fingerprint, nil, ""
}

func hmacEqual(left string, right string) bool {
	if len(left) != len(right) {
		return false
	}
	var result byte
	for i := 0; i < len(left); i++ {
		result |= left[i] ^ right[i]
	}
	return result == 0
}
