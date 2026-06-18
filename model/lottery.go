package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	LotteryPeriodStatusDraft   = "draft"
	LotteryPeriodStatusDrawing = "drawing"
	LotteryPeriodStatusDrawn   = "drawn"
)

const (
	LotteryCodeStatusUnused   = 1
	LotteryCodeStatusAssigned = 2
)

type LotteryPeriod struct {
	Id             int    `json:"id"`
	Issue          int    `json:"issue" gorm:"uniqueIndex"`
	Title          string `json:"title" gorm:"type:varchar(128);default:''"`
	StartTime      int64  `json:"start_time" gorm:"index"`
	EndTime        int64  `json:"end_time" gorm:"index"`
	DisplayEnabled bool   `json:"display_enabled" gorm:"default:false;index"`
	Status         string `json:"status" gorm:"type:varchar(32);default:'draft';index"`
	DrawTime       int64  `json:"draw_time"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`

	PrizeCount  int64 `json:"prize_count" gorm:"-"`
	CodeCount   int64 `json:"code_count" gorm:"-"`
	EntryCount  int64 `json:"entry_count" gorm:"-"`
	WinnerCount int64 `json:"winner_count" gorm:"-"`
}

type LotteryPrize struct {
	Id          int    `json:"id"`
	PeriodId    int    `json:"period_id" gorm:"index"`
	LevelName   string `json:"level_name" gorm:"type:varchar(64)"`
	Quantity    int    `json:"quantity"`
	Description string `json:"description" gorm:"type:text"`
	PaidOnly    bool   `json:"paid_only" gorm:"default:false;index"`
	SortOrder   int    `json:"sort_order"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`

	CodeCount int64 `json:"code_count" gorm:"-"`
}

type LotteryPrizeCode struct {
	Id         int    `json:"id"`
	PeriodId   int    `json:"period_id" gorm:"index"`
	PrizeId    int    `json:"prize_id" gorm:"index"`
	Code       string `json:"code" gorm:"type:text"`
	Status     int    `json:"status" gorm:"default:1;index"`
	WinnerId   int    `json:"winner_id" gorm:"index"`
	CreatedAt  int64  `json:"created_at"`
	AssignedAt int64  `json:"assigned_at"`
}

type LotteryEntry struct {
	Id        int    `json:"id"`
	PeriodId  int    `json:"period_id" gorm:"uniqueIndex:idx_lottery_entry_period_user"`
	UserId    int    `json:"user_id" gorm:"uniqueIndex:idx_lottery_entry_period_user;index"`
	Username  string `json:"username" gorm:"type:varchar(64)"`
	CreatedAt int64  `json:"created_at"`
}

type LotteryWinner struct {
	Id               int    `json:"id"`
	PeriodId         int    `json:"period_id" gorm:"uniqueIndex:idx_lottery_winner_period_user;index"`
	PrizeId          int    `json:"prize_id" gorm:"index"`
	CodeId           int    `json:"code_id" gorm:"uniqueIndex"`
	UserId           int    `json:"user_id" gorm:"uniqueIndex:idx_lottery_winner_period_user;index"`
	Username         string `json:"username" gorm:"type:varchar(64)"`
	PrizeName        string `json:"prize_name" gorm:"type:varchar(64)"`
	PrizeDescription string `json:"prize_description" gorm:"type:text"`
	Code             string `json:"code" gorm:"type:text"`
	CodeScratched    bool   `json:"code_scratched" gorm:"default:false;index"`
	CodeScratchedAt  int64  `json:"code_scratched_at"`
	CreatedAt        int64  `json:"created_at"`
}

type LotteryPublicPeriod struct {
	Period      LotteryPeriod   `json:"period"`
	Prizes      []LotteryPrize  `json:"prizes"`
	Winners     []LotteryWinner `json:"winners"`
	SelfEntry   *LotteryEntry   `json:"self_entry,omitempty"`
	SelfWinner  *LotteryWinner  `json:"self_winner,omitempty"`
	CurrentTime int64           `json:"current_time"`
}

func (p *LotteryPeriod) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if p.CreatedAt == 0 {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.Status == "" {
		p.Status = LotteryPeriodStatusDraft
	}
	return nil
}

func (p *LotteryPeriod) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

func (p *LotteryPrize) BeforeCreate(tx *gorm.DB) error {
	now := common.GetTimestamp()
	if p.CreatedAt == 0 {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	return nil
}

func (p *LotteryPrize) BeforeUpdate(tx *gorm.DB) error {
	p.UpdatedAt = common.GetTimestamp()
	return nil
}

func (c *LotteryPrizeCode) BeforeCreate(tx *gorm.DB) error {
	if c.CreatedAt == 0 {
		c.CreatedAt = common.GetTimestamp()
	}
	if c.Status == 0 {
		c.Status = LotteryCodeStatusUnused
	}
	return nil
}

func (e *LotteryEntry) BeforeCreate(tx *gorm.DB) error {
	if e.CreatedAt == 0 {
		e.CreatedAt = common.GetTimestamp()
	}
	return nil
}

func (w *LotteryWinner) BeforeCreate(tx *gorm.DB) error {
	if w.CreatedAt == 0 {
		w.CreatedAt = common.GetTimestamp()
	}
	return nil
}

func NextLotteryIssue() (int, error) {
	var maxIssue int
	err := DB.Model(&LotteryPeriod{}).Select("COALESCE(MAX(issue), 0)").Scan(&maxIssue).Error
	return maxIssue + 1, err
}

func FillLotteryPeriodStats(periods []LotteryPeriod) []LotteryPeriod {
	for i := range periods {
		FillLotteryPeriodStat(&periods[i])
	}
	return periods
}

func FillLotteryPeriodStat(period *LotteryPeriod) {
	if period == nil || period.Id <= 0 {
		return
	}
	_ = DB.Model(&LotteryPrize{}).Where("period_id = ?", period.Id).Select("COALESCE(SUM(quantity), 0)").Scan(&period.PrizeCount).Error
	_ = DB.Model(&LotteryPrizeCode{}).Where("period_id = ?", period.Id).Count(&period.CodeCount).Error
	_ = DB.Model(&LotteryEntry{}).Where("period_id = ?", period.Id).Count(&period.EntryCount).Error
	_ = DB.Model(&LotteryWinner{}).Where("period_id = ?", period.Id).Count(&period.WinnerCount).Error
}

func FillLotteryPrizeCodeCounts(prizes []LotteryPrize) []LotteryPrize {
	for i := range prizes {
		_ = DB.Model(&LotteryPrizeCode{}).Where("prize_id = ?", prizes[i].Id).Count(&prizes[i].CodeCount).Error
	}
	return prizes
}

func IsPaidUser(userId int) bool {
	if userId <= 0 {
		return false
	}
	var count int64
	if err := DB.Model(&TopUp{}).
		Where("user_id = ? AND status = ? AND money > ?", userId, common.TopUpStatusSuccess, 0).
		Count(&count).Error; err != nil {
		return false
	}
	if count == 0 {
		return false
	}
	var user User
	if err := DB.Select("quota").Where("id = ?", userId).First(&user).Error; err != nil {
		return false
	}
	if user.Quota > 0 {
		return true
	}
	hasActiveSubscription, err := HasActiveUserSubscription(userId)
	if err != nil {
		return false
	}
	return hasActiveSubscription
}

func GetLotteryPublicPeriod(periodId int, userId int) (*LotteryPublicPeriod, error) {
	var period LotteryPeriod
	if err := DB.Where("id = ? AND display_enabled = ?", periodId, true).First(&period).Error; err != nil {
		return nil, err
	}
	FillLotteryPeriodStat(&period)

	var prizes []LotteryPrize
	if err := DB.Where("period_id = ?", period.Id).Order("sort_order asc, id asc").Find(&prizes).Error; err != nil {
		return nil, err
	}
	prizes = FillLotteryPrizeCodeCounts(prizes)

	var winners []LotteryWinner
	if period.Status == LotteryPeriodStatusDrawn {
		if err := DB.Where("period_id = ?", period.Id).Order("id asc").Find(&winners).Error; err != nil {
			return nil, err
		}
		for i := range winners {
			winners[i].Code = ""
			winners[i].CodeScratched = false
			winners[i].CodeScratchedAt = 0
		}
	}

	result := &LotteryPublicPeriod{
		Period:      period,
		Prizes:      prizes,
		Winners:     winners,
		CurrentTime: common.GetTimestamp(),
	}
	if userId > 0 {
		var entry LotteryEntry
		if err := DB.Where("period_id = ? AND user_id = ?", period.Id, userId).First(&entry).Error; err == nil {
			result.SelfEntry = &entry
		}
		var winner LotteryWinner
		if err := DB.Where("period_id = ? AND user_id = ?", period.Id, userId).First(&winner).Error; err == nil {
			result.SelfWinner = &winner
		}
	}
	return result, nil
}

func NormalizeLotteryCodes(raw string) []string {
	lines := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';' || r == '\t'
	})
	codes := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		code := strings.TrimSpace(line)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	return codes
}
