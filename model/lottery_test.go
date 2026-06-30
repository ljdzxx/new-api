package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaskLotteryUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		want     string
	}{
		{name: "empty", username: "", want: ""},
		{name: "single character", username: "a", want: "a"},
		{name: "two characters", username: "ab", want: "a*"},
		{name: "three characters", username: "abc", want: "a*c"},
		{name: "longer username", username: "alice", want: "a***e"},
		{name: "unicode username", username: "张三丰", want: "张*丰"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskLotteryUsername(tt.username); got != tt.want {
				t.Fatalf("MaskLotteryUsername(%q) = %q, want %q", tt.username, got, tt.want)
			}
		})
	}
}

func TestGetUserLotteryPrizeRecords(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(
		&Redemption{},
		&LotteryPeriod{},
		&LotteryPrize{},
		&LotteryPrizeCode{},
		&LotteryEntry{},
		&LotteryWinner{},
	))
	require.NoError(t, DB.Exec("DELETE FROM lottery_winners").Error)
	require.NoError(t, DB.Exec("DELETE FROM lottery_prize_codes").Error)
	require.NoError(t, DB.Exec("DELETE FROM lottery_prizes").Error)
	require.NoError(t, DB.Exec("DELETE FROM lottery_entries").Error)
	require.NoError(t, DB.Exec("DELETE FROM lottery_periods").Error)
	require.NoError(t, DB.Exec("DELETE FROM redemptions").Error)
	require.NoError(t, DB.Exec("DELETE FROM users").Error)

	now := common.GetTimestamp()
	user := &User{
		Username: fmt.Sprintf("lottery_prize_user_%d", time.Now().UnixNano()),
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(user).Error)

	baseIssue := int(time.Now().UnixNano() % 1_000_000_000)
	period := &LotteryPeriod{
		Issue:          baseIssue,
		Title:          "prize records",
		StartTime:      now - 3600,
		EndTime:        now - 1,
		DisplayEnabled: true,
		Status:         LotteryPeriodStatusDrawn,
		DrawTime:       now,
	}
	require.NoError(t, DB.Create(period).Error)
	resetPeriod := &LotteryPeriod{
		Issue:          baseIssue + 1,
		Title:          "reset records",
		StartTime:      now - 3600,
		EndTime:        now - 1,
		DisplayEnabled: true,
		Status:         LotteryPeriodStatusDrawn,
		DrawTime:       now,
	}
	require.NoError(t, DB.Create(resetPeriod).Error)
	welfarePeriod := &LotteryPeriod{
		Issue:          baseIssue + 2,
		Title:          "welfare records",
		StartTime:      now - 3600,
		EndTime:        now - 1,
		DisplayEnabled: true,
		Status:         LotteryPeriodStatusDrawn,
		DrawTime:       now,
	}
	require.NoError(t, DB.Create(welfarePeriod).Error)
	prize := &LotteryPrize{
		PeriodId:  period.Id,
		LevelName: "normal prize",
		Quantity:  1,
	}
	require.NoError(t, DB.Create(prize).Error)
	resetPrize := &LotteryPrize{
		PeriodId:  resetPeriod.Id,
		LevelName: "reset prize",
		Quantity:  1,
	}
	require.NoError(t, DB.Create(resetPrize).Error)
	welfarePrize := &LotteryPrize{
		PeriodId:  welfarePeriod.Id,
		LevelName: "welfare prize",
		Quantity:  1,
	}
	require.NoError(t, DB.Create(welfarePrize).Error)

	normalKey := fmt.Sprintf("N%031d", time.Now().UnixNano()%1_000_000_000_000_000_000)
	resetKey := fmt.Sprintf("R%031d", time.Now().UnixNano()%1_000_000_000_000_000_000)
	welfareKey := fmt.Sprintf("W%031d", time.Now().UnixNano()%1_000_000_000_000_000_000)
	require.NoError(t, DB.Create(&Redemption{
		Key:         normalKey,
		Status:      common.RedemptionCodeStatusEnabled,
		CodeType:    common.RedemptionCodeTypeNormal,
		Name:        "normal",
		Quota:       1000,
		ExpiredTime: now + 3600,
	}).Error)
	require.NoError(t, DB.Create(&Redemption{
		Key:      resetKey,
		Status:   common.RedemptionCodeStatusUsed,
		CodeType: common.RedemptionCodeTypeReset,
		Name:     "reset",
	}).Error)
	require.NoError(t, DB.Create(&Redemption{
		Key:      welfareKey,
		Status:   common.RedemptionCodeStatusEnabled,
		CodeType: common.RedemptionCodeTypeWelfare,
		Name:     "welfare",
		Quota:    1000,
	}).Error)

	require.NoError(t, DB.Create(&LotteryWinner{
		PeriodId:  period.Id,
		PrizeId:   prize.Id,
		CodeId:    1,
		UserId:    user.Id,
		Username:  user.Username,
		PrizeName: prize.LevelName,
		Code:      normalKey,
		CreatedAt: now - 10,
	}).Error)
	require.NoError(t, DB.Create(&LotteryWinner{
		PeriodId:  resetPeriod.Id,
		PrizeId:   resetPrize.Id,
		CodeId:    2,
		UserId:    user.Id,
		Username:  user.Username,
		PrizeName: resetPrize.LevelName,
		Code:      resetKey,
		CreatedAt: now,
	}).Error)
	require.NoError(t, DB.Create(&LotteryWinner{
		PeriodId:  welfarePeriod.Id,
		PrizeId:   welfarePrize.Id,
		CodeId:    3,
		UserId:    user.Id,
		Username:  user.Username,
		PrizeName: welfarePrize.LevelName,
		Code:      welfareKey,
		CreatedAt: now + 10,
	}).Error)

	items, total, err := GetUserLotteryPrizeRecords(user.Id, &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, items, 2)

	byPrize := make(map[string]LotteryPrizeRecord, len(items))
	for _, item := range items {
		byPrize[item.PrizeName] = item
	}

	normalRecord := byPrize["normal prize"]
	assert.Equal(t, period.Issue, normalRecord.Issue)
	assert.Equal(t, period.Title, normalRecord.Title)
	assert.Equal(t, common.RedemptionCodeTypeNormal, normalRecord.CodeType)
	assert.Equal(t, normalKey, normalRecord.Code)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, normalRecord.RedemptionStatus)
	assert.Equal(t, now+3600, normalRecord.ExpiredTime)

	resetRecord := byPrize["reset prize"]
	assert.Equal(t, common.RedemptionCodeTypeReset, resetRecord.CodeType)
	assert.Equal(t, resetKey, resetRecord.Code)
	assert.Equal(t, common.RedemptionCodeStatusUsed, resetRecord.RedemptionStatus)
}
