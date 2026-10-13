package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSyncLegacyModelPrice(t *testing.T) {
	oldDB := model.DB
	backup := billing_policy.MarshalConfig()
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = oldOptions })
	prices := ratio_setting.ModelPrice2JSONString()
	ratios := ratio_setting.ModelRatio2JSONString()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	t.Cleanup(func() {
		model.DB = oldDB
		require.NoError(t, billing_policy.UpdateFromJSON(backup))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(ratios))
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sync-target":0,"covered-model":1,"another-target":2}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	config := billing_policy.NewConfig()
	config.State = billing_policy.StateActive
	config.Policies["covered-*"] = billing_policy.Policy{Version: 1, Mode: "per_request", Currency: "USD", Unit: "per_request", Price: "5"}
	require.NoError(t, model.UpdateBillingPolicyOptionAtomic(common.GetJsonString(config)))
	call := func(handler gin.HandlerFunc, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		handler(c)
		return w
	}
	preview := call(PreviewLegacyModelPrices, `{"models":["sync-target","covered-model","missing"]}`)
	require.Equal(t, http.StatusOK, preview.Code)
	var response struct {
		Data struct {
			Models map[string]struct {
				Configured bool
				Available  bool
			}
		}
	}
	require.NoError(t, common.Unmarshal(preview.Body.Bytes(), &response))
	require.True(t, response.Data.Models["covered-model"].Configured)
	require.True(t, response.Data.Models["sync-target"].Available)
	require.False(t, response.Data.Models["missing"].Available)
	require.Equal(t, http.StatusConflict, call(SyncLegacyModelPrice, `{"model":"covered-model","revision":1}`).Code)
	require.Equal(t, http.StatusConflict, call(SyncLegacyModelPrice, `{"model":"sync-target","revision":0}`).Code)
	require.Equal(t, http.StatusBadRequest, call(SyncLegacyModelPrice, `{"model":"missing","revision":1}`).Code)
	require.Equal(t, http.StatusOK, call(SyncLegacyModelPrice, `{"model":"sync-target","revision":1}`).Code)
	saved := billing_policy.GetConfig()
	require.Equal(t, int64(2), saved.Revision)
	require.Equal(t, "0", saved.Policies["sync-target"].Price)
	require.Equal(t, "5", saved.Policies["covered-*"].Price)
	require.Equal(t, http.StatusConflict, call(SyncLegacyModelPrice, `{"model":"sync-target","revision":2}`).Code)
	// A different process can update the DB before this process reloads config.
	persisted := billing_policy.GetConfig()
	persisted.Revision++
	require.NoError(t, db.Model(&model.Option{}).Where(map[string]interface{}{"key": "ModelBillingPolicy"}).Update("value", common.GetJsonString(persisted)).Error)
	require.Equal(t, http.StatusConflict, call(SyncLegacyModelPrice, `{"model":"another-target","revision":2}`).Code)
	var stored model.Option
	require.NoError(t, db.First(&stored, model.Option{Key: "ModelBillingPolicy"}).Error)
	require.Equal(t, common.GetJsonString(persisted), stored.Value)
	for _, state := range []string{billing_policy.StateLegacy, billing_policy.StateShadow, billing_policy.StatePrepared} {
		saved.State = state
		require.NoError(t, billing_policy.UpdateFromJSON(common.GetJsonString(saved)))
		require.Equal(t, http.StatusConflict, call(SyncLegacyModelPrice, `{"model":"missing","revision":2}`).Code)
	}
}
