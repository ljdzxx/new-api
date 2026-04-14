package model

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm/clause"
)

const ChannelDailyMarkTypeQuotaInsufficient = "quota_insufficient"

type ChannelDailyMark struct {
	ChannelID int    `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	MarkType  string `json:"mark_type" gorm:"primaryKey;autoIncrement:false;type:varchar(64)"`
	ExpireAt  int64  `json:"expire_at" gorm:"bigint;index"`
	Reason    string `json:"reason" gorm:"type:varchar(255)"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func nextDayFivePastMidnightTS(now time.Time) int64 {
	loc := now.Location()
	next := now.In(loc).AddDate(0, 0, 1)
	resetAt := time.Date(next.Year(), next.Month(), next.Day(), 0, 5, 0, 0, loc)
	return resetAt.Unix()
}

func MarkChannelQuotaInsufficientDaily(channelID int, reason string) error {
	if channelID <= 0 || DB == nil {
		return nil
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 255 {
		reason = reason[:255]
	}
	expireAt := nextDayFivePastMidnightTS(time.Now())
	now := time.Now()
	mark := ChannelDailyMark{
		ChannelID: channelID,
		MarkType:  ChannelDailyMarkTypeQuotaInsufficient,
		ExpireAt:  expireAt,
		Reason:    reason,
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "channel_id"},
			{Name: "mark_type"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"expire_at":  expireAt,
			"reason":     reason,
			"updated_at": now,
		}),
	}).Create(&mark).Error
}

func ClearChannelQuotaInsufficientDailyMark(channelID int) error {
	if channelID <= 0 || DB == nil {
		return nil
	}
	return DB.Where("channel_id = ? AND mark_type = ?", channelID, ChannelDailyMarkTypeQuotaInsufficient).Delete(&ChannelDailyMark{}).Error
}

func CleanupExpiredChannelDailyMarks() error {
	if DB == nil {
		return nil
	}
	nowTs := time.Now().Unix()
	return DB.Where("expire_at <= ?", nowTs).Delete(&ChannelDailyMark{}).Error
}

func GetQuotaInsufficientMarkedSet(channelIDs []int) (map[int]struct{}, error) {
	result := make(map[int]struct{})
	if len(channelIDs) == 0 || DB == nil {
		return result, nil
	}
	nowTs := time.Now().Unix()
	var markedIDs []int
	err := DB.Model(&ChannelDailyMark{}).
		Where("mark_type = ? AND expire_at > ? AND channel_id IN ?", ChannelDailyMarkTypeQuotaInsufficient, nowTs, channelIDs).
		Pluck("channel_id", &markedIDs).Error
	if err != nil {
		return nil, err
	}
	for _, id := range markedIDs {
		result[id] = struct{}{}
	}
	return result, nil
}

func FilterOutQuotaInsufficientMarkedChannels(channels []*Channel) ([]*Channel, error) {
	if len(channels) == 0 {
		return channels, nil
	}
	ids := make([]int, 0, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		ids = append(ids, channel.Id)
	}
	markedSet, err := GetQuotaInsufficientMarkedSet(ids)
	if err != nil {
		return channels, err
	}
	if len(markedSet) == 0 {
		return channels, nil
	}
	filtered := make([]*Channel, 0, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		if _, marked := markedSet[channel.Id]; marked {
			continue
		}
		filtered = append(filtered, channel)
	}
	return filtered, nil
}

func FilterOutQuotaInsufficientMarkedChannelIDs(channelIDs []int) ([]int, error) {
	if len(channelIDs) == 0 {
		return channelIDs, nil
	}
	markedSet, err := GetQuotaInsufficientMarkedSet(channelIDs)
	if err != nil {
		return channelIDs, err
	}
	if len(markedSet) == 0 {
		return channelIDs, nil
	}
	filtered := make([]int, 0, len(channelIDs))
	for _, id := range channelIDs {
		if _, marked := markedSet[id]; marked {
			continue
		}
		filtered = append(filtered, id)
	}
	return filtered, nil
}

func IsChannelQuotaInsufficientDailyMarked(channelID int) bool {
	if channelID <= 0 {
		return false
	}
	markedSet, err := GetQuotaInsufficientMarkedSet([]int{channelID})
	if err != nil {
		common.SysLog("check channel daily mark failed: " + err.Error())
		return false
	}
	_, marked := markedSet[channelID]
	return marked
}
