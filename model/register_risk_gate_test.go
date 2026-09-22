package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func setupRegisterRiskRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	rdb, enabled, config := common.RDB, common.RedisEnabled, common.GetRegisterRiskConfig()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RDB, common.RedisEnabled = client, true
	common.OptionMapRWMutex.Lock()
	common.RegistrationRisk = common.RegisterRiskConfig{Enabled: true, CooldownHours: 24, HitThreshold: 1, RejectMessage: "custom rejection"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		client.Close()
		common.RDB, common.RedisEnabled = rdb, enabled
		common.OptionMapRWMutex.Lock()
		common.RegistrationRisk = config
		common.OptionMapRWMutex.Unlock()
	})
	return server
}

func riskHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func refreshRiskFingerprint(fp *RegistrationFingerprint) {
	fp.FingerprintHash = riskHash(strings.Join([]string{fp.CanvasHash, fp.WebGLHash, fp.AudioHash, fp.FontsHash, fp.UAHash, fp.LocaleHash, fp.ScreenHash, fp.HardwareHash}, "|"))
}

func validRiskUser() *User {
	fp := RegistrationFingerprint{
		CanvasHash: riskHash("canvas"), WebGLHash: riskHash("webgl"), AudioHash: riskHash("audio"), FontsHash: riskHash("fonts"),
		UAHash: riskHash("ua"), LocaleHash: riskHash("locale"), ScreenHash: riskHash("screen"), HardwareHash: riskHash("hardware"),
	}
	refreshRiskFingerprint(&fp)
	return &User{RegistrationIP: "203.0.113.1", RegistrationFingerprint: fp}
}

func TestRegisterRiskThresholdAndSlidingCooldown(t *testing.T) {
	server := setupRegisterRiskRedis(t)
	user := validRiskUser()
	key, err := registrationRiskKey(user.RegistrationIP, user.RegistrationFingerprint)
	require.NoError(t, err)
	first, err := ReserveRegisterRisk(context.Background(), user)
	require.NoError(t, err)
	first.Finish(true)
	value, err := server.Get(key)
	require.NoError(t, err)
	require.Equal(t, "0", value)
	for i := 0; i < 3; i++ {
		server.FastForward(23 * time.Hour)
		_, err = ReserveRegisterRisk(context.Background(), user)
		var denied *RegisterRiskError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, "hit_threshold", denied.Reason)
		require.Equal(t, "custom rejection", denied.Message)
		require.Equal(t, 24*time.Hour, server.TTL(key))
	}
	value, err = server.Get(key)
	require.NoError(t, err)
	require.Equal(t, "3", value)
	server.FastForward(24 * time.Hour)
	next, err := ReserveRegisterRisk(context.Background(), user)
	require.NoError(t, err)
	next.Finish(true)
	value, err = server.Get(key)
	require.NoError(t, err)
	require.Equal(t, "0", value)
}

func TestRegisterRiskConcurrentRegistration(t *testing.T) {
	setupRegisterRiskRedis(t)
	common.RegistrationRisk.HitThreshold = 3
	var allowed, denied, unexpected atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reservation, err := ReserveRegisterRisk(context.Background(), validRiskUser())
			if err == nil {
				allowed.Add(1)
				reservation.Finish(true)
			} else if riskErr, ok := err.(*RegisterRiskError); ok && riskErr.Reason == "hit_threshold" {
				denied.Add(1)
			} else {
				unexpected.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 3, allowed.Load())
	require.EqualValues(t, 29, denied.Load())
	require.Zero(t, unexpected.Load())
}

func TestRegisterRiskCompensationAndGeneration(t *testing.T) {
	server := setupRegisterRiskRedis(t)
	user := validRiskUser()
	first, err := ReserveRegisterRisk(context.Background(), user)
	require.NoError(t, err)
	first.Finish(false)
	require.False(t, server.Exists(first.keys[0]))
	next, err := ReserveRegisterRisk(context.Background(), user)
	require.NoError(t, err)
	first.Finish(false) // Duplicate compensation must not erase the new account.
	require.True(t, server.Exists(next.keys[0]))
	server.FastForward(24 * time.Hour)
	newWindow, err := ReserveRegisterRisk(context.Background(), user)
	require.NoError(t, err)
	next.Finish(false)
	require.True(t, server.Exists(newWindow.keys[0]))
	newWindow.Finish(true)
	common.RegistrationRisk.HitThreshold = 2
	second, err := ReserveRegisterRisk(context.Background(), user)
	require.NoError(t, err)
	second.Finish(false)
	value, err := server.Get(newWindow.keys[0])
	require.NoError(t, err)
	require.Equal(t, "0", value)
}

func TestRegisterRiskCompensationPreservesConcurrentHits(t *testing.T) {
	server := setupRegisterRiskRedis(t)
	first, err := ReserveRegisterRisk(context.Background(), validRiskUser())
	require.NoError(t, err)
	server.FastForward(1 * time.Hour)
	_, err = ReserveRegisterRisk(context.Background(), validRiskUser())
	require.Error(t, err)
	first.Finish(false)
	require.Equal(t, 24*time.Hour, server.TTL(first.keys[0]))
	_, err = ReserveRegisterRisk(context.Background(), validRiskUser())
	require.Error(t, err)
}

func TestRegisterRiskAllConcurrentCreationsFail(t *testing.T) {
	server := setupRegisterRiskRedis(t)
	common.RegistrationRisk.HitThreshold = 3
	var pending []*RegisterRiskReservation
	for i := 0; i < 3; i++ {
		reservation, err := ReserveRegisterRisk(context.Background(), validRiskUser())
		require.NoError(t, err)
		pending = append(pending, reservation)
	}
	for _, reservation := range pending {
		reservation.Finish(false)
	}
	require.Empty(t, server.Keys())
}

func TestRegisterRiskOldReceiptCannotUndoNewCooldown(t *testing.T) {
	server := setupRegisterRiskRedis(t)
	pending, err := ReserveRegisterRisk(context.Background(), validRiskUser())
	require.NoError(t, err)
	// Changing cooldown can leave a previous reservation receipt outliving its counter.
	common.RegistrationRisk.CooldownHours = 1
	_, err = ReserveRegisterRisk(context.Background(), validRiskUser())
	require.Error(t, err)
	server.FastForward(time.Hour)
	newWindow, err := ReserveRegisterRisk(context.Background(), validRiskUser())
	require.NoError(t, err)
	require.True(t, server.Exists(pending.keys[2]))
	pending.Finish(false)
	require.True(t, server.Exists(newWindow.keys[0]))
	newWindow.Finish(true)
}

func TestRegisterRiskExactProfileAndInvalidProfile(t *testing.T) {
	user := validRiskUser()
	key, err := registrationRiskKey(user.RegistrationIP, user.RegistrationFingerprint)
	require.NoError(t, err)
	newIP, err := registrationRiskKey("203.0.113.2", user.RegistrationFingerprint)
	require.NoError(t, err)
	require.NotEqual(t, key, newIP)
	for i := 0; i < 8; i++ {
		fp := user.RegistrationFingerprint
		fields := []*string{&fp.CanvasHash, &fp.WebGLHash, &fp.AudioHash, &fp.FontsHash, &fp.UAHash, &fp.LocaleHash, &fp.ScreenHash, &fp.HardwareHash}
		*fields[i] = riskHash("changed")
		refreshRiskFingerprint(&fp)
		changed, err := registrationRiskKey(user.RegistrationIP, fp)
		require.NoError(t, err)
		require.NotEqual(t, key, changed)
	}
	fp := user.RegistrationFingerprint
	fp.FingerprintHash = riskHash("inconsistent")
	_, err = registrationRiskKey(user.RegistrationIP, fp)
	require.Error(t, err)
	fp = user.RegistrationFingerprint
	fp.AudioHash = "" // Unsupported optional capability is an explicit empty field.
	refreshRiskFingerprint(&fp)
	_, err = registrationRiskKey(user.RegistrationIP, fp)
	require.NoError(t, err)
	fp.CanvasHash, fp.WebGLHash, fp.FontsHash = "", "", ""
	refreshRiskFingerprint(&fp)
	_, err = registrationRiskKey(user.RegistrationIP, fp)
	require.Error(t, err)
}

func TestRegisterRiskDisabledAndFailClosed(t *testing.T) {
	server := setupRegisterRiskRedis(t)
	common.RedisEnabled = false
	reservation, err := ReserveRegisterRisk(context.Background(), &User{})
	require.NoError(t, err)
	require.Nil(t, reservation)
	common.RedisEnabled = true
	common.RegistrationRisk.Enabled = false
	_, err = ReserveRegisterRisk(context.Background(), &User{})
	require.NoError(t, err)
	common.RegistrationRisk.Enabled = true
	_, err = ReserveRegisterRisk(context.Background(), &User{})
	require.Error(t, err)
	require.Empty(t, server.Keys())
	server.Close()
	_, err = ReserveRegisterRisk(context.Background(), validRiskUser())
	var riskErr *RegisterRiskError
	require.ErrorAs(t, err, &riskErr)
	require.Equal(t, "unavailable", riskErr.Reason)
}
