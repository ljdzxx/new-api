package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListModelsPricing(t *testing.T) {
	backup := billing_policy.MarshalConfig()
	selfUse := operation_setting.SelfUseModeEnabled
	ratios := ratio_setting.ModelRatio2JSONString()
	prices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, billing_policy.UpdateFromJSON(backup))
		operation_setting.SelfUseModeEnabled = selfUse
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(ratios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prices))
	})
	operation_setting.SelfUseModeEnabled = false
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"legacy-ratio":1}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"legacy-price":0.1}`))

	policy := billing_policy.Policy{
		Version: 1, Mode: "tiered", Currency: "USD", Unit: "per_million_tokens",
		Tiers: []billing_policy.Tier{
			{ID: "base", Priority: 1, Fallback: true, Prices: billing_policy.Prices{Input: "10", Output: "50"}},
		},
	}
	for _, state := range []string{billing_policy.StateActive, billing_policy.StateLegacy, billing_policy.StateShadow, billing_policy.StatePrepared} {
		t.Run(state, func(t *testing.T) {
			config := billing_policy.NewConfig()
			config.State = state
			config.Policies = map[string]billing_policy.Policy{"gpt-6-astra": policy, "custom-*": policy}
			data, err := common.Marshal(config)
			require.NoError(t, err)
			require.NoError(t, billing_policy.UpdateFromJSON(string(data)))

			for _, tc := range []struct {
				model   string
				visible bool
			}{
				{"gpt-6-astra", state == billing_policy.StateActive},
				{"custom-model", state == billing_policy.StateActive},
				{"legacy-ratio", true},
				{"legacy-price", true},
				{"unpriced-model", false},
			} {
				t.Run(tc.model, func(t *testing.T) {
					assert.Equal(t, tc.visible, hasModelPricing(tc.model))
					recorder := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(recorder)
					ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
					common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
					common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{tc.model: true})
					ListModels(ctx, constant.ChannelTypeOpenAI)
					require.Equal(t, http.StatusOK, recorder.Code)
					var response struct {
						Success bool               `json:"success"`
						Data    []dto.OpenAIModels `json:"data"`
					}
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
					require.True(t, response.Success)
					if tc.visible {
						require.Len(t, response.Data, 1)
						assert.Equal(t, tc.model, response.Data[0].Id)
					} else {
						assert.Empty(t, response.Data)
					}
				})
			}
		})
	}
}

func TestFilterCompactVirtualModels(t *testing.T) {
	models := []dto.OpenAIModels{
		{Id: "gpt-5.4"},
		{Id: "gpt-5.4-openai-compact"},
		{Id: "gpt-5.3-codex"},
		{Id: "gpt-5.3-codex-openai-compact"},
	}

	filtered := filterCompactVirtualModels(models)

	assert.Equal(t, []dto.OpenAIModels{
		{Id: "gpt-5.4"},
		{Id: "gpt-5.3-codex"},
	}, filtered)
}
