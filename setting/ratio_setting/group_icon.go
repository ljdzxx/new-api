package ratio_setting

import (
	"errors"
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var groupIcons = struct {
	sync.RWMutex
	values map[string]string
}{values: map[string]string{}}

var groupIconNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(\.Color)?$`)

func parseGroupIcons(value string) (map[string]string, error) {
	var icons map[string]string
	if err := common.UnmarshalJsonStr(value, &icons); err != nil {
		return nil, err
	}
	if icons == nil {
		return nil, errors.New("group icons must be a JSON object")
	}
	for group, icon := range icons {
		if strings.TrimSpace(group) == "" || !groupIconNamePattern.MatchString(icon) {
			return nil, errors.New("group icons require non-empty group names and icon names such as OpenAI or Claude.Color")
		}
	}
	return icons, nil
}

func CheckGroupIcon(value string) error {
	_, err := parseGroupIcons(value)
	return err
}

func UpdateGroupIconByJSONString(value string) error {
	icons, err := parseGroupIcons(value)
	if err != nil {
		return err
	}
	groupIcons.Lock()
	defer groupIcons.Unlock()
	groupIcons.values = icons
	return nil
}

func GroupIcon2JSONString() string {
	groupIcons.RLock()
	defer groupIcons.RUnlock()
	value, err := common.Marshal(groupIcons.values)
	if err != nil {
		return "{}"
	}
	return string(value)
}

func GetGroupIcon(group string) string {
	groupIcons.RLock()
	defer groupIcons.RUnlock()
	return groupIcons.values[group]
}
