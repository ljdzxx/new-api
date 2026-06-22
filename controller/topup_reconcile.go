package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type startEpayTopUpReconcileRequest struct {
	Keyword         string `json:"keyword"`
	UserId          int    `json:"user_id"`
	Username        string `json:"username"`
	Status          string `json:"status"`
	PaymentProvider string `json:"payment_provider"`
	PaymentMethod   string `json:"payment_method"`
	ReconcileStatus string `json:"reconcile_status"`
	StartTimestamp  int64  `json:"start_timestamp"`
	EndTimestamp    int64  `json:"end_timestamp"`
}

func StartEpayTopUpReconcile(c *gin.Context) {
	var req startEpayTopUpReconcileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}

	job, err := service.StartEpayTopUpReconcile(service.EpayTopUpReconcileStartRequest{
		Filter: model.TopUpFilter{
			Keyword:         req.Keyword,
			UserId:          req.UserId,
			Username:        req.Username,
			Status:          req.Status,
			PaymentProvider: req.PaymentProvider,
			PaymentMethod:   req.PaymentMethod,
			ReconcileStatus: req.ReconcileStatus,
			StartTimestamp:  req.StartTimestamp,
			EndTimestamp:    req.EndTimestamp,
		},
		OperatorId:   c.GetInt("id"),
		OperatorName: c.GetString("username"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"job_id":      job.Id,
		"total_count": job.TotalCount,
	})
}

func GetPaymentReconcileJobs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	jobs, total, err := model.ListPaymentReconcileJobs(pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(jobs)
	common.ApiSuccess(c, pageInfo)
}

func GetPaymentReconcileItems(c *gin.Context) {
	jobId, err := strconv.Atoi(c.Param("job_id"))
	if err != nil || jobId <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.ListPaymentReconcileItems(jobId, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}
