package setting

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var ModelRequestRateLimitEnabled = false
var ModelRequestRateLimitDurationMinutes = 1
var ModelRequestRateLimitCount = 0
var ModelRequestRateLimitSuccessCount = 1000
var ModelRequestRateLimitGroup = map[string][2]int{}
var ModelRequestRateLimitMutex sync.RWMutex

type GroupUserRateLimit struct {
	Max     *int `json:"max,omitempty"`
	Success *int `json:"success,omitempty"`
}

var groupRateLimit = map[string]GroupUserRateLimit{}
var groupRateLimitMutex sync.RWMutex

func ModelRequestRateLimitGroup2JSONString() string {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(ModelRequestRateLimitGroup)
	if err != nil {
		common.SysLog("error marshalling model ratio: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateModelRequestRateLimitGroupByJSONString(jsonStr string) error {
	ModelRequestRateLimitMutex.Lock()
	defer ModelRequestRateLimitMutex.Unlock()

	ModelRequestRateLimitGroup = make(map[string][2]int)
	return common.UnmarshalJsonStr(jsonStr, &ModelRequestRateLimitGroup)
}

func GetGroupRateLimit(group string) (totalCount, successCount int, found bool) {
	ModelRequestRateLimitMutex.RLock()
	defer ModelRequestRateLimitMutex.RUnlock()

	if ModelRequestRateLimitGroup == nil {
		return 0, 0, false
	}

	limits, found := ModelRequestRateLimitGroup[group]
	if !found {
		return 0, 0, false
	}
	return limits[0], limits[1], true
}

func CheckModelRequestRateLimitGroup(jsonStr string) error {
	checkModelRequestRateLimitGroup := make(map[string][2]int)
	err := common.UnmarshalJsonStr(jsonStr, &checkModelRequestRateLimitGroup)
	if err != nil {
		return err
	}
	for group, limits := range checkModelRequestRateLimitGroup {
		if limits[0] < 0 || limits[1] < 1 {
			return fmt.Errorf("group %s has negative rate limit values: [%d, %d]", group, limits[0], limits[1])
		}
		if limits[0] > math.MaxInt32 || limits[1] > math.MaxInt32 {
			return fmt.Errorf("group %s [%d, %d] has max rate limits value 2147483647", group, limits[0], limits[1])
		}
	}

	return nil
}

func GroupRateLimit2JSONString() string {
	groupRateLimitMutex.RLock()
	defer groupRateLimitMutex.RUnlock()

	jsonBytes, err := common.Marshal(groupRateLimit)
	if err != nil {
		common.SysLog("error marshalling group rate limit: " + err.Error())
		return "{}"
	}
	return string(jsonBytes)
}

func UpdateGroupRateLimitByJSONString(jsonStr string) error {
	if err := CheckGroupRateLimit(jsonStr); err != nil {
		return err
	}
	parsed := make(map[string]GroupUserRateLimit)
	if err := common.UnmarshalJsonStr(jsonStr, &parsed); err != nil {
		return err
	}

	groupRateLimitMutex.Lock()
	defer groupRateLimitMutex.Unlock()
	groupRateLimit = parsed
	return nil
}

func GetGroupUserRateLimit(group string) (GroupUserRateLimit, bool) {
	groupRateLimitMutex.RLock()
	defer groupRateLimitMutex.RUnlock()

	rate, found := groupRateLimit[group]
	return rate, found
}

func CheckGroupRateLimit(jsonStr string) error {
	parsed := make(map[string]map[string]json.RawMessage)
	if err := common.UnmarshalJsonStr(jsonStr, &parsed); err != nil {
		return err
	}

	for group, fields := range parsed {
		if strings.TrimSpace(group) == "" {
			return fmt.Errorf("group name cannot be empty")
		}
		maxJSON, hasMax := fields["max"]
		successJSON, hasSuccess := fields["success"]
		if !hasMax && !hasSuccess {
			return fmt.Errorf("group %s must define max or success", group)
		}
		if hasMax {
			var maxValue int
			if common.GetJsonType(maxJSON) != "number" || common.Unmarshal(maxJSON, &maxValue) != nil || maxValue < 0 || maxValue > math.MaxInt32 {
				return fmt.Errorf("group %s max must be between 0 and %d", group, math.MaxInt32)
			}
		}
		if hasSuccess {
			var successValue int
			if common.GetJsonType(successJSON) != "number" || common.Unmarshal(successJSON, &successValue) != nil || successValue < 1 || successValue > math.MaxInt32 {
				return fmt.Errorf("group %s success must be between 1 and %d", group, math.MaxInt32)
			}
		}
	}
	return nil
}
