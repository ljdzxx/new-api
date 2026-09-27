package service

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestModelHealthSelectionAndRetry(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory=%v", memory), func(t *testing.T) {
			oldDB, oldMemory, oldRedis := model.DB, common.MemoryCacheEnabled, common.RedisEnabled
			oldSetting := *operation_setting.GetMonitorSetting()
			t.Cleanup(func() {
				model.DB = oldDB
				common.MemoryCacheEnabled = oldMemory
				common.RedisEnabled = oldRedis
				*operation_setting.GetMonitorSetting() = oldSetting
			})
			common.RedisEnabled = false
			common.MemoryCacheEnabled = memory
			t.Setenv("SQL_DSN", "local")
			oldPath, oldMaster := common.SQLitePath, common.IsMasterNode
			common.SQLitePath, common.IsMasterNode = ":memory:", false
			t.Cleanup(func() { common.SQLitePath = oldPath; common.IsMasterNode = oldMaster })
			require.NoError(t, model.InitDB())
			bootstrap, err := model.DB.DB()
			require.NoError(t, err)
			require.NoError(t, bootstrap.Close())
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { sqlDB.Close() })
			model.DB = db
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.ChannelDailyMark{}))
			settings := operation_setting.GetMonitorSetting()
			settings.ModelHealthEnabled = true
			settings.ModelHealthThreshold = 2
			settings.ModelHealthStatusCodes = "429,502,503"
			group := "health-test"
			high := &model.Channel{Id: 94001, Type: 1, Name: "high", Models: "health-a,health-b", Group: group, Status: 1, Priority: common.GetPointer(int64(10))}
			low := &model.Channel{Id: 94002, Type: 1, Name: "low", Models: "health-a,health-b", Group: group, Status: 1, Priority: common.GetPointer(int64(1))}
			for _, ch := range []*model.Channel{high, low} {
				require.NoError(t, model.PruneChannelModelHealth(ch.Id, nil))
				require.NoError(t, ch.Insert())
			}
			model.InitChannelCache()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			// Build candidates before the state changes to exercise the recheck.
			param := &RetryParam{Ctx: c, TokenGroup: group, ModelName: "health-a"}
			require.NoError(t, param.buildRetryCandidates())
			for i := 0; i < 2; i++ {
				a, err := model.BeginModelHealthAttempt(high, "health-a", false)
				require.NoError(t, err)
				require.NoError(t, a.Record(502))
			}
			selected, err := selectChannelWithModelHealth(c, group, "health-a", 0, nil)
			require.NoError(t, err)
			require.Equal(t, low.Id, selected.Id)
			selected, _, err = GetNextRetryChannel(param)
			require.NoError(t, err)
			require.Equal(t, low.Id, selected.Id)
			require.ErrorIs(t, CheckModelHealthForRequest(c, high, "health-a"), model.ErrModelUnavailable)
			selected, err = selectChannelWithModelHealth(c, group, "health-b", 0, nil)
			require.NoError(t, err)
			require.Equal(t, high.Id, selected.Id)
			c.Request = httptest.NewRequest("POST", "/v1/videos", nil)
			require.NoError(t, CheckModelHealthForRequest(c, high, "health-a"))
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			restore, err := model.BeginModelHealthAttempt(high, "health-a", true)
			require.NoError(t, err)
			require.NoError(t, restore.Recover(1))
			selected, err = selectChannelWithModelHealth(c, group, "health-a", 0, nil)
			require.NoError(t, err)
			require.Equal(t, high.Id, selected.Id)
		})
	}
}
