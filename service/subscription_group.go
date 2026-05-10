package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func SubscriptionCandidateGroups(info *relaycommon.RelayInfo) []string {
	if info == nil {
		return nil
	}
	group := strings.TrimSpace(info.TokenGroup)
	if group == "" {
		group = strings.TrimSpace(info.UsingGroup)
	}
	if group == "" {
		group = strings.TrimSpace(info.UserGroup)
	}
	if group == "" {
		return nil
	}
	if group != "auto" {
		return []string{group}
	}
	userGroups := GetUserAutoGroup(info.UserGroup)
	if len(userGroups) == 0 {
		return []string{"auto"}
	}
	return userGroups
}

func groupAllowedBySubscriptionSet(group string, allowedSet map[string]struct{}) bool {
	if len(allowedSet) == 0 {
		return true
	}
	_, ok := allowedSet[strings.TrimSpace(group)]
	return ok
}

func FilterGroupsBySubscription(groupNames []string, allowedGroups string) []string {
	allowedSet := model.SubscriptionAllowedGroupSet(allowedGroups)
	if len(allowedSet) == 0 {
		return groupNames
	}
	filtered := make([]string, 0, len(groupNames))
	for _, group := range groupNames {
		if groupAllowedBySubscriptionSet(group, allowedSet) {
			filtered = append(filtered, group)
		}
	}
	return filtered
}

func SubscriptionAllowsGroup(group string, allowedGroups string) bool {
	return groupAllowedBySubscriptionSet(group, model.SubscriptionAllowedGroupSet(allowedGroups))
}

func SubscriptionAllowedGroupsFromContext(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if allowedGroups := common.GetContextKeyString(c, constant.ContextKeySubscriptionPlanAllowedGroups); strings.TrimSpace(allowedGroups) != "" {
		return allowedGroups
	}
	userSetting, ok := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting)
	if !ok || common.NormalizeBillingPreference(userSetting.BillingPreference) != "subscription_only" {
		return ""
	}
	return common.GetContextKeyString(c, constant.ContextKeySubscriptionAllowedGroups)
}
