package controller

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetTokenModels(t *testing.T) {
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldSQLite, oldMySQL, oldPostgres, oldRedis := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL, common.RedisEnabled
	oldSelfUse := operation_setting.SelfUseModeEnabled
	oldGroups, oldAuto := setting.UserUsableGroups2JSONString(), setting.AutoGroups2JsonString()
	oldGroupRatios := ratio_setting.GroupRatio2JSONString()
	oldRatios, oldPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	oldPolicy := billing_policy.MarshalConfig()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL, common.RedisEnabled = oldSQLite, oldMySQL, oldPostgres, oldRedis
		operation_setting.SelfUseModeEnabled = oldSelfUse
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(oldAuto))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatios))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		require.NoError(t, billing_policy.UpdateFromJSON(oldPolicy))
	})
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Ability{}))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","team":"Team","auto":"Auto","retired":"Retired"}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","team","private"]`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"team":1,"private":1}`))
	operation_setting.SelfUseModeEnabled = true
	user := model.User{Id: 1, Username: "token-model-owner", Group: "default"}
	require.NoError(t, db.Create(&user).Error)
	abilities := []model.Ability{
		{Group: "default", Model: "custom-claude", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "shared-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "shared-model", ChannelId: 2, Enabled: true},
		{Group: "default", Model: "disabled-model", ChannelId: 3, Enabled: false},
		{Group: "default", Model: "gpt-4-gizmo-example", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "custom-claude" + ratio_setting.CompactModelSuffix, ChannelId: 1, Enabled: true},
		{Group: "team", Model: "team-model", ChannelId: 1, Enabled: true},
		{Group: "team", Model: "shared-model", ChannelId: 1, Enabled: true},
		{Group: "private", Model: "private-model", ChannelId: 1, Enabled: true},
		{Group: "retired", Model: "retired-model", ChannelId: 1, Enabled: true},
	}
	require.NoError(t, db.Create(&abilities).Error)
	token := seedToken(t, db, user.Id, "models", "token-models-test-key")
	requestModels := func(t *testing.T, id string, userID int) tokenAPIResponse {
		t.Helper()
		ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/token/"+id+"/models", nil, userID)
		ctx.Params = gin.Params{{Key: "id", Value: id}}
		GetTokenModels(ctx)
		require.NotContains(t, recorder.Body.String(), token.Key)
		return decodeAPIResponse(t, recorder)
	}

	for _, tc := range []struct {
		name    string
		group   string
		limited bool
		limits  string
		want    []string
		failure bool
	}{
		{name: "default group", want: []string{"custom-claude", "gpt-4-gizmo-example", "shared-model"}},
		{name: "explicit group", group: "team", want: []string{"shared-model", "team-model"}},
		{name: "auto group union", group: "auto", want: []string{"custom-claude", "gpt-4-gizmo-example", "shared-model", "team-model"}},
		{name: "intersect limits and enabled abilities", limited: true, limits: "custom-claude,team-model,disabled-model,missing-model", want: []string{"custom-claude"}},
		{name: "auto group limits", group: "auto", limited: true, limits: "team-model,private-model", want: []string{"team-model"}},
		{name: "empty limits", limited: true, want: []string{}},
		{name: "no intersection", group: "team", limited: true, limits: "custom-claude", want: []string{}},
		{name: "disabled limits ignored", group: "team", limits: "custom-claude", want: []string{"shared-model", "team-model"}},
		{name: "relay normalized model name", limited: true, limits: "gpt-4-gizmo-*", want: []string{"gpt-4-gizmo-example"}},
		{name: "unusable group", group: "private", failure: true},
		{name: "retired group", group: "retired", failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Model(token).Updates(map[string]any{
				"group": tc.group, "model_limits_enabled": tc.limited, "model_limits": tc.limits,
			}).Error)
			response := requestModels(t, strconv.Itoa(token.Id), user.Id)
			require.Equal(t, !tc.failure, response.Success, response.Message)
			if tc.failure {
				return
			}
			var names []string
			require.NoError(t, common.Unmarshal(response.Data, &names))
			require.Equal(t, tc.want, names)
		})
	}

	t.Run("ownership and invalid identifiers", func(t *testing.T) {
		for _, request := range []struct {
			id   string
			user int
		}{{strconv.Itoa(token.Id), 2}, {"999999", user.Id}, {"invalid", user.Id}, {"0", user.Id}} {
			require.False(t, requestModels(t, request.id, request.user).Success)
		}
	})

	t.Run("pricing and account preference", func(t *testing.T) {
		operation_setting.SelfUseModeEnabled = false
		policy := billing_policy.NewConfig()
		policy.State = billing_policy.StateLegacy
		policyJSON, err := common.Marshal(policy)
		require.NoError(t, err)
		require.NoError(t, billing_policy.UpdateFromJSON(string(policyJSON)))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"shared-model":1}`))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
		require.NoError(t, db.Model(token).Updates(map[string]any{"group": "team", "model_limits_enabled": false}).Error)
		response := requestModels(t, strconv.Itoa(token.Id), user.Id)
		require.True(t, response.Success, response.Message)
		var names []string
		require.NoError(t, common.Unmarshal(response.Data, &names))
		require.Equal(t, []string{"shared-model"}, names)
		userSettings, err := common.Marshal(dto.UserSetting{AcceptUnsetRatioModel: true})
		require.NoError(t, err)
		require.NoError(t, db.Model(&user).Update("setting", string(userSettings)).Error)
		response = requestModels(t, strconv.Itoa(token.Id), user.Id)
		require.True(t, response.Success, response.Message)
		require.NoError(t, common.Unmarshal(response.Data, &names))
		require.Equal(t, []string{"shared-model", "team-model"}, names)
	})

	t.Run("database failure is not an empty success", func(t *testing.T) {
		require.NoError(t, db.Migrator().DropTable(&model.Ability{}))
		require.False(t, requestModels(t, strconv.Itoa(token.Id), user.Id).Success)
	})
}
