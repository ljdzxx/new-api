package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// GetTokenAllowedModels intersects the token's model limits with its usable groups.
// Account authentication and token ownership are checked by the caller.
func GetTokenAllowedModels(token *model.Token, userGroup string) ([]string, error) {
	group := token.Group
	if group == "" {
		group = userGroup
	} else {
		if !GroupInUserUsableGroups(userGroup, group) {
			return nil, fmt.Errorf("无权访问 %s 分组", group)
		}
		if group != "auto" && !ratio_setting.ContainsGroupRatio(group) {
			return nil, fmt.Errorf("分组 %s 已被弃用", group)
		}
	}
	groups := []string{group}
	if group == "auto" {
		groups = GetUserAutoGroup(userGroup)
	}
	models, err := model.GetGroupsEnabledModels(groups)
	if err != nil {
		return nil, err
	}
	if !token.ModelLimitsEnabled {
		return models, nil
	}
	allowed := token.GetModelLimitsMap()
	filtered := make([]string, 0, len(models))
	for _, name := range models {
		if allowed[ratio_setting.FormatMatchingModelName(name)] {
			filtered = append(filtered, name)
		}
	}
	return filtered, nil
}
