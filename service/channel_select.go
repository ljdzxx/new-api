package service

import (
	"errors"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

type RetryParam struct {
	Ctx          *gin.Context
	TokenGroup   string
	ModelName    string
	Retry        *int
	resetNextTry bool

	retryCandidates      []retryChannelCandidate
	retryCandidatesBuilt bool
	retryCursor          int
	failedChannels       map[int]struct{}
}

type retryChannelCandidate struct {
	ChannelID  int
	Group      string
	Priority   int64
	Weight     int
	GroupOrder int
}

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *RetryParam) IncreaseRetry() {
	if p.resetNextTry {
		p.resetNextTry = false
		return
	}
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

func (p *RetryParam) ResetRetryNextTry() {
	p.resetNextTry = true
}

func (p *RetryParam) MarkChannelFailed(channelID int) {
	if channelID <= 0 {
		return
	}
	if p.failedChannels == nil {
		p.failedChannels = make(map[int]struct{})
	}
	p.failedChannels[channelID] = struct{}{}
}

func (p *RetryParam) isFailedChannel(channelID int) bool {
	if len(p.failedChannels) == 0 {
		return false
	}
	_, failed := p.failedChannels[channelID]
	return failed
}

func getUserLevelAllowedChannelSet(ctx *gin.Context) map[string]struct{} {
	userLevelID := common.GetContextKeyInt(ctx, constant.ContextKeyUserLevelID)
	levelName, levelFound := setting.GetUserLevelPolicyLevelByID(userLevelID)
	allowedChannels, hasUserLevelPolicy := setting.GetUserLevelAllowedChannels(levelName)
	if !levelFound || !hasUserLevelPolicy || len(allowedChannels) == 0 {
		return nil
	}
	allowedChannelSet := make(map[string]struct{}, len(allowedChannels))
	for _, name := range allowedChannels {
		allowedChannelSet[strings.TrimSpace(name)] = struct{}{}
	}
	return allowedChannelSet
}

func (p *RetryParam) buildRetryCandidates() error {
	if p.retryCandidatesBuilt {
		return nil
	}
	p.retryCandidatesBuilt = true

	allowedChannelSet := getUserLevelAllowedChannelSet(p.Ctx)
	candidates := make([]retryChannelCandidate, 0, 16)
	channelSeen := make(map[int]struct{})

	appendGroupChannels := func(group string, groupOrder int) error {
		channels, err := model.ListSatisfiedChannelsWithNameFilter(group, p.ModelName, allowedChannelSet)
		if err != nil {
			return err
		}
		for _, ch := range channels {
			if ch == nil {
				continue
			}
			if _, exists := channelSeen[ch.Id]; exists {
				continue
			}
			channelSeen[ch.Id] = struct{}{}
			candidates = append(candidates, retryChannelCandidate{
				ChannelID:  ch.Id,
				Group:      group,
				Priority:   ch.GetPriority(),
				Weight:     ch.GetWeight(),
				GroupOrder: groupOrder,
			})
		}
		return nil
	}

	if p.TokenGroup == "auto" {
		if len(setting.GetAutoGroups()) == 0 {
			return errors.New("auto groups is not enabled")
		}
		userGroup := common.GetContextKeyString(p.Ctx, constant.ContextKeyUserGroup)
		autoGroups := GetUserAutoGroup(userGroup)
		for i, autoGroup := range autoGroups {
			if err := appendGroupChannels(autoGroup, i); err != nil {
				return err
			}
		}
	} else {
		if err := appendGroupChannels(p.TokenGroup, 0); err != nil {
			return err
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		if candidates[i].GroupOrder != candidates[j].GroupOrder {
			return candidates[i].GroupOrder < candidates[j].GroupOrder
		}
		if candidates[i].Weight != candidates[j].Weight {
			return candidates[i].Weight > candidates[j].Weight
		}
		return candidates[i].ChannelID < candidates[j].ChannelID
	})

	p.retryCandidates = candidates
	p.retryCursor = 0
	return nil
}

// GetNextRetryChannel selects the next retry channel from a pre-built candidate list.
// Candidate list is built once per request and sorted by priority (high -> low),
// then retries walk the list in order while skipping channels already marked as failed.
func GetNextRetryChannel(param *RetryParam) (*model.Channel, string, error) {
	if err := param.buildRetryCandidates(); err != nil {
		return nil, param.TokenGroup, err
	}
	for param.retryCursor < len(param.retryCandidates) {
		candidate := param.retryCandidates[param.retryCursor]
		param.retryCursor++

		if param.isFailedChannel(candidate.ChannelID) {
			continue
		}
		channel, err := model.CacheGetChannel(candidate.ChannelID)
		if err != nil || channel == nil {
			continue
		}
		if channel.Status != common.ChannelStatusEnabled {
			continue
		}
		if param.TokenGroup == "auto" {
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, candidate.Group)
		}
		return channel, candidate.Group, nil
	}
	return nil, param.TokenGroup, nil
}

// CacheGetRandomSatisfiedChannel tries to get a random channel that satisfies the requirements.
// 尝试获取一个满足要求的随机渠道。
//
// For "auto" tokenGroup with cross-group Retry enabled:
// 对于启用了跨分组重试的 "auto" tokenGroup：
//
//   - Each group will exhaust all its priorities before moving to the next group.
//     每个分组会用完所有优先级后才会切换到下一个分组。
//
//   - Uses ContextKeyAutoGroupIndex to track current group index.
//     使用 ContextKeyAutoGroupIndex 跟踪当前分组索引。
//
//   - Uses ContextKeyAutoGroupRetryIndex to track the global Retry count when current group started.
//     使用 ContextKeyAutoGroupRetryIndex 跟踪当前分组开始时的全局重试次数。
//
//   - priorityRetry = Retry - startRetryIndex, represents the priority level within current group.
//     priorityRetry = Retry - startRetryIndex，表示当前分组内的优先级级别。
//
//   - When GetRandomSatisfiedChannel returns nil (priorities exhausted), moves to next group.
//     当 GetRandomSatisfiedChannel 返回 nil（优先级用完）时，切换到下一个分组。
//
// Example flow (2 groups, each with 2 priorities, RetryTimes=3):
// 示例流程（2个分组，每个有2个优先级，RetryTimes=3）：
//
//	Retry=0: GroupA, priority0 (startRetryIndex=0, priorityRetry=0)
//	         分组A, 优先级0
//
//	Retry=1: GroupA, priority1 (startRetryIndex=0, priorityRetry=1)
//	         分组A, 优先级1
//
//	Retry=2: GroupA exhausted → GroupB, priority0 (startRetryIndex=2, priorityRetry=0)
//	         分组A用完 → 分组B, 优先级0
//
//	Retry=3: GroupB, priority1 (startRetryIndex=2, priorityRetry=1)
//	         分组B, 优先级1
func CacheGetRandomSatisfiedChannel(param *RetryParam) (*model.Channel, string, error) {
	var channel *model.Channel
	var err error
	selectGroup := param.TokenGroup
	userGroup := common.GetContextKeyString(param.Ctx, constant.ContextKeyUserGroup)
	allowedChannelSet := getUserLevelAllowedChannelSet(param.Ctx)

	if param.TokenGroup == "auto" {
		if len(setting.GetAutoGroups()) == 0 {
			return nil, selectGroup, errors.New("auto groups is not enabled")
		}
		autoGroups := GetUserAutoGroup(userGroup)

		// startGroupIndex: the group index to start searching from
		// startGroupIndex: 开始搜索的分组索引
		startGroupIndex := 0
		crossGroupRetry := common.GetContextKeyBool(param.Ctx, constant.ContextKeyTokenCrossGroupRetry)

		if lastGroupIndex, exists := common.GetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex); exists {
			if idx, ok := lastGroupIndex.(int); ok {
				startGroupIndex = idx
			}
		}

		for i := startGroupIndex; i < len(autoGroups); i++ {
			autoGroup := autoGroups[i]
			// Calculate priorityRetry for current group
			// 计算当前分组的 priorityRetry
			priorityRetry := param.GetRetry()
			// If moved to a new group, reset priorityRetry and update startRetryIndex
			// 如果切换到新分组，重置 priorityRetry 并更新 startRetryIndex
			if i > startGroupIndex {
				priorityRetry = 0
			}
			logger.LogDebug(param.Ctx, "Auto selecting group: %s, priorityRetry: %d", autoGroup, priorityRetry)

			channel, _ = model.GetRandomSatisfiedChannelWithNameFilter(autoGroup, param.ModelName, priorityRetry, allowedChannelSet)
			if channel == nil {
				// Current group has no available channel for this model, try next group
				// 当前分组没有该模型的可用渠道，尝试下一个分组
				logger.LogDebug(param.Ctx, "No available channel in group %s for model %s at priorityRetry %d, trying next group", autoGroup, param.ModelName, priorityRetry)
				// 重置状态以尝试下一个分组
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupRetryIndex, 0)
				// Reset retry counter so outer loop can continue for next group
				// 重置重试计数器，以便外层循环可以为下一个分组继续
				param.SetRetry(0)
				continue
			}
			common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroup, autoGroup)
			selectGroup = autoGroup
			logger.LogDebug(param.Ctx, "Auto selected group: %s", autoGroup)

			// Prepare state for next retry
			// 为下一次重试准备状态
			if crossGroupRetry && priorityRetry >= common.RetryTimes {
				// Current group has exhausted all retries, prepare to switch to next group
				// This request still uses current group, but next retry will use next group
				// 当前分组已用完所有重试次数，准备切换到下一个分组
				// 本次请求仍使用当前分组，但下次重试将使用下一个分组
				logger.LogDebug(param.Ctx, "Current group %s retries exhausted (priorityRetry=%d >= RetryTimes=%d), preparing switch to next group for next retry", autoGroup, priorityRetry, common.RetryTimes)
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i+1)
				// Reset retry counter so outer loop can continue for next group
				// 重置重试计数器，以便外层循环可以为下一个分组继续
				param.SetRetry(0)
				param.ResetRetryNextTry()
			} else {
				// Stay in current group, save current state
				// 保持在当前分组，保存当前状态
				common.SetContextKey(param.Ctx, constant.ContextKeyAutoGroupIndex, i)
			}
			break
		}
	} else {
		channel, err = model.GetRandomSatisfiedChannelWithNameFilter(param.TokenGroup, param.ModelName, param.GetRetry(), allowedChannelSet)
		if err != nil {
			return nil, param.TokenGroup, err
		}
	}
	return channel, selectGroup, nil
}
