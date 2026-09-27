package service

import (
	"math/rand"
	"sort"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func ModelHealthApplies(c *gin.Context) bool {
	return operation_setting.GetModelHealthSettings().Enabled && c != nil && c.Request != nil &&
		!c.GetBool("model_health_admin_test") && model.ModelHealthRequestSupported(c.Request.URL.Path)
}

func FilterModelHealthForRequest(c *gin.Context, channels []*model.Channel, name string) ([]*model.Channel, error) {
	if !ModelHealthApplies(c) {
		return channels, nil
	}
	return model.FilterModelHealthChannels(channels, name)
}

func CheckModelHealthForRequest(c *gin.Context, ch *model.Channel, name string) error {
	channels, err := FilterModelHealthForRequest(c, []*model.Channel{ch}, name)
	if err != nil {
		return err
	}
	if len(channels) == 0 {
		return model.ErrModelUnavailable
	}
	return nil
}

// Filter before choosing a priority so blocked top-priority channels cannot hide
// healthy lower-priority channels. This works with both DB and memory routing.
func selectChannelWithModelHealth(c *gin.Context, group, name string, retry int, allowed map[string]struct{}) (*model.Channel, error) {
	if !ModelHealthApplies(c) {
		return model.GetRandomSatisfiedChannelWithNameFilter(group, name, retry, allowed)
	}
	channels, err := model.ListSatisfiedChannelsWithNameFilter(group, name, allowed)
	if err != nil {
		return nil, err
	}
	channels, err = model.FilterModelHealthChannels(channels, name)
	if err != nil || len(channels) == 0 {
		return nil, err
	}
	priorities := make(map[int64]bool)
	for _, ch := range channels {
		priorities[ch.GetPriority()] = true
	}
	ordered := make([]int64, 0, len(priorities))
	for priority := range priorities {
		ordered = append(ordered, priority)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] > ordered[j] })
	if retry >= len(ordered) {
		retry = len(ordered) - 1
	}
	if retry < 0 {
		retry = 0
	}
	selected := make([]*model.Channel, 0)
	total := 0
	for _, ch := range channels {
		if ch.GetPriority() == ordered[retry] {
			selected = append(selected, ch)
			total += ch.GetWeight()
		}
	}
	factor, adjustment := 1, 0
	if total == 0 {
		total, adjustment = len(selected)*100, 100
	} else if total/len(selected) < 10 {
		factor = 100
	}
	weight := rand.Intn(total * factor)
	for _, ch := range selected {
		weight -= ch.GetWeight()*factor + adjustment
		if weight < 0 {
			return ch, nil
		}
	}
	return selected[len(selected)-1], nil
}
