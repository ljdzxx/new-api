package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_policy"
	"github.com/stretchr/testify/require"
)

func TestBillingPolicyOptionConditionalWrite(t *testing.T) {
	backup := billing_policy.MarshalConfig()
	oldOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = oldOptions })
	require.NoError(t, DB.AutoMigrate(&Option{}))
	t.Cleanup(func() {
		DB.Where(map[string]interface{}{"key": "ModelBillingPolicy"}).Delete(&Option{})
		require.NoError(t, billing_policy.UpdateFromJSON(backup))
	})
	initial := billing_policy.NewConfig()
	initial.State = billing_policy.StateActive
	initial.Policies["existing"] = billing_policy.Policy{Version: 1, Mode: "per_request", Currency: "USD", Unit: "per_request", Price: "1"}
	require.NoError(t, UpdateBillingPolicyOptionAtomic(common.GetJsonString(initial)))
	next := initial
	next.Revision++
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- UpdateBillingPolicyOptionChecked(common.GetJsonString(next), initial.Revision)
		}()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrBillingPolicyRevisionConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	var saved Option
	require.NoError(t, DB.First(&saved, Option{Key: "ModelBillingPolicy"}).Error)
	require.Equal(t, common.GetJsonString(next), saved.Value)
	require.Equal(t, next.Revision, billing_policy.GetConfig().Revision)
}
