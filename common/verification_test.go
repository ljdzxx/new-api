package common

import (
	"testing"
	"time"
)

func resetVerificationStateForTest() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}

func TestVerifyCodeWithKeyNormalizesEmailAndCode(t *testing.T) {
	resetVerificationStateForTest()

	RegisterVerificationCodeWithKey("User@Example.COM ", " ab12cd ", EmailVerificationPurpose)

	if !VerifyCodeWithKey(" user@example.com", "AB12CD ", EmailVerificationPurpose) {
		t.Fatal("expected normalized email and code to verify")
	}
}

func TestVerifyCodeWithKeyRejectsExpiredCode(t *testing.T) {
	resetVerificationStateForTest()

	key := normalizeVerificationKey("user@example.com")
	verificationMap[EmailVerificationPurpose+key] = verificationValue{
		code: normalizeVerificationCode("abc123"),
		time: time.Now().Add(-time.Duration(VerificationValidMinutes)*time.Minute - time.Second),
	}

	if VerifyCodeWithKey("user@example.com", "abc123", EmailVerificationPurpose) {
		t.Fatal("expected expired code to be rejected")
	}
}

func TestDeleteKeyRemovesNormalizedVerificationCode(t *testing.T) {
	resetVerificationStateForTest()

	RegisterVerificationCodeWithKey("User@Example.COM", "abc123", EmailVerificationPurpose)
	DeleteKey(" user@example.com ", EmailVerificationPurpose)

	if VerifyCodeWithKey("user@example.com", "abc123", EmailVerificationPurpose) {
		t.Fatal("expected deleted code to be rejected")
	}
}
