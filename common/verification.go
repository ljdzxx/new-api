package common

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type verificationValue struct {
	code string
	time time.Time
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"
)

var verificationMutex sync.Mutex
var verificationMap map[string]verificationValue
var verificationMapMaxSize = 10
var VerificationValidMinutes = 10

func normalizeVerificationKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func normalizeVerificationCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}

func verificationStorageKey(key string, purpose string) string {
	return fmt.Sprintf("verification:%s:%s", purpose, normalizeVerificationKey(key))
}

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) {
	key = normalizeVerificationKey(key)
	code = normalizeVerificationCode(code)

	if RedisEnabled && RDB != nil {
		if err := RedisSet(verificationStorageKey(key, purpose), code, time.Duration(VerificationValidMinutes)*time.Minute); err != nil {
			SysLog(fmt.Sprintf("failed to save verification code to Redis: %v", err))
		}
	}

	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap[purpose+key] = verificationValue{
		code: code,
		time: time.Now(),
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
	}
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	key = normalizeVerificationKey(key)
	code = normalizeVerificationCode(code)

	if RedisEnabled && RDB != nil {
		value, err := RedisGet(verificationStorageKey(key, purpose))
		if err == nil {
			return code == value
		}
	}

	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	now := time.Now()
	if !okay || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 {
		return false
	}
	return code == value.code
}

func DeleteKey(key string, purpose string) {
	key = normalizeVerificationKey(key)
	if RedisEnabled && RDB != nil {
		if err := RedisDelKey(verificationStorageKey(key, purpose)); err != nil {
			SysLog(fmt.Sprintf("failed to delete verification code from Redis: %v", err))
		}
	}

	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	delete(verificationMap, purpose+key)
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
