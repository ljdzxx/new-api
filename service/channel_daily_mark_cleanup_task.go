package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
)

const channelDailyMarkCleanupTickInterval = 5 * time.Minute

func StartChannelDailyMarkCleanupTask() {
	if !common.IsMasterNode {
		return
	}
	gopool.Go(func() {
		ticker := time.NewTicker(channelDailyMarkCleanupTickInterval)
		defer ticker.Stop()
		for range ticker.C {
			if err := CleanupExpiredChannelDailyMarks(); err != nil {
				common.SysLog("cleanup expired channel daily marks failed: " + err.Error())
			}
		}
	})
}
