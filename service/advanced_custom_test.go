package service

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAdvancedCustomRoutingFiltersBeforePriorityAndRetries(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory=%v", memory), func(t *testing.T) {
			oldDB, oldMemory, oldRedis := model.DB, common.MemoryCacheEnabled, common.RedisEnabled
			oldSettings := *operation_setting.GetMonitorSetting()
			oldPath, oldMaster := common.SQLitePath, common.IsMasterNode
			t.Cleanup(func() {
				model.DB = oldDB
				common.MemoryCacheEnabled = oldMemory
				common.RedisEnabled = oldRedis
				*operation_setting.GetMonitorSetting() = oldSettings
				common.SQLitePath = oldPath
				common.IsMasterNode = oldMaster
			})
			common.MemoryCacheEnabled = memory
			common.RedisEnabled = false
			common.SQLitePath = ":memory:"
			common.IsMasterNode = false
			t.Setenv("SQL_DSN", "local")
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
			operation_setting.GetMonitorSetting().ModelHealthEnabled = false
			advanced := &model.Channel{Id: 95001, Type: constant.ChannelTypeAdvancedCustom, Name: "advanced", Models: "route-model", Group: "route-test", Status: 1, Priority: common.GetPointer(int64(10))}
			advanced.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1beta/models/{model}:generateContent", UpstreamPath: "/v1/responses", Converter: dto.AdvancedCustomGeminiToResponses}}}})
			fallback := &model.Channel{Id: 95002, Type: constant.ChannelTypeOpenAI, Name: "openai", Models: "route-model", Group: "route-test", Status: 1, Priority: common.GetPointer(int64(1))}
			require.NoError(t, advanced.Insert())
			require.NoError(t, fallback.Insert())
			model.InitChannelCache()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			selected, err := selectChannelWithModelHealth(c, "route-test", "route-model", 0, nil)
			require.NoError(t, err)
			require.Equal(t, fallback.Id, selected.Id)
			param := &RetryParam{Ctx: c, TokenGroup: "route-test", ModelName: "route-model"}
			next, _, err := GetNextRetryChannel(param)
			require.NoError(t, err)
			require.Equal(t, fallback.Id, next.Id)
			require.ErrorIs(t, CheckModelHealthForRequest(c, advanced, "route-model"), model.ErrModelUnavailable)
			c.Request = httptest.NewRequest("POST", "/v1beta/models/route-model:streamGenerateContent", nil)
			selected, err = selectChannelWithModelHealth(c, "route-test", "route-model", 0, nil)
			require.NoError(t, err)
			require.Equal(t, advanced.Id, selected.Id)
		})
	}
}

func TestAdvancedCustomSearchUsesUpstreamToolPrice(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1beta/models/gemini-test:generateContent", nil)
	info := &relaycommon.RelayInfo{OriginModelName: "gemini-test", StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAdvancedCustom, UpstreamModelName: "gpt-5"}, PriceData: types.PriceData{UsePrice: true, ModelPrice: 0, GlobalModelRatio: 1, GlobalRatioConfigSnapshot: true, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	info.SetBillableToolCount(dto.BuildInToolWebSearchPreview, 2)
	summary := calculateTextQuotaSummary(c, info, &dto.Usage{})
	require.Equal(t, 10.0, summary.WebSearchPrice)
	require.Equal(t, 2, summary.WebSearchCallCount)
	require.Zero(t, summary.GeminiSearchCallCount)
	require.EqualValues(t, 2, billingPolicyToolUsage(c, summary)[billing_policy.ToolWebSearchStandard])
	require.NotContains(t, billingPolicyToolUsage(c, summary), billing_policy.ToolWebSearchPremium)
}
