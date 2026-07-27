package setting

import (
	"fmt"
	"math"
	"testing"
)

func TestGroupRateLimitValidationAndLookup(t *testing.T) {
	backup := GroupRateLimit2JSONString()
	t.Cleanup(func() {
		_ = UpdateGroupRateLimitByJSONString(backup)
	})

	valid := `{"default":{"max":1000,"success":100},"vip":{"success":500},"unlimited":{"max":0}}`
	if err := CheckGroupRateLimit(valid); err != nil {
		t.Fatalf("CheckGroupRateLimit() error = %v", err)
	}
	if err := UpdateGroupRateLimitByJSONString(valid); err != nil {
		t.Fatalf("UpdateGroupRateLimitByJSONString() error = %v", err)
	}
	if rate, found := GetGroupUserRateLimit("default"); !found || rate.Max == nil || *rate.Max != 1000 || rate.Success == nil || *rate.Success != 100 {
		t.Fatalf("GetGroupUserRateLimit(default) = (%+v, %v), want ({Max:1000 Success:100}, true)", rate, found)
	}
	if rate, found := GetGroupUserRateLimit("vip"); !found || rate.Max != nil || rate.Success == nil || *rate.Success != 500 {
		t.Fatalf("GetGroupUserRateLimit(vip) = (%+v, %v), want success-only rate", rate, found)
	}
	if _, found := GetGroupUserRateLimit("missing"); found {
		t.Fatal("GetGroupUserRateLimit(missing) found = true, want false")
	}

	invalidCases := []string{
		`[]`,
		`{"":{"max":100,"success":10}}`,
		`{"default":{}}`,
		`{"default":{"max":null,"success":10}}`,
		`{"default":{"success":null}}`,
		`{"default":{"max":-1,"success":10}}`,
		`{"default":{"max":100,"success":0}}`,
		`{"default":{"max":1.5,"success":1}}`,
		`{"default":{"max":` + fmt.Sprint(int64(math.MaxInt32)+1) + `,"success":1}}`,
		`{"default":{"max":100,"success":` + fmt.Sprint(int64(math.MaxInt32)+1) + `}}`,
	}
	for _, input := range invalidCases {
		if err := CheckGroupRateLimit(input); err == nil {
			t.Errorf("CheckGroupRateLimit(%s) error = nil, want error", input)
		}
	}
}
