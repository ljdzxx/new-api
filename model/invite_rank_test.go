package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResolveInviteRankWindow(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 7, 27, 15, 30, 0, 0, loc)

	today, err := ResolveInviteRankWindow(InviteRankRangeToday, 0, 0, now, loc)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 27, 0, 0, 0, 0, loc).Unix(), today.Start)
	require.Equal(t, time.Date(2026, 7, 28, 0, 0, 0, 0, loc).Unix(), today.End)

	for rangeKey, seconds := range map[string]int64{
		InviteRankRangeDay:   24 * 60 * 60,
		InviteRankRangeWeek:  7 * 24 * 60 * 60,
		InviteRankRangeMonth: 30 * 24 * 60 * 60,
	} {
		window, resolveErr := ResolveInviteRankWindow(rangeKey, 0, 0, now, loc)
		require.NoError(t, resolveErr)
		require.Equal(t, seconds, window.End-window.Start)
	}

	_, err = ResolveInviteRankWindow(InviteRankRangeCustom, 100, 100+InviteRankMaxRangeSeconds, now, loc)
	require.NoError(t, err)
	_, err = ResolveInviteRankWindow(InviteRankRangeCustom, 100, 101+InviteRankMaxRangeSeconds, now, loc)
	require.ErrorIs(t, err, ErrInviteRankRangeTooLarge)
	_, err = ResolveInviteRankWindow(InviteRankRangeCustom, 100, 100, now, loc)
	require.ErrorIs(t, err, ErrInviteRankInvalidRange)
}

func TestGetInviteRankCountsGrantedDistinctInviteesAndUsesHalfOpenWindow(t *testing.T) {
	originalDB := DB
	originalGroupCol := commonGroupCol
	originalSQLite, originalMySQL, originalPostgreSQL := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB = originalDB
		commonGroupCol = originalGroupCol
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = originalSQLite, originalMySQL, originalPostgreSQL
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	commonGroupCol = "`group`"
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	require.NoError(t, db.AutoMigrate(&User{}, &InviteRewardAudit{}))
	require.NoError(t, db.Create(&[]User{
		{Id: 1, Username: "alice", DisplayName: "Alice", Group: "default", Status: 1, AffCode: "aff-alice", AffCount: 10},
		{Id: 2, Username: "bob", DisplayName: "Bob", Group: "vip", Status: 1, AffCode: "aff-bob", AffCount: 20},
		{Id: 3, Username: "carol", DisplayName: "Carol", Group: "default", Status: 1, AffCode: "aff-carol", AffCount: 5},
	}).Error)

	audits := []InviteRewardAudit{
		{InviterId: 1, InviteeId: 11, RewardStatus: InviteRewardAuditStatusGranted, CreatedAt: 100},
		{InviterId: 1, InviteeId: 12, RewardStatus: InviteRewardAuditStatusGranted, CreatedAt: 110},
		{InviterId: 1, InviteeId: 12, RewardStatus: InviteRewardAuditStatusGranted, CreatedAt: 111},
		{InviterId: 1, InviteeId: 13, RewardStatus: InviteRewardAuditStatusDeniedRisk, CreatedAt: 120},
		{InviterId: 2, InviteeId: 21, RewardStatus: InviteRewardAuditStatusGranted, CreatedAt: 115},
		{InviterId: 2, InviteeId: 22, RewardStatus: InviteRewardAuditStatusGranted, CreatedAt: 125},
		{InviterId: 3, InviteeId: 31, RewardStatus: InviteRewardAuditStatusGranted, CreatedAt: 125},
		{InviterId: 3, InviteeId: 32, RewardStatus: InviteRewardAuditStatusGranted, CreatedAt: 200},
	}
	require.NoError(t, db.Create(&audits).Error)

	items, summary, err := GetInviteRank(InviteRankWindow{RangeKey: InviteRankRangeCustom, Start: 100, End: 200})
	require.NoError(t, err)
	require.Len(t, items, 3)
	require.Equal(t, 2, items[0].UserId)
	require.Equal(t, int64(2), items[0].InviteCount)
	require.Equal(t, 1, items[1].UserId)
	require.Equal(t, int64(2), items[1].InviteCount)
	require.Equal(t, 3, items[2].UserId)
	require.Equal(t, int64(1), items[2].InviteCount)
	require.Equal(t, int64(3), summary.InviterCount)
	require.Equal(t, int64(5), summary.TotalInviteCount)
	require.Equal(t, int64(5), summary.Top100InviteCount)
	require.Equal(t, int64(2), summary.TopInviteCount)
	require.Equal(t, 20, items[0].TotalAffCount)
}
