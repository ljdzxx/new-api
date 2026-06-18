package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/limiter"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	ModelRequestRateLimitCountMark            = "MRRL"
	ModelRequestRateLimitSuccessCountMark     = "MRRLS"
	UserModelRequestRateLimitCountMark        = "UMRRL"
	UserModelRequestRateLimitSuccessCountMark = "UMRRLS"
)

func checkRedisRateLimit(ctx context.Context, rdb *redis.Client, key string, maxCount int, duration int64, expiration time.Duration) (bool, error) {
	if maxCount == 0 {
		return true, nil
	}

	length, err := rdb.LLen(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if length < int64(maxCount) {
		return true, nil
	}

	oldTimeStr, _ := rdb.LIndex(ctx, key, -1).Result()
	oldTime, err := time.Parse(timeFormat, oldTimeStr)
	if err != nil {
		return false, err
	}
	nowTimeStr := time.Now().Format(timeFormat)
	nowTime, err := time.Parse(timeFormat, nowTimeStr)
	if err != nil {
		return false, err
	}
	if int64(nowTime.Sub(oldTime).Seconds()) < duration {
		rdb.Expire(ctx, key, expiration)
		return false, nil
	}

	return true, nil
}

func recordRedisRequest(ctx context.Context, rdb *redis.Client, key string, maxCount int, expiration time.Duration) {
	if maxCount == 0 {
		return
	}

	now := time.Now().Format(timeFormat)
	rdb.LPush(ctx, key, now)
	rdb.LTrim(ctx, key, 0, int64(maxCount-1))
	rdb.Expire(ctx, key, expiration)
}

func redisTotalRateLimitKey(mark string, userID string) string {
	if mark == ModelRequestRateLimitCountMark {
		return fmt.Sprintf("rateLimit:%s", userID)
	}
	return fmt.Sprintf("rateLimit:%s:%s", mark, userID)
}

func systemRateLimitMessage(limitType string, maxCount int, duration int64) string {
	if duration <= 0 {
		duration = 60
	}
	return fmt.Sprintf("本系统速率限制已触发：%s，%d秒内最多%d次", limitType, duration, maxCount)
}

func redisRateLimitHandler(duration int64, totalMaxCount, successMaxCount int, totalMark, successMark string, expiration time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := strconv.Itoa(c.GetInt("id"))
		ctx := context.Background()
		rdb := common.RDB

		successKey := fmt.Sprintf("rateLimit:%s:%s", successMark, userID)
		allowed, err := checkRedisRateLimit(ctx, rdb, successKey, successMaxCount, duration, expiration)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
			return
		}
		if !allowed {
			abortWithSystemRateLimit(c, systemRateLimitMessage("请求完成次数过多", successMaxCount, duration))
			return
		}

		if totalMaxCount > 0 {
			totalKey := redisTotalRateLimitKey(totalMark, userID)
			tb := limiter.New(ctx, rdb)
			allowed, err = tb.Allow(
				ctx,
				totalKey,
				limiter.WithCapacity(int64(totalMaxCount)*duration),
				limiter.WithRate(int64(totalMaxCount)),
				limiter.WithRequested(duration),
			)
			if err != nil {
				abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
				return
			}
			if !allowed {
				abortWithSystemRateLimit(c, systemRateLimitMessage("总请求次数过多", totalMaxCount, duration))
				return
			}
		}

		c.Next()
		if c.Writer.Status() < 400 {
			recordRedisRequest(ctx, rdb, successKey, successMaxCount, expiration)
		}
	}
}

func memoryRateLimitHandler(duration int64, totalMaxCount, successMaxCount int, totalMark, successMark string, expiration time.Duration) gin.HandlerFunc {
	inMemoryRateLimiter.Init(expiration)

	return func(c *gin.Context) {
		userID := strconv.Itoa(c.GetInt("id"))
		totalKey := totalMark + userID
		successKey := successMark + userID

		if totalMaxCount > 0 && !inMemoryRateLimiter.Request(totalKey, totalMaxCount, duration) {
			abortWithSystemRateLimit(c, systemRateLimitMessage("总请求次数过多", totalMaxCount, duration))
			return
		}

		checkKey := successKey + "_check"
		if !inMemoryRateLimiter.Request(checkKey, successMaxCount, duration) {
			abortWithSystemRateLimit(c, systemRateLimitMessage("请求完成次数过多", successMaxCount, duration))
			return
		}

		c.Next()
		if c.Writer.Status() < 400 {
			inMemoryRateLimiter.Request(successKey, successMaxCount, duration)
		}
	}
}

func modelRateLimitHandler(duration int64, totalMaxCount, successMaxCount int, totalMark, successMark string, expiration time.Duration) gin.HandlerFunc {
	if common.RedisEnabled {
		return redisRateLimitHandler(duration, totalMaxCount, successMaxCount, totalMark, successMark, expiration)
	}
	return memoryRateLimitHandler(duration, totalMaxCount, successMaxCount, totalMark, successMark, expiration)
}

func ModelRequestRateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		if common.GetContextKeyBool(c, constant.ContextKeyUserRateLimitEnabled) {
			durationMinutes := common.GetContextKeyInt(c, constant.ContextKeyUserRateLimitDurationMinutes)
			if durationMinutes <= 0 {
				durationMinutes = 1
			}
			totalMaxCount := common.GetContextKeyInt(c, constant.ContextKeyUserRateLimitCount)
			successMaxCount := common.GetContextKeyInt(c, constant.ContextKeyUserRateLimitSuccessCount)
			if totalMaxCount < 0 {
				totalMaxCount = 0
			}
			if successMaxCount < 1 {
				successMaxCount = 1000
			}
			duration := int64(durationMinutes * 60)
			expiration := time.Duration(durationMinutes) * time.Minute
			modelRateLimitHandler(duration, totalMaxCount, successMaxCount, UserModelRequestRateLimitCountMark, UserModelRequestRateLimitSuccessCountMark, expiration)(c)
			return
		}

		userLevelID := common.GetContextKeyInt(c, constant.ContextKeyUserLevelID)
		if levelRate, found := setting.GetUserLevelRateLimitByID(userLevelID); found {
			if levelRate == 0 {
				c.Next()
				return
			}
			duration := int64(60)
			modelRateLimitHandler(duration, levelRate, levelRate, ModelRequestRateLimitCountMark, ModelRequestRateLimitSuccessCountMark, time.Minute)(c)
			return
		}

		if !setting.ModelRequestRateLimitEnabled {
			c.Next()
			return
		}

		duration := int64(setting.ModelRequestRateLimitDurationMinutes * 60)
		totalMaxCount := setting.ModelRequestRateLimitCount
		successMaxCount := setting.ModelRequestRateLimitSuccessCount

		group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		}

		groupTotalCount, groupSuccessCount, found := setting.GetGroupRateLimit(group)
		if found {
			totalMaxCount = groupTotalCount
			successMaxCount = groupSuccessCount
		}

		modelRateLimitHandler(duration, totalMaxCount, successMaxCount, ModelRequestRateLimitCountMark, ModelRequestRateLimitSuccessCountMark, time.Duration(setting.ModelRequestRateLimitDurationMinutes)*time.Minute)(c)
	}
}
