package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func GetAllLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	logs, total, err := model.GetAllLogs(logType, startTimestamp, endTimestamp, modelName, username, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), channel, group, requestId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetUserLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")
	logs, total, err := model.GetUserLogs(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), group, requestId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

// Deprecated: SearchAllLogs 已废弃，前端未使用该接口。
func SearchAllLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

// Deprecated: SearchUserLogs 已废弃，前端未使用该接口。
func SearchUserLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

func GetLogByKey(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		c.JSON(200, gin.H{
			"success": false,
			"message": "无效的令牌",
		})
		return
	}
	logs, err := model.GetLogByTokenId(tokenId)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data":    logs,
	})
}

func GetLogsStat(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	username := c.Query("username")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	stat, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, "")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota": stat.Quota,
			"rpm":   stat.Rpm,
			"tpm":   stat.Tpm,
		},
	})
	return
}

func GetLogsSelfStat(c *gin.Context) {
	username := c.GetString("username")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	quotaNum, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, tokenName)
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota": quotaNum.Quota,
			"rpm":   quotaNum.Rpm,
			"tpm":   quotaNum.Tpm,
			//"token": tokenNum,
		},
	})
	return
}

func DeleteHistoryLogs(c *gin.Context) {
	targetTimestamp, err := strconv.ParseInt(c.Query("target_timestamp"), 10, 64)
	if err != nil || targetTimestamp <= 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "target timestamp is required",
		})
		return
	}

	const defaultBatchSize = 1000
	const streamBatchSize = 100
	const maxBatchSize = 10000
	batchSize := 0
	if value := c.Query("batch_size"); value != "" {
		batchSize, err = strconv.Atoi(value)
		if err != nil || batchSize <= 0 || batchSize > maxBatchSize {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "batch size must be between 1 and 10000",
			})
			return
		}
	}

	if c.Query("stream") == "true" {
		if batchSize == 0 {
			batchSize = streamBatchSize
		}
		streamDeleteHistoryLogs(c, targetTimestamp, batchSize)
		return
	}

	var count int64
	for {
		limit := defaultBatchSize
		if batchSize > 0 {
			limit = batchSize
		}
		deleted, deleteErr := model.DeleteOldLog(c.Request.Context(), targetTimestamp, limit)
		if deleteErr != nil {
			common.ApiError(c, deleteErr)
			return
		}
		count += deleted
		if batchSize > 0 || deleted < int64(limit) {
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
	return
}

type logDeletionEvent struct {
	Type    string `json:"type"`
	Deleted int64  `json:"deleted"`
	Message string `json:"message,omitempty"`
}

type logDeletionResult struct {
	deleted int64
	err     error
}

func streamDeleteHistoryLogs(c *gin.Context, targetTimestamp int64, batchSize int) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	var deletedCount int64
	writeLogDeletionEvent(c, logDeletionEvent{Type: "progress", Deleted: deletedCount})

	for {
		const maxLockRetries = 3
		lockRetryCount := 0
		var deleted int64

		for {
			var err error
			deleted, err = deleteOldLogBatchWithHeartbeat(c, targetTimestamp, batchSize)
			if err == nil {
				break
			}
			if c.Request.Context().Err() != nil {
				return
			}
			if !isRetryableLogDeletionError(err) || lockRetryCount >= maxLockRetries {
				writeLogDeletionEvent(c, logDeletionEvent{
					Type:    "error",
					Deleted: deletedCount,
					Message: err.Error(),
				})
				writeLogDeletionDone(c)
				return
			}

			writeLogDeletionEvent(c, logDeletionEvent{
				Type:    "retry",
				Deleted: deletedCount,
				Message: err.Error(),
			})
			if !waitForLogDeletionRetry(c, time.Second*time.Duration(1<<lockRetryCount)) {
				return
			}
			lockRetryCount++
		}

		deletedCount += deleted
		writeLogDeletionEvent(c, logDeletionEvent{Type: "progress", Deleted: deletedCount})
		if deleted < int64(batchSize) {
			writeLogDeletionEvent(c, logDeletionEvent{Type: "complete", Deleted: deletedCount})
			writeLogDeletionDone(c)
			return
		}
	}
}

func deleteOldLogBatchWithHeartbeat(c *gin.Context, targetTimestamp int64, batchSize int) (int64, error) {
	resultChannel := make(chan logDeletionResult, 1)
	go func() {
		deleted, err := model.DeleteOldLog(c.Request.Context(), targetTimestamp, batchSize)
		resultChannel <- logDeletionResult{deleted: deleted, err: err}
	}()

	heartbeatTicker := time.NewTicker(10 * time.Second)
	defer heartbeatTicker.Stop()
	for {
		select {
		case result := <-resultChannel:
			return result.deleted, result.err
		case <-heartbeatTicker.C:
			_, _ = fmt.Fprint(c.Writer, ": keep-alive\n\n")
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return 0, c.Request.Context().Err()
		}
	}
}

func waitForLogDeletionRetry(c *gin.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-c.Request.Context().Done():
		return false
	}
}

func isRetryableLogDeletionError(err error) bool {
	message := err.Error()
	return strings.Contains(message, "Error 1205") || strings.Contains(message, "Error 1213")
}

func writeLogDeletionEvent(c *gin.Context, event logDeletionEvent) {
	data, err := common.Marshal(event)
	if err != nil {
		common.SysError("failed to marshal log deletion event: " + err.Error())
		return
	}
	_, _ = fmt.Fprintf(c.Writer, "data: %s\n\n", data)
	c.Writer.Flush()
}

func writeLogDeletionDone(c *gin.Context) {
	_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}
