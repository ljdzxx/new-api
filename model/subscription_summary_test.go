package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionSummariesIncludeDisabledPlanTitle(t *testing.T) {
	setupUserLevelUpgradeE2E(t, `[]`)

	user := createRegisteredUser(t, "disabled_plan_summary")
	plan := &SubscriptionPlan{
		Title:            "Disabled Plan",
		PriceAmount:      10,
		Currency:         "USD",
		DurationUnit:     SubscriptionDurationMonth,
		DurationValue:    1,
		Enabled:          false,
		TotalAmount:      1000,
		QuotaResetPeriod: SubscriptionResetNever,
	}
	require.NoError(t, DB.Create(plan).Error)

	now := common.GetTimestamp()
	subscription := &UserSubscription{
		UserId:      user.Id,
		PlanId:      plan.Id,
		AmountTotal: plan.TotalAmount,
		StartTime:   now,
		EndTime:     now + 3600,
		Status:      "active",
		Source:      "admin",
	}
	require.NoError(t, DB.Create(subscription).Error)

	summaries, err := GetAllActiveUserSubscriptions(user.Id)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, plan.Title, summaries[0].PlanTitle)
	assert.Equal(t, subscription.Id, summaries[0].Subscription.Id)
}
