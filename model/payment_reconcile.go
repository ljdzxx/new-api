package model

import "github.com/QuantumNous/new-api/common"

const (
	PaymentReconcileJobStatusPending = "pending"
	PaymentReconcileJobStatusRunning = "running"
	PaymentReconcileJobStatusSuccess = "success"
	PaymentReconcileJobStatusFailed  = "failed"
	PaymentReconcileJobStatusPartial = "partial"
)

type PaymentReconcileJob struct {
	Id            int    `json:"id"`
	Provider      string `json:"provider" gorm:"type:varchar(50);index"`
	Status        string `json:"status" gorm:"type:varchar(32);index"`
	OperatorId    int    `json:"operator_id" gorm:"index"`
	OperatorName  string `json:"operator_name" gorm:"type:varchar(255)"`
	Filter        string `json:"filter" gorm:"type:text"`
	TotalCount    int    `json:"total_count"`
	CheckedCount  int    `json:"checked_count"`
	NormalCount   int    `json:"normal_count"`
	AbnormalCount int    `json:"abnormal_count"`
	FailedCount   int    `json:"failed_count"`
	LastTopUpId   int    `json:"last_topup_id"`
	Message       string `json:"message" gorm:"type:text"`
	CreatedAt     int64  `json:"created_at" gorm:"index"`
	UpdatedAt     int64  `json:"updated_at"`
	StartedAt     int64  `json:"started_at"`
	FinishedAt    int64  `json:"finished_at"`
}

type PaymentReconcileItem struct {
	Id               int    `json:"id"`
	JobId            int    `json:"job_id" gorm:"index"`
	TopUpId          int    `json:"topup_id" gorm:"index"`
	TradeNo          string `json:"trade_no" gorm:"type:varchar(255);index"`
	Status           string `json:"status" gorm:"type:varchar(32);index"`
	Message          string `json:"message" gorm:"type:text"`
	RemoteTradeNo    string `json:"remote_trade_no" gorm:"type:varchar(255)"`
	RemoteOutTradeNo string `json:"remote_out_trade_no" gorm:"type:varchar(255)"`
	RemoteMoney      string `json:"remote_money" gorm:"type:varchar(64)"`
	RemoteStatus     string `json:"remote_status" gorm:"type:varchar(32)"`
	RemoteType       string `json:"remote_type" gorm:"type:varchar(50)"`
	RemotePid        string `json:"remote_pid" gorm:"type:varchar(255)"`
	CheckedAt        int64  `json:"checked_at" gorm:"index"`
}

func CreatePaymentReconcileJob(job *PaymentReconcileJob) error {
	now := common.GetTimestamp()
	job.CreatedAt = now
	job.UpdatedAt = now
	if job.Status == "" {
		job.Status = PaymentReconcileJobStatusPending
	}
	return DB.Create(job).Error
}

func UpdatePaymentReconcileJobFields(jobId int, fields map[string]interface{}) error {
	if fields == nil {
		fields = map[string]interface{}{}
	}
	fields["updated_at"] = common.GetTimestamp()
	return DB.Model(&PaymentReconcileJob{}).Where("id = ?", jobId).Updates(fields).Error
}

func CreatePaymentReconcileItem(item *PaymentReconcileItem) error {
	if item.CheckedAt == 0 {
		item.CheckedAt = common.GetTimestamp()
	}
	return DB.Create(item).Error
}

func ListPaymentReconcileJobs(pageInfo *common.PageInfo) (jobs []*PaymentReconcileJob, total int64, err error) {
	query := DB.Model(&PaymentReconcileJob{})
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&jobs).Error
	return jobs, total, err
}

func ListPaymentReconcileItems(jobId int, pageInfo *common.PageInfo) (items []*PaymentReconcileItem, total int64, err error) {
	query := DB.Model(&PaymentReconcileItem{}).Where("job_id = ?", jobId)
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Order("id asc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&items).Error
	return items, total, err
}
