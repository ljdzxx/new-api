package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResolveConsumptionRankWindow(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 7, 27, 15, 30, 0, 0, loc)

	today, err := ResolveConsumptionRankWindow(ConsumptionRankRangeToday, 0, 0, now, loc)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 7, 27, 0, 0, 0, 0, loc).Unix(), today.Start)
	require.Equal(t, time.Date(2026, 7, 28, 0, 0, 0, 0, loc).Unix(), today.End)

	day, err := ResolveConsumptionRankWindow(ConsumptionRankRangeDay, 0, 0, now, loc)
	require.NoError(t, err)
	require.Equal(t, int64(24*60*60), day.End-day.Start)

	week, err := ResolveConsumptionRankWindow(ConsumptionRankRangeWeek, 0, 0, now, loc)
	require.NoError(t, err)
	require.Equal(t, int64(7*24*60*60), week.End-week.Start)

	_, err = ResolveConsumptionRankWindow(ConsumptionRankRangeCustom, 100, 100+ConsumptionRankMaxRangeSeconds, now, loc)
	require.NoError(t, err)
	_, err = ResolveConsumptionRankWindow(ConsumptionRankRangeCustom, 100, 101+ConsumptionRankMaxRangeSeconds, now, loc)
	require.ErrorIs(t, err, ErrConsumptionRankRangeTooLarge)
	_, err = ResolveConsumptionRankWindow(ConsumptionRankRangeCustom, 100, 100, now, loc)
	require.ErrorIs(t, err, ErrConsumptionRankInvalidRange)
}

func TestGetConsumptionRankAggregatesAndUsesHalfOpenWindow(t *testing.T) {
	originalDB, originalLogDB := DB, LOG_DB
	originalGroupCol := commonGroupCol
	originalSQLite, originalMySQL, originalPostgreSQL := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = originalDB, originalLogDB
		commonGroupCol = originalGroupCol
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = originalSQLite, originalMySQL, originalPostgreSQL
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = db, db
	commonGroupCol = "`group`"
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	require.NoError(t, db.AutoMigrate(&User{}, &Log{}))
	require.NoError(t, db.Create(&[]User{
		{Id: 1, Username: "alice", DisplayName: "Alice", Group: "default", Status: 1, AffCode: "aff-alice"},
		{Id: 2, Username: "bob", DisplayName: "Bob", Group: "vip", Status: 1, AffCode: "aff-bob"},
	}).Error)

	logs := []Log{
		{UserId: 1, Username: "alice", Type: LogTypeConsume, CreatedAt: 100, PromptTokens: 10, CompletionTokens: 20, Quota: 100},
		{UserId: 1, Username: "alice", Type: LogTypeConsume, CreatedAt: 101, PromptTokens: 0, CompletionTokens: 0, Quota: 50},
		{UserId: 1, Username: "alice", Type: LogTypeConsume, CreatedAt: 102, PromptTokens: -10, CompletionTokens: -20, Quota: -50},
		{UserId: 2, Username: "bob", Type: LogTypeConsume, CreatedAt: 103, PromptTokens: 25, CompletionTokens: 25, Quota: 200},
		{UserId: 2, Username: "bob", Type: LogTypeConsume, CreatedAt: 200, PromptTokens: 999, CompletionTokens: 999, Quota: 999},
		{UserId: 1, Username: "alice", Type: LogTypeConsume, CreatedAt: 105, PromptTokens: 999, CompletionTokens: 999, Quota: 999, Other: `{"channel_forward_precheck":true}`},
		{UserId: 1, Username: "alice", Type: LogTypeRefund, CreatedAt: 104, PromptTokens: 999, CompletionTokens: 999, Quota: 999},
	}
	require.NoError(t, db.Create(&logs).Error)

	items, summary, err := GetConsumptionRank(ConsumptionRankWindow{RangeKey: ConsumptionRankRangeCustom, Start: 100, End: 200})
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, 2, items[0].UserId)
	require.Equal(t, int64(50), items[0].TotalTokens)
	require.Equal(t, int64(200), items[0].ConsumedQuota)
	require.Equal(t, int64(1), items[0].RequestCount)
	require.Equal(t, 1, items[1].UserId)
	require.Equal(t, int64(30), items[1].TotalTokens)
	require.Equal(t, int64(150), items[1].ConsumedQuota)
	require.Equal(t, int64(1), items[1].RequestCount)
	require.Equal(t, int64(80), summary.TotalTokens)
	require.Equal(t, int64(350), summary.TotalConsumedQuota)
}
