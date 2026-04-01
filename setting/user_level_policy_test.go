package setting

import "testing"

func TestUpdateUserLevelPoliciesAndGetters(t *testing.T) {
	backup := UserLevelPolicies2JSONString()
	defer func() {
		if err := UpdateUserLevelPoliciesByJSONString(backup); err != nil {
			t.Fatalf("restore user level policies failed: %v", err)
		}
	}()

	input := `[{"id":1,"level":"Tier 1","recharge":0,"discount":"0","icon":"/t1.png","channel":[],"rate":50,"group_day_limit":"100"},{"id":2,"level":"Tier 2","recharge":500,"discount":"0.1","icon":"/t2.png","channel":["xxx-openai","xxl-gemini"],"rate":100,"group_day_limit":"0"}]`
	if err := UpdateUserLevelPoliciesByJSONString(input); err != nil {
		t.Fatalf("update policies failed: %v", err)
	}

	if got := GetUserLevelDiscountMultiplier("Tier 2"); got != 0.9 {
		t.Fatalf("unexpected discount multiplier, got %v, want 0.9", got)
	}
	if got := GetUserLevelDiscountMultiplier("Unknown"); got != 1 {
		t.Fatalf("unexpected default discount multiplier, got %v, want 1", got)
	}

	if policy, found := GetUserLevelPolicyByID(2); !found || policy.Level != "Tier 2" {
		t.Fatalf("unexpected policy by id, found=%v policy=%+v", found, policy)
	}
	if policy, found := GetHighestUserLevelByRecharge(600); !found || policy.ID != 2 {
		t.Fatalf("unexpected highest level by recharge, found=%v policy=%+v", found, policy)
	}
	if policy, found := GetHighestUserLevelByRecharge(10); !found || policy.ID != 1 {
		t.Fatalf("unexpected base level by recharge, found=%v policy=%+v", found, policy)
	}

	if rate, found := GetUserLevelRateLimit("Tier 1"); !found || rate != 50 {
		t.Fatalf("unexpected rate limit for tier 1, found=%v rate=%d", found, rate)
	}
	if _, found := GetUserLevelRateLimit("Unknown"); found {
		t.Fatalf("unknown level should not have rate limit")
	}
	if dayLimit, found := GetUserLevelGroupDayLimit("Tier 1"); !found || dayLimit != 100 {
		t.Fatalf("unexpected day limit for tier 1, found=%v limit=%v", found, dayLimit)
	}
	if dayLimit, found := GetUserLevelGroupDayLimit("Tier 2"); !found || dayLimit != 0 {
		t.Fatalf("unexpected day limit for tier 2, found=%v limit=%v", found, dayLimit)
	}

	if channels, found := GetUserLevelAllowedChannels("Tier 1"); !found || len(channels) != 0 {
		t.Fatalf("tier 1 should allow all channels, found=%v channels=%v", found, channels)
	}
	if !IsChannelAllowedForUserLevel("Tier 1", "any-channel") {
		t.Fatalf("tier 1 should allow any channel")
	}

	if !IsChannelAllowedForUserLevel("Tier 2", "xxx-openai") {
		t.Fatalf("tier 2 should allow configured channel")
	}
	if IsChannelAllowedForUserLevel("Tier 2", "other-channel") {
		t.Fatalf("tier 2 should block non-configured channel")
	}
}

func TestCheckUserLevelPoliciesValidation(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{
			name:  "duplicate level",
			input: `[{"id":1,"level":"Tier 1"},{"id":2,"level":"Tier 1"}]`,
		},
		{
			name:  "duplicate id",
			input: `[{"id":1,"level":"Tier 1"},{"id":1,"level":"Tier 2"}]`,
		},
		{
			name:  "invalid discount",
			input: `[{"id":1,"level":"Tier 1","discount":"abc"}]`,
		},
		{
			name:  "discount out of range",
			input: `[{"id":1,"level":"Tier 1","discount":"1.2"}]`,
		},
		{
			name:  "invalid recharge type",
			input: `[{"id":1,"level":"Tier 1","recharge":"abc"}]`,
		},
		{
			name:  "negative recharge",
			input: `[{"id":1,"level":"Tier 1","recharge":-1}]`,
		},
		{
			name:  "invalid channel type",
			input: `[{"id":1,"level":"Tier 1","channel":"not-array"}]`,
		},
		{
			name:  "invalid rate type",
			input: `[{"id":1,"level":"Tier 1","rate":"1.2"}]`,
		},
		{
			name:  "negative rate",
			input: `[{"id":1,"level":"Tier 1","rate":-1}]`,
		},
		{
			name:  "invalid group_day_limit type",
			input: `[{"id":1,"level":"Tier 1","group_day_limit":"abc"}]`,
		},
		{
			name:  "negative group_day_limit",
			input: `[{"id":1,"level":"Tier 1","group_day_limit":-1}]`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckUserLevelPolicies(tc.input); err == nil {
				t.Fatalf("expected validation error, got nil")
			}
		})
	}
}
