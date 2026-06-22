package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

const epayTopUpReconcileBatchSize = 100

var epayTopUpReconcileRunning atomic.Bool

type EpayTopUpReconcileStartRequest struct {
	Filter       model.TopUpFilter
	OperatorId   int
	OperatorName string
}

func BuildEpayTopUpReconcileFilter(filter model.TopUpFilter) model.TopUpFilter {
	filter.PaymentProvider = model.PaymentProviderEpay
	filter.Status = common.TopUpStatusSuccess
	filter.ReconcileStatus = model.PaymentReconcileStatusUnchecked
	return filter
}

func StartEpayTopUpReconcile(req EpayTopUpReconcileStartRequest) (*model.PaymentReconcileJob, error) {
	if !epayTopUpReconcileRunning.CompareAndSwap(false, true) {
		return nil, errors.New("已有 Epay 对账任务正在执行")
	}

	filter := BuildEpayTopUpReconcileFilter(req.Filter)
	maxTopUpId, err := model.MaxTopUpIdWithFilter(filter)
	if err != nil {
		epayTopUpReconcileRunning.Store(false)
		return nil, err
	}
	filter.MaxId = maxTopUpId
	total, err := model.CountTopUpsWithFilter(filter)
	if err != nil {
		epayTopUpReconcileRunning.Store(false)
		return nil, err
	}

	filterBytes, err := common.Marshal(map[string]interface{}{
		"keyword":          filter.Keyword,
		"user_id":          filter.UserId,
		"username":         filter.Username,
		"status":           filter.Status,
		"payment_provider": filter.PaymentProvider,
		"payment_method":   filter.PaymentMethod,
		"reconcile_status": filter.ReconcileStatus,
		"start_timestamp":  filter.StartTimestamp,
		"end_timestamp":    filter.EndTimestamp,
		"max_id":           filter.MaxId,
	})
	if err != nil {
		epayTopUpReconcileRunning.Store(false)
		return nil, err
	}

	job := &model.PaymentReconcileJob{
		Provider:     model.PaymentProviderEpay,
		Status:       model.PaymentReconcileJobStatusPending,
		OperatorId:   req.OperatorId,
		OperatorName: strings.TrimSpace(req.OperatorName),
		Filter:       string(filterBytes),
		TotalCount:   int(total),
	}
	if err = model.CreatePaymentReconcileJob(job); err != nil {
		epayTopUpReconcileRunning.Store(false)
		return nil, err
	}

	go runEpayTopUpReconcileJob(job.Id, filter)
	return job, nil
}

func runEpayTopUpReconcileJob(jobId int, filter model.TopUpFilter) {
	defer epayTopUpReconcileRunning.Store(false)
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("Epay 对账任务异常退出: %v", r)
			common.SysError(msg)
			_ = model.UpdatePaymentReconcileJobFields(jobId, map[string]interface{}{
				"status":      model.PaymentReconcileJobStatusFailed,
				"message":     msg,
				"finished_at": common.GetTimestamp(),
			})
		}
	}()

	now := common.GetTimestamp()
	_ = model.UpdatePaymentReconcileJobFields(jobId, map[string]interface{}{
		"status":     model.PaymentReconcileJobStatusRunning,
		"started_at": now,
	})

	var checkedCount int
	var normalCount int
	var abnormalCount int
	var failedCount int
	var lastTopUpId int

	for {
		topups, err := model.FindTopUpsWithFilter(filter, lastTopUpId, epayTopUpReconcileBatchSize)
		if err != nil {
			failedCount++
			_ = model.UpdatePaymentReconcileJobFields(jobId, map[string]interface{}{
				"status":         model.PaymentReconcileJobStatusFailed,
				"checked_count":  checkedCount,
				"normal_count":   normalCount,
				"abnormal_count": abnormalCount,
				"failed_count":   failedCount,
				"last_topup_id":  lastTopUpId,
				"message":        err.Error(),
				"finished_at":    common.GetTimestamp(),
			})
			return
		}
		if len(topups) == 0 {
			break
		}

		for _, topUp := range topups {
			if topUp == nil {
				continue
			}
			lastTopUpId = topUp.Id
			status, message, remote := reconcileOneEpayTopUp(context.Background(), topUp)
			if err := model.UpdateTopUpReconcileResult(topUp.Id, status, message); err != nil {
				status = model.PaymentReconcileStatusAbnormal
				message = "写入订单对账状态失败: " + err.Error()
				failedCount++
			}
			if status == model.PaymentReconcileStatusNormal {
				normalCount++
			} else {
				abnormalCount++
			}
			checkedCount++
			_ = model.CreatePaymentReconcileItem(buildEpayReconcileItem(jobId, topUp, status, message, remote))
			_ = model.UpdatePaymentReconcileJobFields(jobId, map[string]interface{}{
				"checked_count":  checkedCount,
				"normal_count":   normalCount,
				"abnormal_count": abnormalCount,
				"failed_count":   failedCount,
				"last_topup_id":  lastTopUpId,
			})
		}
	}

	status := model.PaymentReconcileJobStatusSuccess
	if abnormalCount > 0 || failedCount > 0 {
		status = model.PaymentReconcileJobStatusPartial
	}
	_ = model.UpdatePaymentReconcileJobFields(jobId, map[string]interface{}{
		"status":         status,
		"checked_count":  checkedCount,
		"normal_count":   normalCount,
		"abnormal_count": abnormalCount,
		"failed_count":   failedCount,
		"last_topup_id":  lastTopUpId,
		"message":        fmt.Sprintf("对账完成，正常 %d，异常 %d，失败 %d", normalCount, abnormalCount, failedCount),
		"finished_at":    common.GetTimestamp(),
	})
}

func reconcileOneEpayTopUp(ctx context.Context, topUp *model.TopUp) (string, string, *EpayOrderInfo) {
	if topUp.PaymentProvider != model.PaymentProviderEpay {
		return model.PaymentReconcileStatusAbnormal, "本地支付通道不是 Epay", nil
	}
	if strings.TrimSpace(topUp.TradeNo) == "" {
		return model.PaymentReconcileStatusAbnormal, "本地订单号为空", nil
	}
	if strings.TrimSpace(topUp.PaymentMethod) == "" {
		return model.PaymentReconcileStatusAbnormal, "本地支付方式为空", nil
	}
	orderInfo, err := QueryEpayOrder(ctx, topUp.TradeNo, "")
	if err != nil {
		return model.PaymentReconcileStatusAbnormal, "Epay 查单失败: " + err.Error(), nil
	}
	if err := validateEpayReconcileOrder(topUp, orderInfo); err != nil {
		return model.PaymentReconcileStatusAbnormal, err.Error(), orderInfo
	}
	return model.PaymentReconcileStatusNormal, "Epay 平台订单已支付且金额、方式、商户号一致", orderInfo
}

func validateEpayReconcileOrder(topUp *model.TopUp, orderInfo *EpayOrderInfo) error {
	if orderInfo == nil {
		return errors.New("Epay 查单结果为空")
	}
	if orderInfo.Code != 0 {
		return fmt.Errorf("Epay 查单返回异常: code=%d msg=%s", orderInfo.Code, orderInfo.Msg)
	}
	if orderInfo.Status != "1" {
		return fmt.Errorf("Epay 平台订单未支付: status=%s", orderInfo.Status)
	}
	if strings.TrimSpace(orderInfo.TradeNo) == "" {
		return errors.New("Epay 平台订单号为空")
	}
	if orderInfo.OutTradeNo != topUp.TradeNo {
		return fmt.Errorf("Epay 商户订单号不一致: query=%s local=%s", orderInfo.OutTradeNo, topUp.TradeNo)
	}
	if orderInfo.Type != topUp.PaymentMethod {
		return fmt.Errorf("Epay 支付方式不一致: query=%s local=%s", orderInfo.Type, topUp.PaymentMethod)
	}
	if orderInfo.Pid != operation_setting.EpayId {
		return fmt.Errorf("Epay 商户号不一致: query=%s", orderInfo.Pid)
	}
	if !sameEpayMoney(orderInfo.Money, topUp.Money) {
		return fmt.Errorf("Epay 金额不一致: query=%s local=%.6f", orderInfo.Money, topUp.Money)
	}
	return nil
}

func buildEpayReconcileItem(jobId int, topUp *model.TopUp, status string, message string, remote *EpayOrderInfo) *model.PaymentReconcileItem {
	item := &model.PaymentReconcileItem{
		JobId:   jobId,
		TopUpId: topUp.Id,
		TradeNo: topUp.TradeNo,
		Status:  status,
		Message: message,
	}
	if remote != nil {
		item.RemoteTradeNo = remote.TradeNo
		item.RemoteOutTradeNo = remote.OutTradeNo
		item.RemoteMoney = remote.Money
		item.RemoteStatus = remote.Status
		item.RemoteType = remote.Type
		item.RemotePid = remote.Pid
	}
	return item
}

func sameEpayMoney(actual string, expected float64) bool {
	actualDecimal, err := decimal.NewFromString(strings.TrimSpace(actual))
	if err != nil {
		return false
	}
	expectedDecimal := decimal.NewFromFloat(expected)
	return actualDecimal.Round(2).Equal(expectedDecimal.Round(2))
}
