package service

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const lotteryDrawTickInterval = time.Minute

var lotteryDrawTaskOnce sync.Once

type lotteryPrizeSlot struct {
	Prize model.LotteryPrize
}

type lotteryUserCandidate struct {
	Entry  model.LotteryEntry
	IsPaid bool
}

func StartLotteryDrawTask() {
	if !common.IsMasterNode {
		return
	}
	lotteryDrawTaskOnce.Do(func() {
		go func() {
			runDueLotteryDraws()
			ticker := time.NewTicker(lotteryDrawTickInterval)
			defer ticker.Stop()
			for range ticker.C {
				runDueLotteryDraws()
			}
		}()
	})
}

func runDueLotteryDraws() {
	var periods []model.LotteryPeriod
	now := common.GetTimestamp()
	if err := model.DB.
		Where("status <> ? AND end_time > 0 AND end_time <= ?", model.LotteryPeriodStatusDrawn, now).
		Order("end_time asc").
		Find(&periods).Error; err != nil {
		common.SysError("lottery draw task list failed: " + err.Error())
		return
	}
	for _, period := range periods {
		if _, err := DrawLotteryPeriod(period.Id); err != nil {
			common.SysError(fmt.Sprintf("lottery draw failed, period #%d: %s", period.Id, err.Error()))
		}
	}
}

func DrawLotteryPeriod(periodId int) ([]model.LotteryWinner, error) {
	if periodId <= 0 {
		return nil, errors.New("无效的抽奖期数")
	}

	var winners []model.LotteryWinner
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var period model.LotteryPeriod
		if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("id = ?", periodId).First(&period).Error; err != nil {
			return errors.New("抽奖期数不存在")
		}
		if period.Status == model.LotteryPeriodStatusDrawn {
			return errors.New("该期已开奖，不能重复开奖")
		}
		now := common.GetTimestamp()
		if period.EndTime <= 0 || period.EndTime > now {
			return errors.New("未到截止时间，不能开奖")
		}
		if err := tx.Model(&model.LotteryPeriod{}).
			Where("id = ? AND status <> ?", period.Id, model.LotteryPeriodStatusDrawn).
			Updates(map[string]interface{}{"status": model.LotteryPeriodStatusDrawing, "updated_at": now}).Error; err != nil {
			return err
		}

		prizes, codesByPrize, err := loadAndValidateLotteryPrizes(tx, period.Id)
		if err != nil {
			return err
		}

		var entries []model.LotteryEntry
		if err := tx.Where("period_id = ?", period.Id).Order("id asc").Find(&entries).Error; err != nil {
			return err
		}

		winners, err = buildLotteryWinners(tx, period.Id, prizes, codesByPrize, entries, now)
		if err != nil {
			return err
		}

		for _, winner := range winners {
			if err := tx.Create(&winner).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.LotteryPrizeCode{}).
				Where("id = ? AND status = ?", winner.CodeId, model.LotteryCodeStatusUnused).
				Updates(map[string]interface{}{
					"status":      model.LotteryCodeStatusAssigned,
					"winner_id":   winner.UserId,
					"assigned_at": now,
				}).Error; err != nil {
				return err
			}
			if err := model.BindRedemptionExclusiveUserTx(tx, winner.Code, winner.UserId); err != nil {
				return err
			}
		}

		return tx.Model(&model.LotteryPeriod{}).
			Where("id = ?", period.Id).
			Updates(map[string]interface{}{
				"status":     model.LotteryPeriodStatusDrawn,
				"draw_time":  now,
				"updated_at": now,
			}).Error
	})
	if err != nil {
		return nil, err
	}
	return winners, nil
}

func loadAndValidateLotteryPrizes(tx *gorm.DB, periodId int) ([]model.LotteryPrize, map[int][]model.LotteryPrizeCode, error) {
	var prizes []model.LotteryPrize
	if err := tx.Where("period_id = ?", periodId).Order("sort_order asc, id asc").Find(&prizes).Error; err != nil {
		return nil, nil, err
	}
	if len(prizes) == 0 {
		return nil, nil, errors.New("请先配置奖品")
	}

	codesByPrize := make(map[int][]model.LotteryPrizeCode, len(prizes))
	totalPrizeCount := 0
	totalCodeCount := 0
	for _, prize := range prizes {
		if prize.Quantity <= 0 {
			return nil, nil, fmt.Errorf("%s 的奖品数量必须大于 0", prize.LevelName)
		}
		var codes []model.LotteryPrizeCode
		if err := tx.Where("period_id = ? AND prize_id = ? AND status = ?", periodId, prize.Id, model.LotteryCodeStatusUnused).
			Order("id asc").
			Find(&codes).Error; err != nil {
			return nil, nil, err
		}
		if len(codes) != prize.Quantity {
			return nil, nil, fmt.Errorf("%s 的兑换码数量必须等于奖品数量", prize.LevelName)
		}
		codesByPrize[prize.Id] = codes
		totalPrizeCount += prize.Quantity
		totalCodeCount += len(codes)
	}
	if totalPrizeCount != totalCodeCount {
		return nil, nil, errors.New("兑换码总数必须与奖品总数一致")
	}
	return prizes, codesByPrize, nil
}

func buildLotteryWinners(tx *gorm.DB, periodId int, prizes []model.LotteryPrize, codesByPrize map[int][]model.LotteryPrizeCode, entries []model.LotteryEntry, now int64) ([]model.LotteryWinner, error) {
	if len(entries) == 0 {
		return []model.LotteryWinner{}, nil
	}

	paidUsers, err := loadPaidUserSet(tx, entries)
	if err != nil {
		return nil, err
	}

	users := make([]lotteryUserCandidate, 0, len(entries))
	for _, entry := range entries {
		users = append(users, lotteryUserCandidate{
			Entry:  entry,
			IsPaid: paidUsers[entry.UserId],
		})
	}

	slots := make([]lotteryPrizeSlot, 0)
	for _, prize := range prizes {
		for i := 0; i < prize.Quantity; i++ {
			slots = append(slots, lotteryPrizeSlot{Prize: prize})
		}
	}
	if err := shufflePrizeSlots(slots); err != nil {
		return nil, err
	}
	if err := shuffleUserCandidates(users); err != nil {
		return nil, err
	}

	winners := make([]model.LotteryWinner, 0, minInt(len(users), len(slots)))
	for len(users) > 0 && len(slots) > 0 {
		userIndex := pickMostConstrainedUser(users, slots)
		if userIndex < 0 {
			break
		}
		user := users[userIndex]
		eligiblePrizeIndexes := eligiblePrizeIndexesForUser(user, slots)
		if len(eligiblePrizeIndexes) == 0 {
			break
		}
		chosenEligibleIndex, err := cryptoRandomInt(len(eligiblePrizeIndexes))
		if err != nil {
			return nil, err
		}
		slotIndex := eligiblePrizeIndexes[chosenEligibleIndex]
		slot := slots[slotIndex]

		codes := codesByPrize[slot.Prize.Id]
		if len(codes) == 0 {
			return nil, fmt.Errorf("%s 可用兑换码不足", slot.Prize.LevelName)
		}
		code := codes[0]
		codesByPrize[slot.Prize.Id] = codes[1:]

		winners = append(winners, model.LotteryWinner{
			PeriodId:         periodId,
			PrizeId:          slot.Prize.Id,
			CodeId:           code.Id,
			UserId:           user.Entry.UserId,
			Username:         user.Entry.Username,
			PrizeName:        slot.Prize.LevelName,
			PrizeDescription: slot.Prize.Description,
			Code:             code.Code,
			CreatedAt:        now,
		})

		users = append(users[:userIndex], users[userIndex+1:]...)
		slots = append(slots[:slotIndex], slots[slotIndex+1:]...)
	}

	return winners, nil
}

func loadPaidUserSet(tx *gorm.DB, entries []model.LotteryEntry) (map[int]bool, error) {
	userIds := make([]int, 0, len(entries))
	seen := make(map[int]struct{}, len(entries))
	for _, entry := range entries {
		if entry.UserId <= 0 {
			continue
		}
		if _, ok := seen[entry.UserId]; ok {
			continue
		}
		seen[entry.UserId] = struct{}{}
		userIds = append(userIds, entry.UserId)
	}
	paidUsers := make(map[int]bool)
	if len(userIds) == 0 {
		return paidUsers, nil
	}

	successOrderUsers, err := loadSuccessfulOrderUserSet(tx, userIds)
	if err != nil {
		return nil, err
	}
	positiveBalanceUsers, err := loadPositiveBalanceUserSet(tx, userIds)
	if err != nil {
		return nil, err
	}
	activeSubscriptionUsers, err := loadActiveSubscriptionUserSet(tx, userIds)
	if err != nil {
		return nil, err
	}

	for _, userId := range userIds {
		if successOrderUsers[userId] && (positiveBalanceUsers[userId] || activeSubscriptionUsers[userId]) {
			paidUsers[userId] = true
		}
	}
	return paidUsers, nil
}

func loadSuccessfulOrderUserSet(tx *gorm.DB, userIds []int) (map[int]bool, error) {
	var userIdsWithOrders []int
	if err := tx.Model(&model.TopUp{}).
		Where("user_id IN ? AND status = ? AND money > ?", userIds, common.TopUpStatusSuccess, 0).
		Distinct("user_id").
		Pluck("user_id", &userIdsWithOrders).Error; err != nil {
		return nil, err
	}
	return intSetFromSlice(userIdsWithOrders), nil
}

func loadPositiveBalanceUserSet(tx *gorm.DB, userIds []int) (map[int]bool, error) {
	var userIdsWithBalance []int
	if err := tx.Model(&model.User{}).
		Where("id IN ? AND quota > ?", userIds, 0).
		Pluck("id", &userIdsWithBalance).Error; err != nil {
		return nil, err
	}
	return intSetFromSlice(userIdsWithBalance), nil
}

func loadActiveSubscriptionUserSet(tx *gorm.DB, userIds []int) (map[int]bool, error) {
	var userIdsWithSubscription []int
	now := common.GetTimestamp()
	if err := tx.Model(&model.UserSubscription{}).
		Where("user_id IN ? AND status = ? AND end_time > ?", userIds, "active", now).
		Distinct("user_id").
		Pluck("user_id", &userIdsWithSubscription).Error; err != nil {
		return nil, err
	}
	return intSetFromSlice(userIdsWithSubscription), nil
}

func intSetFromSlice(values []int) map[int]bool {
	result := make(map[int]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func pickMostConstrainedUser(users []lotteryUserCandidate, slots []lotteryPrizeSlot) int {
	type optionCount struct {
		Index int
		Count int
	}
	counts := make([]optionCount, 0, len(users))
	for i, user := range users {
		count := len(eligiblePrizeIndexesForUser(user, slots))
		if count > 0 {
			counts = append(counts, optionCount{Index: i, Count: count})
		}
	}
	if len(counts) == 0 {
		return -1
	}
	sort.SliceStable(counts, func(i, j int) bool {
		return counts[i].Count < counts[j].Count
	})
	minCount := counts[0].Count
	tied := make([]int, 0)
	for _, count := range counts {
		if count.Count != minCount {
			break
		}
		tied = append(tied, count.Index)
	}
	chosen, err := cryptoRandomInt(len(tied))
	if err != nil {
		return tied[0]
	}
	return tied[chosen]
}

func eligiblePrizeIndexesForUser(user lotteryUserCandidate, slots []lotteryPrizeSlot) []int {
	indexes := make([]int, 0, len(slots))
	for i, slot := range slots {
		if slot.Prize.PaidOnly && !user.IsPaid {
			continue
		}
		indexes = append(indexes, i)
	}
	return indexes
}

func shufflePrizeSlots(items []lotteryPrizeSlot) error {
	for i := len(items) - 1; i > 0; i-- {
		j, err := cryptoRandomInt(i + 1)
		if err != nil {
			return err
		}
		items[i], items[j] = items[j], items[i]
	}
	return nil
}

func shuffleUserCandidates(items []lotteryUserCandidate) error {
	for i := len(items) - 1; i > 0; i-- {
		j, err := cryptoRandomInt(i + 1)
		if err != nil {
			return err
		}
		items[i], items[j] = items[j], items[i]
	}
	return nil
}

func cryptoRandomInt(max int) (int, error) {
	if max <= 0 {
		return 0, errors.New("invalid random range")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
