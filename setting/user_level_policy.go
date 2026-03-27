package setting

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

const defaultUserLevelPoliciesJSON = `[{"id":1,"level":"Tier 1","icon":"t1.png","discount":"0","channel":[],"rate":50,"recharge":"0","group_day_limit":"100"},{"id":2,"level":"Tier 2","icon":"t2.png","discount":"0.1","channel":[],"rate":100,"recharge":"500","group_day_limit":"0"},{"id":3,"level":"Tier 3","icon":"t3.png","discount":"0.2","channel":[],"rate":500,"recharge":"2000","group_day_limit":"0"},{"id":4,"level":"Tier 4","icon":"t4.png","discount":"0.4","channel":[],"rate":0,"recharge":"10000","group_day_limit":"0"}]`

type UserLevelPolicy struct {
	ID            int      `json:"id"`
	Level         string   `json:"level"`
	Recharge      float64  `json:"recharge"`
	Discount      string   `json:"discount"`
	Icon          string   `json:"icon"`
	Channel       []string `json:"channel"`
	Rate          int      `json:"rate"`
	GroupDayLimit string   `json:"group_day_limit"`
}

var (
	userLevelPolicies      = make([]UserLevelPolicy, 0)
	userLevelPolicyMap     = make(map[string]UserLevelPolicy)
	userLevelPolicyIDMap   = make(map[int]UserLevelPolicy)
	userLevelPoliciesMutex sync.RWMutex
)

func init() {
	if err := UpdateUserLevelPoliciesByJSONString(defaultUserLevelPoliciesJSON); err != nil {
		panic("failed to initialize user level policies: " + err.Error())
	}
}

func UserLevelPolicies2JSONString() string {
	userLevelPoliciesMutex.RLock()
	defer userLevelPoliciesMutex.RUnlock()

	data, err := common.Marshal(userLevelPolicies)
	if err != nil {
		common.SysLog("error marshalling user level policies: " + err.Error())
		return "[]"
	}
	return string(data)
}

func ListUserLevelPolicies() []UserLevelPolicy {
	userLevelPoliciesMutex.RLock()
	defer userLevelPoliciesMutex.RUnlock()
	result := make([]UserLevelPolicy, len(userLevelPolicies))
	copy(result, userLevelPolicies)
	return result
}

func UpdateUserLevelPoliciesByJSONString(jsonStr string) error {
	policies, err := parseAndNormalizeUserLevelPolicies(jsonStr)
	if err != nil {
		return err
	}

	userLevelPoliciesMutex.Lock()
	defer userLevelPoliciesMutex.Unlock()

	userLevelPolicies = policies
	userLevelPolicyMap = make(map[string]UserLevelPolicy, len(policies))
	userLevelPolicyIDMap = make(map[int]UserLevelPolicy, len(policies))
	for _, policy := range policies {
		userLevelPolicyMap[policy.Level] = policy
		userLevelPolicyIDMap[policy.ID] = policy
	}
	return nil
}

func CheckUserLevelPolicies(jsonStr string) error {
	_, err := parseAndNormalizeUserLevelPolicies(jsonStr)
	return err
}

func GetUserLevelPolicyByLevel(level string) (UserLevelPolicy, bool) {
	userLevelPoliciesMutex.RLock()
	policy, ok := userLevelPolicyMap[level]
	userLevelPoliciesMutex.RUnlock()
	return policy, ok
}

func GetUserLevelPolicyByID(id int) (UserLevelPolicy, bool) {
	userLevelPoliciesMutex.RLock()
	policy, ok := userLevelPolicyIDMap[id]
	userLevelPoliciesMutex.RUnlock()
	return policy, ok
}

func GetUserLevelPolicyLevelByID(id int) (string, bool) {
	policy, ok := GetUserLevelPolicyByID(id)
	if !ok {
		return "", false
	}
	return policy.Level, true
}

func GetHighestUserLevelByRecharge(totalRecharge float64) (UserLevelPolicy, bool) {
	userLevelPoliciesMutex.RLock()
	defer userLevelPoliciesMutex.RUnlock()

	if len(userLevelPolicies) == 0 {
		return UserLevelPolicy{}, false
	}

	found := false
	best := UserLevelPolicy{}
	for _, policy := range userLevelPolicies {
		if policy.Recharge <= totalRecharge {
			if !found || policy.Recharge > best.Recharge || (policy.Recharge == best.Recharge && policy.ID > best.ID) {
				best = policy
				found = true
			}
		}
	}
	return best, found
}

func GetUserLevelDiscountMultiplier(level string) float64 {
	userLevelPoliciesMutex.RLock()
	policy, ok := userLevelPolicyMap[level]
	userLevelPoliciesMutex.RUnlock()
	if !ok {
		return 1
	}

	discount, err := strconv.ParseFloat(policy.Discount, 64)
	if err != nil {
		return 1
	}
	multiplier := 1 - discount
	if multiplier < 0 {
		return 0
	}
	return multiplier
}

func GetUserLevelDiscountMultiplierByID(id int) float64 {
	level, ok := GetUserLevelPolicyLevelByID(id)
	if !ok {
		return 1
	}
	return GetUserLevelDiscountMultiplier(level)
}

func GetUserLevelAllowedChannels(level string) ([]string, bool) {
	userLevelPoliciesMutex.RLock()
	policy, ok := userLevelPolicyMap[level]
	userLevelPoliciesMutex.RUnlock()
	if !ok {
		return nil, false
	}

	channels := make([]string, 0, len(policy.Channel))
	for _, name := range policy.Channel {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			channels = append(channels, trimmed)
		}
	}
	return channels, true
}

func IsChannelAllowedForUserLevel(level string, channelName string) bool {
	channels, found := GetUserLevelAllowedChannels(level)
	if !found || len(channels) == 0 {
		return true
	}
	for _, name := range channels {
		if name == channelName {
			return true
		}
	}
	return false
}

func IsChannelAllowedForUserLevelID(id int, channelName string) bool {
	level, ok := GetUserLevelPolicyLevelByID(id)
	if !ok {
		return true
	}
	return IsChannelAllowedForUserLevel(level, channelName)
}

func GetUserLevelRateLimit(level string) (int, bool) {
	userLevelPoliciesMutex.RLock()
	policy, ok := userLevelPolicyMap[level]
	userLevelPoliciesMutex.RUnlock()
	if !ok {
		return 0, false
	}
	return policy.Rate, true
}

func GetUserLevelRateLimitByID(id int) (int, bool) {
	level, ok := GetUserLevelPolicyLevelByID(id)
	if !ok {
		return 0, false
	}
	return GetUserLevelRateLimit(level)
}

func GetUserLevelGroupDayLimit(level string) (float64, bool) {
	userLevelPoliciesMutex.RLock()
	policy, ok := userLevelPolicyMap[level]
	userLevelPoliciesMutex.RUnlock()
	if !ok {
		return 0, false
	}
	limit, err := strconv.ParseFloat(strings.TrimSpace(policy.GroupDayLimit), 64)
	if err != nil {
		return 0, false
	}
	if limit < 0 {
		return 0, false
	}
	return limit, true
}

func GetUserLevelGroupDayLimitByID(id int) (float64, bool) {
	level, ok := GetUserLevelPolicyLevelByID(id)
	if !ok {
		return 0, false
	}
	return GetUserLevelGroupDayLimit(level)
}

func parseAndNormalizeUserLevelPolicies(jsonStr string) ([]UserLevelPolicy, error) {
	raw := strings.TrimSpace(jsonStr)
	if raw == "" {
		return []UserLevelPolicy{}, nil
	}

	var rawPolicies []map[string]any
	if err := common.UnmarshalJsonStr(raw, &rawPolicies); err != nil {
		return nil, fmt.Errorf("invalid user level policies json: %w", err)
	}

	levels := make(map[string]struct{}, len(rawPolicies))
	ids := make(map[int]struct{}, len(rawPolicies))
	policies := make([]UserLevelPolicy, 0, len(rawPolicies))
	for idx, item := range rawPolicies {
		level := strings.TrimSpace(common.Interface2String(item["level"]))
		if level == "" {
			return nil, fmt.Errorf("user level policy[%d] level is required", idx)
		}
		if _, exists := levels[level]; exists {
			return nil, fmt.Errorf("user level %s is duplicated", level)
		}
		levels[level] = struct{}{}

		id, err := parseID(item["id"], idx)
		if err != nil {
			return nil, fmt.Errorf("user level %s id invalid: %w", level, err)
		}
		if id <= 0 {
			return nil, fmt.Errorf("user level %s id must be greater than 0", level)
		}
		if _, exists := ids[id]; exists {
			return nil, fmt.Errorf("user level id %d is duplicated", id)
		}
		ids[id] = struct{}{}

		recharge, err := parseRecharge(item["recharge"])
		if err != nil {
			return nil, fmt.Errorf("user level %s recharge invalid: %w", level, err)
		}
		if recharge < 0 {
			return nil, fmt.Errorf("user level %s recharge must be greater than or equal to 0", level)
		}
		recharge = roundRecharge(recharge)

		discountValue, err := parseDiscount(item["discount"])
		if err != nil {
			return nil, fmt.Errorf("user level %s discount invalid: %w", level, err)
		}
		if discountValue < 0 || discountValue > 1 {
			return nil, fmt.Errorf("user level %s discount must be between 0 and 1", level)
		}

		channels, err := parseChannels(item["channel"])
		if err != nil {
			return nil, fmt.Errorf("user level %s channel invalid: %w", level, err)
		}

		rate, err := parseRate(item["rate"])
		if err != nil {
			return nil, fmt.Errorf("user level %s rate invalid: %w", level, err)
		}
		if rate < 0 {
			return nil, fmt.Errorf("user level %s rate must be greater than or equal to 0", level)
		}

		groupDayLimitValue, err := parseGroupDayLimit(item["group_day_limit"])
		if err != nil {
			return nil, fmt.Errorf("user level %s group_day_limit invalid: %w", level, err)
		}
		if groupDayLimitValue < 0 {
			return nil, fmt.Errorf("user level %s group_day_limit must be greater than or equal to 0", level)
		}

		icon := strings.TrimSpace(common.Interface2String(item["icon"]))

		policies = append(policies, UserLevelPolicy{
			ID:            id,
			Level:         level,
			Recharge:      recharge,
			Discount:      strconv.FormatFloat(discountValue, 'f', -1, 64),
			Icon:          icon,
			Channel:       channels,
			Rate:          rate,
			GroupDayLimit: strconv.FormatFloat(groupDayLimitValue, 'f', -1, 64),
		})
	}

	sort.Slice(policies, func(i, j int) bool {
		if policies[i].Recharge != policies[j].Recharge {
			return policies[i].Recharge < policies[j].Recharge
		}
		if policies[i].ID != policies[j].ID {
			return policies[i].ID < policies[j].ID
		}
		return policies[i].Level < policies[j].Level
	})
	return policies, nil
}

func parseID(v any, idx int) (int, error) {
	switch value := v.(type) {
	case nil:
		return idx + 1, nil
	case float64:
		if float64(int(value)) != value {
			return 0, fmt.Errorf("id must be integer")
		}
		return int(value), nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return idx + 1, nil
		}
		return strconv.Atoi(trimmed)
	default:
		return 0, fmt.Errorf("unsupported id type: %T", v)
	}
}

func parseDiscount(v any) (float64, error) {
	switch value := v.(type) {
	case nil:
		return 0, nil
	case float64:
		return value, nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, nil
		}
		return strconv.ParseFloat(trimmed, 64)
	default:
		return 0, fmt.Errorf("unsupported discount type: %T", v)
	}
}

func parseRecharge(v any) (float64, error) {
	switch value := v.(type) {
	case nil:
		return 0, nil
	case float64:
		return value, nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, nil
		}
		return strconv.ParseFloat(trimmed, 64)
	default:
		return 0, fmt.Errorf("unsupported recharge type: %T", v)
	}
}

func parseRate(v any) (int, error) {
	switch value := v.(type) {
	case nil:
		return 0, nil
	case float64:
		if float64(int(value)) != value {
			return 0, fmt.Errorf("rate must be integer")
		}
		return int(value), nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, nil
		}
		return strconv.Atoi(trimmed)
	default:
		return 0, fmt.Errorf("unsupported rate type: %T", v)
	}
}

func parseGroupDayLimit(v any) (float64, error) {
	switch value := v.(type) {
	case nil:
		return 0, nil
	case float64:
		return value, nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, nil
		}
		return strconv.ParseFloat(trimmed, 64)
	default:
		return 0, fmt.Errorf("unsupported group_day_limit type: %T", v)
	}
}

func parseChannels(v any) ([]string, error) {
	if v == nil {
		return []string{}, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("channel must be array")
	}
	channels := make([]string, 0, len(items))
	for _, item := range items {
		name, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("channel entry must be string")
		}
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		channels = append(channels, trimmed)
	}
	return channels, nil
}

func roundRecharge(v float64) float64 {
	return math.Round(v*100) / 100
}
