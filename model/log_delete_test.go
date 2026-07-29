package model

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDeleteOldLogDeletesAtMostOneBatch(t *testing.T) {
	originalLogDB := LOG_DB
	t.Cleanup(func() { LOG_DB = originalLogDB })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db

	logs := []Log{
		{CreatedAt: 10},
		{CreatedAt: 20},
		{CreatedAt: 30},
		{CreatedAt: 40},
	}
	require.NoError(t, db.Create(&logs).Error)

	deleted, err := DeleteOldLog(context.Background(), 35, 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)

	var oldCount int64
	require.NoError(t, db.Model(&Log{}).Where("created_at < ?", 35).Count(&oldCount).Error)
	require.Equal(t, int64(1), oldCount)

	var newCount int64
	require.NoError(t, db.Model(&Log{}).Where("created_at >= ?", 35).Count(&newCount).Error)
	require.Equal(t, int64(1), newCount)
}

func TestDeleteOldLogHonorsCanceledContext(t *testing.T) {
	originalLogDB := LOG_DB
	t.Cleanup(func() { LOG_DB = originalLogDB })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	deleted, err := DeleteOldLog(ctx, 35, 2)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, deleted)
}
