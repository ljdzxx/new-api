package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/groupmonitor"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMonitorSuccessWithoutConsumeLogPersistence(t *testing.T) {
	old := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = old })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	RecordConsumeLog(c, 1, RecordConsumeLogParams{Group: "final-group"})
	require.True(t, c.GetBool(groupmonitor.SuccessKey))
	require.Equal(t, "final-group", c.GetString(groupmonitor.GroupKey))
}

func TestMonitorGroupUpdatePreservesConcurrentQuota(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Token{}))
	old, enabled := DB, common.RedisEnabled
	DB, common.RedisEnabled = db, false
	t.Cleanup(func() { DB, common.RedisEnabled = old, enabled; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	token := Token{UserId: 123, Key: "monitor-test-key", Name: "preserved", RemainQuota: 100, Group: "before", UnlimitedQuota: false}
	require.NoError(t, db.Create(&token).Error)
	require.NoError(t, db.Model(&Token{}).Where("id = ?", token.Id).Update("remain_quota", 75).Error)
	require.NoError(t, token.UpdateMonitorGroup("after"))
	var got Token
	require.NoError(t, db.First(&got, token.Id).Error)
	require.Equal(t, 75, got.RemainQuota)
	require.Equal(t, "after", got.Group)
	require.Equal(t, "preserved", got.Name)
}
