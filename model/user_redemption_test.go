package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetUserRedemptionRecordsOrdersByEffectiveRedeemedTime(t *testing.T) {
	originalDB := DB
	originalSQLite, originalMySQL, originalPostgreSQL := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB = originalDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = originalSQLite, originalMySQL, originalPostgreSQL
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	DB = db
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	require.NoError(t, db.AutoMigrate(&Redemption{}, &RedemptionUsage{}, &SubscriptionPlan{}, &UserSubscription{}))

	require.NoError(t, db.Create(&[]Redemption{
		{Id: 1, Key: "normal", UsedUserId: 7, Status: common.RedemptionCodeStatusUsed, RedeemedTime: 200},
		{Id: 2, Key: "welfare", CodeType: common.RedemptionCodeTypeWelfare, RedeemedTime: 900},
		{Id: 3, Key: "welfare-tie", CodeType: common.RedemptionCodeTypeWelfare, RedeemedTime: 100},
		{Id: 4, Key: "zero-usage-time", UsedUserId: 7, Status: common.RedemptionCodeStatusUsed, RedeemedTime: 250},
		{Id: 5, Key: "other-user", UsedUserId: 8, Status: common.RedemptionCodeStatusUsed, RedeemedTime: 1000},
	}).Error)
	require.NoError(t, db.Create(&[]RedemptionUsage{
		{RedemptionId: 2, UserId: 7, RedeemedTime: 300},
		{RedemptionId: 2, UserId: 8, RedeemedTime: 900},
		{RedemptionId: 3, UserId: 7, RedeemedTime: 300},
		{RedemptionId: 4, UserId: 7, RedeemedTime: 0},
	}).Error)

	// Usage timestamps take precedence; missing/zero timestamps fall back to
	// the redemption row, and equal timestamps use descending IDs for paging.
	for _, status := range []string{"", "quota"} {
		items, total, err := GetUserRedemptionRecords(7, &common.PageInfo{Page: 1, PageSize: 2}, status)
		require.NoError(t, err)
		require.Equal(t, int64(4), total)
		require.Len(t, items, 2)
		require.Equal(t, 3, items[0].Id)
		require.Equal(t, 2, items[1].Id)
		require.Equal(t, int64(300), items[0].RedeemedTime)
		require.Equal(t, int64(300), items[1].RedeemedTime)

		items, total, err = GetUserRedemptionRecords(7, &common.PageInfo{Page: 2, PageSize: 2}, status)
		require.NoError(t, err)
		require.Equal(t, int64(4), total)
		require.Len(t, items, 2)
		require.Equal(t, 4, items[0].Id)
		require.Equal(t, 1, items[1].Id)
		require.Equal(t, int64(250), items[0].RedeemedTime)
		require.Equal(t, int64(200), items[1].RedeemedTime)
	}
}
