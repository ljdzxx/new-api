package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetChannelConsumptionData(t *testing.T) {
	originalDB, originalLogDB := DB, LOG_DB
	originalSQLite, originalMySQL, originalPostgreSQL := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = originalDB, originalLogDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = originalSQLite, originalMySQL, originalPostgreSQL
	})

	mainDB, err := gorm.Open(sqlite.Open("file:channel-consumption-main?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	logDB, err := gorm.Open(sqlite.Open("file:channel-consumption-log?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	DB, LOG_DB = mainDB, logDB
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	require.NoError(t, mainDB.AutoMigrate(&Channel{}))
	require.NoError(t, logDB.AutoMigrate(&Log{}))
	require.NoError(t, mainDB.Create(&Channel{Id: 1, Name: "primary", Key: "test"}).Error)

	logs := []Log{
		{Username: "alice", Type: LogTypeConsume, ChannelId: 1, CreatedAt: 100, PromptTokens: 10, CompletionTokens: 20, Quota: 100},
		{Username: "alice", Type: LogTypeConsume, ChannelId: 1, CreatedAt: 110, PromptTokens: -5, CompletionTokens: 5, Quota: -10},
		{Username: "bob", Type: LogTypeConsume, ChannelId: 2, CreatedAt: 120, PromptTokens: 40, CompletionTokens: 10, Quota: 200},
		{Username: "alice", Type: LogTypeConsume, ChannelId: 1, CreatedAt: 130, PromptTokens: 999, CompletionTokens: 999, Quota: 999, Other: `{"channel_forward_precheck":true}`},
		{Username: "alice", Type: LogTypeRefund, ChannelId: 1, CreatedAt: 140, PromptTokens: 999, CompletionTokens: 999, Quota: 999},
		{Username: "alice", Type: LogTypeConsume, ChannelId: 1, CreatedAt: 201, PromptTokens: 999, CompletionTokens: 999, Quota: 999},
	}
	require.NoError(t, logDB.Create(&logs).Error)

	rows, err := GetChannelConsumptionData(100, 200, "")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, 2, rows[0].ChannelID)
	require.Equal(t, "", rows[0].ChannelName)
	require.True(t, rows[0].Deleted)
	require.Equal(t, int64(1), rows[0].RequestCount)
	require.Equal(t, int64(50), rows[0].TokenUsed)
	require.Equal(t, int64(200), rows[0].Quota)
	require.Equal(t, 1, rows[1].ChannelID)
	require.Equal(t, "primary", rows[1].ChannelName)
	require.False(t, rows[1].Deleted)
	require.Equal(t, int64(2), rows[1].RequestCount)
	require.Equal(t, int64(35), rows[1].TokenUsed)
	require.Equal(t, int64(100), rows[1].Quota)

	rows, err = GetChannelConsumptionData(100, 200, "alice")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 1, rows[0].ChannelID)
}
