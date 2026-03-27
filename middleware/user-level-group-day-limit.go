package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

const userLevelGroupDayLimitExceededMessage = "鎮ㄦ墍鍦ㄧ殑缁勫埆宸茶揪褰撴棩浣跨敤涓婇檺锛岃鎻愬崌璐︽埛绛夌骇銆?"

func UserLevelGroupDayLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		userID := c.GetInt("id")
		if userID <= 0 {
			c.Next()
			return
		}

		userLevelID := common.GetContextKeyInt(c, constant.ContextKeyUserLevelID)
		if userLevelID <= 0 {
			userCache, err := model.GetUserCache(userID)
			if err != nil {
				abortWithOpenAiMessage(c, http.StatusInternalServerError, "group_day_limit_check_failed")
				return
			}
			userLevelID = userCache.UserLevelID
			common.SetContextKey(c, constant.ContextKeyUserLevelID, userLevelID)
		}

		groupDayLimit, found := setting.GetUserLevelGroupDayLimitByID(userLevelID)
		if !found || groupDayLimit <= 0 {
			c.Next()
			return
		}

		levelName, found := setting.GetUserLevelPolicyLevelByID(userLevelID)
		if !found || levelName == "" {
			c.Next()
			return
		}

		dailyConsumedMoney, err := model.GetUserLevelGroupDailyConsumedMoney(levelName)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "group_day_limit_check_failed")
			return
		}
		if dailyConsumedMoney >= groupDayLimit {
			abortWithOpenAiMessage(c, http.StatusForbidden, userLevelGroupDayLimitExceededMessage)
			return
		}

		c.Next()
	}
}

