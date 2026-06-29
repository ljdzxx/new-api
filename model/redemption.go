package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
)

// ErrRedeemFailed is returned when redemption fails due to database error
var ErrRedeemFailed = errors.New("redeem.failed")

var (
	ErrRedemptionAlreadyRedeemed = errors.New("已兑换")
	ErrRedemptionExpired         = errors.New("兑换码已过期")
)

func isRedemptionBusinessError(err error) bool {
	return errors.Is(err, ErrRedemptionAlreadyRedeemed) ||
		errors.Is(err, ErrRedemptionExpired)
}

type Redemption struct {
	Id           int            `json:"id"`
	UserId       int            `json:"user_id"`
	Key          string         `json:"key" gorm:"type:char(32);uniqueIndex"`
	Status       int            `json:"status" gorm:"default:1"`
	CodeType     int            `json:"code_type" gorm:"type:int;default:1;index"`
	RewardType   int            `json:"reward_type" gorm:"type:int;default:1"`
	Name         string         `json:"name" gorm:"index"`
	Quota        int            `json:"quota" gorm:"default:100"`
	PlanId       int            `json:"plan_id" gorm:"type:int;default:0;index"`
	PayMoney     float64        `json:"pay_money" gorm:"default:0;column:pay_money"`
	CreatedTime  int64          `json:"created_time" gorm:"bigint"`
	RedeemedTime int64          `json:"redeemed_time" gorm:"bigint"`
	Count        int            `json:"count" gorm:"-:all"` // only for api request
	UsedUserId   int            `json:"used_user_id"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
	ExpiredTime  int64          `json:"expired_time" gorm:"bigint"` // 过期时间，0 表示不过期
}

type RedemptionUsage struct {
	Id             int    `json:"id"`
	RedemptionId   int    `json:"redemption_id" gorm:"not null;uniqueIndex:idx_redemption_usage_user;index"`
	UserId         int    `json:"user_id" gorm:"not null;uniqueIndex:idx_redemption_usage_user;index"`
	Key            string `json:"key" gorm:"type:char(32);index"`
	RedeemedTime   int64  `json:"redeemed_time" gorm:"bigint;index"`
	RewardType     int    `json:"reward_type" gorm:"type:int;default:1"`
	PlanId         int    `json:"plan_id" gorm:"type:int;default:0;index"`
	Quota          int    `json:"quota" gorm:"default:0"`
	SubscriptionId int    `json:"subscription_id" gorm:"type:int;default:0;index"`
}

func GetAllRedemptions(startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	// 开始事务
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 获取总数
	err = tx.Model(&Redemption{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 获取分页数据
	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 提交事务
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func SearchRedemptions(keyword string, startIdx int, num int) (redemptions []*Redemption, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Build query based on keyword type
	query := tx.Model(&Redemption{})

	// Only try to convert to ID if the string represents a valid integer
	if id, err := strconv.Atoi(keyword); err == nil {
		query = query.Where("id = ? OR name LIKE ?", id, keyword+"%")
	} else {
		query = query.Where("name LIKE ?", keyword+"%")
	}

	// Get total count
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated data
	err = query.Order("id desc").Limit(num).Offset(startIdx).Find(&redemptions).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return redemptions, total, nil
}

func GetRedemptionById(id int) (*Redemption, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	redemption := Redemption{Id: id}
	var err error = nil
	err = DB.First(&redemption, "id = ?", id).Error
	return &redemption, err
}

type RedeemResult struct {
	RewardType int    `json:"reward_type"`
	Quota      int    `json:"quota"`
	PlanId     int    `json:"plan_id"`
	PlanTitle  string `json:"plan_title"`
}

type redemptionRewardApplyResult struct {
	levelChanged              bool
	upgradedGroup             string
	upgradedLevelID           int
	upgradedSubscriptionGroup string
	subscriptionId            int
}

func applyRedemptionRewardTx(tx *gorm.DB, redemption *Redemption, userId int, result *RedeemResult, tradeNo string) (*redemptionRewardApplyResult, error) {
	applyResult := &redemptionRewardApplyResult{}
	rewardType := redemption.RewardType
	if rewardType == 0 {
		rewardType = common.RedemptionRewardTypeQuota
	}
	if redemption.PayMoney < 0 {
		return nil, errors.New("兑换码实付金额无效")
	}
	result.RewardType = rewardType

	redeemedAt := common.GetTimestamp()
	switch rewardType {
	case common.RedemptionRewardTypeQuota:
		if redemption.Quota <= 0 {
			return nil, errors.New("兑换码额度无效")
		}
		if err := tx.Model(&User{}).Where("id = ?", userId).Update("quota", gorm.Expr("quota + ?", redemption.Quota)).Error; err != nil {
			return nil, err
		}
		result.Quota = redemption.Quota

		topup := TopUp{
			UserId:          userId,
			Amount:          quotaToTopUpAmount(redemption.Quota),
			Money:           redemption.PayMoney,
			TradeNo:         tradeNo,
			PaymentMethod:   "redemption",
			PaymentProvider: PaymentProviderMall,
			CreateTime:      redeemedAt,
			CompleteTime:    redeemedAt,
			Status:          common.TopUpStatusSuccess,
		}
		if err := tx.Create(&topup).Error; err != nil {
			return nil, err
		}
	case common.RedemptionRewardTypeSubscription:
		if redemption.PlanId <= 0 {
			return nil, errors.New("兑换码未绑定订阅套餐")
		}
		plan, err := getSubscriptionPlanByIdTx(tx, redemption.PlanId)
		if err != nil {
			return nil, err
		}
		if !plan.Enabled {
			return nil, errors.New("订阅套餐未启用")
		}
		sub, err := CreateUserSubscriptionFromPlanTx(tx, userId, plan, "redemption")
		if err != nil {
			return nil, err
		}
		if sub != nil && sub.Id > 0 {
			if err = tx.Model(sub).Update("redemption_id", redemption.Id).Error; err != nil {
				return nil, err
			}
			applyResult.subscriptionId = sub.Id
		}
		applyResult.upgradedSubscriptionGroup = strings.TrimSpace(plan.UpgradeGroup)
		result.PlanId = redemption.PlanId
		result.PlanTitle = strings.TrimSpace(plan.Title)

		topup := TopUp{
			UserId:          userId,
			Amount:          0,
			Money:           redemption.PayMoney,
			TradeNo:         tradeNo,
			PaymentMethod:   "redemption",
			PaymentProvider: PaymentProviderMall,
			CreateTime:      redeemedAt,
			CompleteTime:    redeemedAt,
			Status:          common.TopUpStatusSuccess,
		}
		if err = tx.Create(&topup).Error; err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("不支持的兑换码奖励类型")
	}

	var totalRecharge float64
	if err := tx.Model(&TopUp{}).
		Where("user_id = ? AND status = ?", userId, common.TopUpStatusSuccess).
		Select("COALESCE(SUM(money), 0)").
		Scan(&totalRecharge).Error; err != nil {
		return nil, err
	}

	var err error
	applyResult.levelChanged, applyResult.upgradedGroup, applyResult.upgradedLevelID, err = applyUserLevelByRechargeTx(tx, userId, totalRecharge)
	if err != nil {
		return nil, err
	}
	return applyResult, nil
}

func Redeem(key string, userId int) (result *RedeemResult, err error) {
	if key == "" {
		return nil, errors.New("未提供兑换码")
	}
	if userId == 0 {
		return nil, errors.New("无效的 user id")
	}
	redemption := &Redemption{}
	var applyResult *redemptionRewardApplyResult
	result = &RedeemResult{}

	keyCol := "`key`"
	if common.UsingPostgreSQL {
		keyCol = `"key"`
	}
	common.RandomSleep()
	err = DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(keyCol+" = ?", key).First(redemption).Error
		if err != nil {
			return errors.New("无效的兑换码")
		}
		if redemption.Status != common.RedemptionCodeStatusEnabled {
			return ErrRedemptionAlreadyRedeemed
		}
		if redemption.ExpiredTime != 0 && redemption.ExpiredTime < common.GetTimestamp() {
			return ErrRedemptionExpired
		}

		codeType := redemption.CodeType
		if codeType == 0 {
			codeType = common.RedemptionCodeTypeNormal
		}
		redeemedAt := common.GetTimestamp()
		if codeType == common.RedemptionCodeTypeWelfare {
			var usageCount int64
			if err := tx.Model(&RedemptionUsage{}).Where("redemption_id = ? AND user_id = ?", redemption.Id, userId).Count(&usageCount).Error; err != nil {
				return err
			}
			if usageCount > 0 {
				return ErrRedemptionAlreadyRedeemed
			}
			tradeNo := fmt.Sprintf("redeem-%d-u%d", redemption.Id, userId)
			applyResult, err = applyRedemptionRewardTx(tx, redemption, userId, result, tradeNo)
			if err != nil {
				return err
			}
			usage := RedemptionUsage{
				RedemptionId:   redemption.Id,
				UserId:         userId,
				Key:            redemption.Key,
				RedeemedTime:   redeemedAt,
				RewardType:     result.RewardType,
				PlanId:         result.PlanId,
				Quota:          result.Quota,
				SubscriptionId: applyResult.subscriptionId,
			}
			if err = tx.Create(&usage).Error; err != nil {
				return ErrRedemptionAlreadyRedeemed
			}
			redemption.RedeemedTime = redeemedAt
			return tx.Model(redemption).Select("redeemed_time").Updates(redemption).Error
		}

		applyResult, err = applyRedemptionRewardTx(tx, redemption, userId, result, fmt.Sprintf("redeem-%d", redemption.Id))
		if err != nil {
			return err
		}
		redemption.RedeemedTime = redeemedAt
		redemption.Status = common.RedemptionCodeStatusUsed
		redemption.UsedUserId = userId
		return tx.Save(redemption).Error
	})
	if err != nil {
		if isRedemptionBusinessError(err) {
			return nil, err
		}
		common.SysError("redemption failed: " + err.Error())
		return nil, ErrRedeemFailed
	}
	if applyResult == nil {
		applyResult = &redemptionRewardApplyResult{}
	}
	if result.RewardType == common.RedemptionRewardTypeSubscription && applyResult.upgradedSubscriptionGroup != "" {
		_ = UpdateUserGroupCache(userId, applyResult.upgradedSubscriptionGroup)
	}
	if result.RewardType == common.RedemptionRewardTypeSubscription {
		if result.PlanTitle != "" {
			RecordLog(userId, LogTypeTopup, fmt.Sprintf("通过兑换码开通订阅，套餐 %s，兑换码ID %d", result.PlanTitle, redemption.Id))
		} else {
			RecordLog(userId, LogTypeTopup, fmt.Sprintf("通过兑换码开通订阅，套餐ID %d，兑换码ID %d", result.PlanId, redemption.Id))
		}
	} else {
		RecordLog(userId, LogTypeTopup, fmt.Sprintf("通过兑换码充值 %s，兑换码ID %d", logger.LogQuota(redemption.Quota), redemption.Id))
	}
	if applyResult.levelChanged {
		_ = invalidateUserCache(userId)
		RecordLog(userId, LogTypeManage, fmt.Sprintf("累计充值达标，用户等级自动升级为 %s (#%d)", applyResult.upgradedGroup, applyResult.upgradedLevelID))
	}
	return result, nil
}

func (redemption *Redemption) Insert() error {
	var err error
	err = DB.Create(redemption).Error
	return err
}

func (redemption *Redemption) SelectUpdate() error {
	// This can update zero values
	return DB.Model(redemption).Select("redeemed_time", "status").Updates(redemption).Error
}

// Update Make sure your token's fields is completed, because this will update non-zero values
func (redemption *Redemption) Update() error {
	var err error
	err = DB.Model(redemption).Select("name", "status", "code_type", "reward_type", "quota", "plan_id", "pay_money", "redeemed_time", "expired_time").Updates(redemption).Error
	return err
}

func (redemption *Redemption) Delete() error {
	var err error
	err = DB.Delete(redemption).Error
	return err
}

func DeleteRedemptionById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	redemption := Redemption{Id: id}
	err = DB.Where(redemption).First(&redemption).Error
	if err != nil {
		return err
	}
	return redemption.Delete()
}

func DeleteInvalidRedemptions() (int64, error) {
	now := common.GetTimestamp()
	result := DB.Where("status IN ? OR (status = ? AND expired_time != 0 AND expired_time < ?)", []int{common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusDisabled}, common.RedemptionCodeStatusEnabled, now).Delete(&Redemption{})
	return result.RowsAffected, result.Error
}
