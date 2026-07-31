package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// 发票申请状态
const (
	InvoiceStatusPending  = 1 // 已申请（未处理）
	InvoiceStatusIssued   = 2 // 已开具
	InvoiceStatusRejected = 3 // 已拒绝
)

// Invoice 用户开票申请
type Invoice struct {
	Id          int     `json:"id"`
	UserId      int     `json:"user_id" gorm:"index"`
	Username    string  `json:"username" gorm:"-"`
	TopUpId     int     `json:"top_up_id" gorm:"uniqueIndex"`
	TradeNo     string  `json:"trade_no" gorm:"type:varchar(255);index"`
	Money       float64 `json:"money"`
	Title       string  `json:"title" gorm:"type:varchar(255)"`
	TaxNo       string  `json:"tax_no" gorm:"type:varchar(64)"`
	Emails      string  `json:"emails" gorm:"type:text"`
	Status      int     `json:"status" gorm:"index"`
	FileKey     string  `json:"file_key" gorm:"type:varchar(512)"`
	Remark      string  `json:"remark" gorm:"type:text"`
	HandledTime int64   `json:"handled_time"`
	CreatedTime int64   `json:"created_time"`
	UpdatedTime int64   `json:"updated_time"`
}

func (invoice *Invoice) Insert() error {
	return DB.Create(invoice).Error
}

func (invoice *Invoice) Update() error {
	return DB.Save(invoice).Error
}

func GetInvoiceById(id int) (*Invoice, error) {
	var invoice Invoice
	if err := DB.Where("id = ?", id).First(&invoice).Error; err != nil {
		return nil, err
	}
	return &invoice, nil
}

// GetInvoiceByTopUpId 查询某充值订单的开票申请，不存在时返回 nil, nil
func GetInvoiceByTopUpId(topUpId int) (*Invoice, error) {
	var invoice Invoice
	if err := DB.Where("top_up_id = ?", topUpId).First(&invoice).Error; err != nil {
		return nil, nil
	}
	return &invoice, nil
}

// GetInvoicesByTopUpIds 批量查询充值订单对应的开票申请
func GetInvoicesByTopUpIds(topUpIds []int) (map[int]*Invoice, error) {
	result := make(map[int]*Invoice, len(topUpIds))
	if len(topUpIds) == 0 {
		return result, nil
	}
	var invoices []*Invoice
	if err := DB.Where("top_up_id IN ?", topUpIds).Find(&invoices).Error; err != nil {
		return nil, err
	}
	for _, invoice := range invoices {
		result[invoice.TopUpId] = invoice
	}
	return result, nil
}

// GetUserInvoiceableTopUps 查询用户可参与开票的充值记录：
// 支付成功、在完成时间上线之后，且支付通道为 EPay 或兑换码兑换（实付金额大于 0）
func GetUserInvoiceableTopUps(userId int, onlineTs int64, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	query := DB.Model(&TopUp{}).
		Where("user_id = ? AND status = ?", userId, common.TopUpStatusSuccess).
		Where("complete_time >= ?", onlineTs).
		Where("(payment_provider = ? OR (payment_method = ? AND money > 0))",
			PaymentProviderEpay, PaymentMethodRedemption)

	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err = query.Order("id desc").
		Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).
		Find(&topups).Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// GetUserInvoices 用户自己的开票申请记录
func GetUserInvoices(userId int, pageInfo *common.PageInfo) (invoices []*Invoice, total int64, err error) {
	query := DB.Model(&Invoice{}).Where("user_id = ?", userId)
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err = query.Order("id desc").
		Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).
		Find(&invoices).Error; err != nil {
		return nil, 0, err
	}
	return invoices, total, nil
}

type InvoiceFilter struct {
	Keyword  string // 充值订单号 / 公司抬头
	Username string
	Status   int
}

// SearchInvoicesWithFilter 管理员查询开票申请
func SearchInvoicesWithFilter(filter InvoiceFilter, pageInfo *common.PageInfo) (invoices []*Invoice, total int64, err error) {
	query := DB.Model(&Invoice{})
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%%" + keyword + "%%"
		query = query.Where("trade_no LIKE ? OR title LIKE ?", like, like)
	}
	if username := strings.TrimSpace(filter.Username); username != "" {
		like := "%%" + username + "%%"
		userQuery := DB.Model(&User{}).Select("id").Where("username LIKE ?", like)
		query = query.Where("user_id IN (?)", userQuery)
	}
	if filter.Status > 0 {
		query = query.Where("status = ?", filter.Status)
	}

	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err = query.Order("id desc").
		Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).
		Find(&invoices).Error; err != nil {
		return nil, 0, err
	}
	fillInvoiceUsernames(invoices)
	return invoices, total, nil
}

func fillInvoiceUsernames(invoices []*Invoice) {
	if len(invoices) == 0 {
		return
	}
	userIdSet := make(map[int]struct{})
	for _, invoice := range invoices {
		if invoice != nil && invoice.UserId > 0 {
			userIdSet[invoice.UserId] = struct{}{}
		}
	}
	if len(userIdSet) == 0 {
		return
	}
	userIds := make([]int, 0, len(userIdSet))
	for userId := range userIdSet {
		userIds = append(userIds, userId)
	}
	var users []User
	if err := DB.Select("id", "username").Where("id IN ?", userIds).Find(&users).Error; err != nil {
		return
	}
	usernameById := make(map[int]string, len(users))
	for _, user := range users {
		usernameById[user.Id] = user.Username
	}
	for _, invoice := range invoices {
		if invoice != nil {
			invoice.Username = usernameById[invoice.UserId]
		}
	}
}
