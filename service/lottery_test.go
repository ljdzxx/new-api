package service

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupLotteryDrawTest(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(
		&model.Redemption{},
		&model.TopUp{},
		&model.LotteryPeriod{},
		&model.LotteryPrize{},
		&model.LotteryPrizeCode{},
		&model.LotteryEntry{},
		&model.LotteryWinner{},
	))
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM lottery_winners")
		model.DB.Exec("DELETE FROM lottery_prize_codes")
		model.DB.Exec("DELETE FROM lottery_prizes")
		model.DB.Exec("DELETE FROM lottery_entries")
		model.DB.Exec("DELETE FROM lottery_periods")
		model.DB.Exec("DELETE FROM redemptions")
		model.DB.Exec("DELETE FROM top_ups")
		model.DB.Exec("DELETE FROM users")
	})
}

func createLotteryWithRedemptionCode(t *testing.T, codeType int) (*model.Redemption, *model.User, int) {
	t.Helper()
	now := common.GetTimestamp()
	user := &model.User{
		Username: fmt.Sprintf("lottery_user_%d", time.Now().UnixNano()),
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)

	key := fmt.Sprintf("L%031d", time.Now().UnixNano()%1_000_000_000_000_000_000)
	redemption := &model.Redemption{
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		CodeType:    codeType,
		Name:        "lottery_redeem",
		Quota:       1000,
		CreatedTime: now,
	}
	require.NoError(t, model.DB.Create(redemption).Error)

	period := &model.LotteryPeriod{
		Issue:          int(time.Now().UnixNano() % 1_000_000_000),
		Title:          "lottery",
		StartTime:      now - 3600,
		EndTime:        now - 1,
		DisplayEnabled: true,
		Status:         model.LotteryPeriodStatusDraft,
	}
	require.NoError(t, model.DB.Create(period).Error)
	prize := &model.LotteryPrize{
		PeriodId:  period.Id,
		LevelName: "first",
		Quantity:  1,
	}
	require.NoError(t, model.DB.Create(prize).Error)
	require.NoError(t, model.DB.Create(&model.LotteryPrizeCode{
		PeriodId: period.Id,
		PrizeId:  prize.Id,
		Code:     key,
		Status:   model.LotteryCodeStatusUnused,
	}).Error)
	require.NoError(t, model.DB.Create(&model.LotteryEntry{
		PeriodId: period.Id,
		UserId:   user.Id,
		Username: user.Username,
	}).Error)
	return redemption, user, period.Id
}

func TestDrawLotteryPeriodBindsNormalRedemptionCodeToWinner(t *testing.T) {
	setupLotteryDrawTest(t)
	redemption, winnerUser, periodId := createLotteryWithRedemptionCode(t, common.RedemptionCodeTypeNormal)

	winners, err := DrawLotteryPeriod(periodId)
	require.NoError(t, err)
	require.Len(t, winners, 1)

	var reloaded model.Redemption
	require.NoError(t, model.DB.First(&reloaded, redemption.Id).Error)
	assert.Equal(t, winnerUser.Id, reloaded.BoundUserId)
}

func TestDrawLotteryPeriodDoesNotBindWelfareRedemptionCode(t *testing.T) {
	setupLotteryDrawTest(t)
	redemption, _, periodId := createLotteryWithRedemptionCode(t, common.RedemptionCodeTypeWelfare)

	winners, err := DrawLotteryPeriod(periodId)
	require.NoError(t, err)
	require.Len(t, winners, 1)

	var reloaded model.Redemption
	require.NoError(t, model.DB.First(&reloaded, redemption.Id).Error)
	assert.Equal(t, 0, reloaded.BoundUserId)
}
