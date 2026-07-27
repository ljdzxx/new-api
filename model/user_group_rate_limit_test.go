package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withGroupRateLimit(t *testing.T, value string) {
	t.Helper()
	backup := setting.GroupRateLimit2JSONString()
	require.NoError(t, setting.UpdateGroupRateLimitByJSONString(value))
	t.Cleanup(func() {
		_ = setting.UpdateGroupRateLimitByJSONString(backup)
	})
}

func TestUserEdit_GroupChangeAppliesConfiguredRateLimit(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[]`)
	withGroupRateLimit(t, `{"vip":{"max":1000,"success":100}}`)

	user := createRegisteredUser(t, "group_rate_edit")
	user.Group = "vip"
	user.RateLimitEnabled = false
	user.RateLimitDurationMinutes = 9
	user.RateLimitCount = 8
	user.RateLimitSuccessCount = 7
	require.NoError(t, user.Edit(false, false))

	updated, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "vip", updated.Group)
	assert.True(t, updated.RateLimitEnabled)
	assert.Equal(t, 1, updated.RateLimitDurationMinutes)
	assert.Equal(t, 1000, updated.RateLimitCount)
	assert.Equal(t, 100, updated.RateLimitSuccessCount)
}

func TestUserEdit_UnchangedGroupKeepsExplicitUserRateLimit(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[]`)
	withGroupRateLimit(t, `{"default":{"max":1000,"success":100}}`)

	user := createRegisteredUser(t, "group_rate_unchanged")
	user.RateLimitEnabled = true
	user.RateLimitDurationMinutes = 2
	user.RateLimitCount = 20
	user.RateLimitSuccessCount = 10
	require.NoError(t, user.Edit(false, false))

	updated, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, 2, updated.RateLimitDurationMinutes)
	assert.Equal(t, 20, updated.RateLimitCount)
	assert.Equal(t, 10, updated.RateLimitSuccessCount)
}

func TestUpdateUserGroupTx_AppliesConfiguredRateLimit(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[]`)
	withGroupRateLimit(t, `{"subscription":{"max":600,"success":60}}`)

	user := createRegisteredUser(t, "group_rate_tx")
	require.NoError(t, updateUserGroupTx(DB, user.Id, "subscription"))

	updated, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "subscription", updated.Group)
	assert.True(t, updated.RateLimitEnabled)
	assert.Equal(t, 1, updated.RateLimitDurationMinutes)
	assert.Equal(t, 600, updated.RateLimitCount)
	assert.Equal(t, 60, updated.RateLimitSuccessCount)
}

func TestUserEdit_GroupChangeAppliesOnlyConfiguredRateLimitField(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[]`)
	withGroupRateLimit(t, `{"limited":{"success":100}}`)

	user := createRegisteredUser(t, "group_rate_partial")
	user.Group = "limited"
	user.RateLimitEnabled = false
	user.RateLimitDurationMinutes = 5
	user.RateLimitCount = 900
	user.RateLimitSuccessCount = 800
	require.NoError(t, user.Edit(false, false))

	updated, err := GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, "limited", updated.Group)
	assert.True(t, updated.RateLimitEnabled)
	assert.Equal(t, 1, updated.RateLimitDurationMinutes)
	assert.Equal(t, 900, updated.RateLimitCount)
	assert.Equal(t, 100, updated.RateLimitSuccessCount)
}
