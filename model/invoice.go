package model

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// 发票申请状态
const (
	InvoiceStatusPending  = 1 // 已申请（未处理）
	InvoiceStatusIssued   = 2 // 已开具
	InvoiceStatusRejected = 3 // 已拒绝
)

// Invoice 用户开票申请
type Invoice struct {
	Id            int            `json:"id"`
	UserId        int            `json:"user_id" gorm:"index"`
	Username      string         `json:"username" gorm:"-"`
	PaymentMethod string         `json:"payment_method" gorm:"-"`
	TopUpId       int            `json:"top_up_id" gorm:"uniqueIndex"`
	TradeNo       string         `json:"trade_no" gorm:"type:varchar(255);index"`
	Money         float64        `json:"money"`
	Title         string         `json:"title" gorm:"type:varchar(255)"`
	TaxNo         string         `json:"tax_no" gorm:"type:varchar(64)"`
	Emails        string         `json:"emails" gorm:"type:text"`
	Status        int            `json:"status" gorm:"index"`
	FileKey       string         `json:"file_key" gorm:"type:varchar(512)"`
	Remark        string         `json:"remark" gorm:"type:text"`
	HandledTime   int64          `json:"handled_time"`
	CreatedTime   int64          `json:"created_time"`
	UpdatedTime   int64          `json:"updated_time"`
	TopUpCount    int            `json:"top_up_count" gorm:"-"`
	Items         []*InvoiceItem `json:"items,omitempty" gorm:"-"`
}

// InvoiceItem is an immutable snapshot of a recharge included in an invoice.
// top_up_id is unique so a recharge can never be invoiced twice, including after rejection.
type InvoiceItem struct {
	Id            int     `json:"id"`
	InvoiceId     int     `json:"invoice_id" gorm:"index"`
	TopUpId       int     `json:"top_up_id" gorm:"uniqueIndex"`
	TradeNo       string  `json:"trade_no" gorm:"type:varchar(255);index"`
	Money         float64 `json:"money"`
	PaymentMethod string  `json:"payment_method" gorm:"type:varchar(50)"`
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
	fillInvoiceItems([]*Invoice{&invoice})
	return &invoice, nil
}

// GetInvoiceByTopUpId 查询某充值订单的开票申请，不存在时返回 nil, nil
func GetInvoiceByTopUpId(topUpId int) (*Invoice, error) {
	var item InvoiceItem
	if err := DB.Where("top_up_id = ?", topUpId).First(&item).Error; err == nil {
		return GetInvoiceById(item.InvoiceId)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var invoice Invoice
	if err := DB.Where("top_up_id = ?", topUpId).First(&invoice).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &invoice, nil
}

// GetInvoicesByTopUpIds 批量查询充值订单对应的开票申请
func GetInvoicesByTopUpIds(topUpIds []int) (map[int]*Invoice, error) {
	result := make(map[int]*Invoice, len(topUpIds))
	if len(topUpIds) == 0 {
		return result, nil
	}
	var items []*InvoiceItem
	if err := DB.Where("top_up_id IN ?", topUpIds).Find(&items).Error; err != nil {
		return nil, err
	}
	invoiceIDs := make([]int, 0, len(items))
	for _, item := range items {
		invoiceIDs = append(invoiceIDs, item.InvoiceId)
	}
	var invoices []*Invoice
	if len(invoiceIDs) > 0 {
		if err := DB.Where("id IN ?", invoiceIDs).Find(&invoices).Error; err != nil {
			return nil, err
		}
	}
	byID := make(map[int]*Invoice, len(invoices))
	for _, invoice := range invoices {
		byID[invoice.Id] = invoice
	}
	for _, item := range items {
		if invoice := byID[item.InvoiceId]; invoice != nil {
			result[item.TopUpId] = invoice
		}
	}
	// Legacy rows are backfilled on migration, but retain this fallback for partially migrated databases.
	var legacy []*Invoice
	if err := DB.Where("top_up_id IN ?", topUpIds).Find(&legacy).Error; err != nil {
		return nil, err
	}
	for _, invoice := range legacy {
		if _, ok := result[invoice.TopUpId]; !ok {
			result[invoice.TopUpId] = invoice
		}
	}
	return result, nil
}

// GetUserInvoiceableTopUps 查询用户可参与开票的充值记录：
// 支付成功、在完成时间上线之后、支付通道为 EPay 或兑换码兑换，且实付金额大于等于开票阈值。
func GetUserInvoiceableTopUps(userId int, onlineTs int64, minAmount float64, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	query := DB.Model(&TopUp{}).
		Where("user_id = ? AND status = ?", userId, common.TopUpStatusSuccess).
		Where("complete_time >= ?", onlineTs).
		Where("(payment_provider = ? OR payment_method = ?)", PaymentProviderEpay, PaymentMethodRedemption).
		Where("money > ?", 0)

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

var (
	ErrInvoiceInvalidTopUps = errors.New("所选充值订单不可申请开票")
	ErrInvoiceTopUpClaimed  = errors.New("所选充值订单已申请过开票")
	ErrInvoiceBelowMinimum  = errors.New("合计充值金额未达到开票门槛")
)

// ApplyInvoice atomically validates and claims all selected top-ups.
func ApplyInvoice(userID int, topUpIDs []int, title, taxNo, emails string, onlineTs int64, minAmount float64, now int64) error {
	if len(topUpIDs) == 0 || math.IsNaN(minAmount) || math.IsInf(minAmount, 0) {
		return ErrInvoiceInvalidTopUps
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var topups []*TopUp
		query := lockForUpdate(tx).Where("id IN ? AND user_id = ?", topUpIDs, userID)
		if err := query.Find(&topups).Error; err != nil {
			return err
		}
		if len(topups) != len(topUpIDs) {
			return ErrInvoiceInvalidTopUps
		}
		byID := make(map[int]*TopUp, len(topups))
		for _, topup := range topups {
			byID[topup.Id] = topup
		}
		orderedTopUps := make([]*TopUp, 0, len(topUpIDs))
		total := decimal.Zero
		for _, topUpID := range topUpIDs {
			topup, ok := byID[topUpID]
			if !ok {
				return ErrInvoiceInvalidTopUps
			}
			orderedTopUps = append(orderedTopUps, topup)
			isSupported := topup.PaymentProvider == PaymentProviderEpay || (topup.PaymentMethod == PaymentMethodRedemption && topup.Money > 0)
			if topup.Status != common.TopUpStatusSuccess || topup.CompleteTime < onlineTs || !isSupported || topup.Money <= 0 || math.IsNaN(topup.Money) || math.IsInf(topup.Money, 0) {
				return ErrInvoiceInvalidTopUps
			}
			total = total.Add(decimal.NewFromFloat(topup.Money))
		}
		if total.LessThan(decimal.NewFromFloat(minAmount)) {
			return ErrInvoiceBelowMinimum
		}
		var claimed []*InvoiceItem
		if err := tx.Where("top_up_id IN ?", topUpIDs).Find(&claimed).Error; err != nil {
			return err
		}
		if len(claimed) > 0 {
			return ErrInvoiceTopUpClaimed
		}
		invoice := &Invoice{UserId: userID, TopUpId: topUpIDs[0], TradeNo: orderedTopUps[0].TradeNo, Money: total.Round(2).InexactFloat64(), Title: title, TaxNo: taxNo, Emails: emails, Status: InvoiceStatusPending, CreatedTime: now, UpdatedTime: now}
		if err := tx.Create(invoice).Error; err != nil {
			return err
		}
		items := make([]InvoiceItem, 0, len(orderedTopUps))
		for _, topup := range orderedTopUps {
			items = append(items, InvoiceItem{InvoiceId: invoice.Id, TopUpId: topup.Id, TradeNo: topup.TradeNo, Money: topup.Money, PaymentMethod: topup.PaymentMethod})
		}
		return tx.Create(&items).Error
	})
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
	fillInvoiceItems(invoices)
	return invoices, total, nil
}

// GetLatestUserInvoiceProfile returns the invoice fields most recently submitted by a user.
func GetLatestUserInvoiceProfile(userId int) (*Invoice, error) {
	var invoice Invoice
	result := DB.Select("title", "tax_no", "emails").
		Where("user_id = ?", userId).
		Order("updated_time desc").
		Order("id desc").
		Limit(1).
		Find(&invoice)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return &invoice, nil
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
		itemQuery := DB.Model(&InvoiceItem{}).Select("invoice_id").Where("trade_no LIKE ?", like)
		query = query.Where("trade_no LIKE ? OR title LIKE ? OR id IN (?)", like, like, itemQuery)
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
	fillInvoiceItems(invoices)
	if err = fillInvoicePaymentMethods(invoices); err != nil {
		return nil, 0, err
	}
	fillInvoiceUsernames(invoices)
	return invoices, total, nil
}

func fillInvoiceItems(invoices []*Invoice) {
	if len(invoices) == 0 {
		return
	}
	ids := make([]int, 0, len(invoices))
	for _, invoice := range invoices {
		ids = append(ids, invoice.Id)
	}
	var items []*InvoiceItem
	if DB.Where("invoice_id IN ?", ids).Order("id asc").Find(&items).Error != nil {
		return
	}
	byID := make(map[int][]*InvoiceItem)
	for _, item := range items {
		byID[item.InvoiceId] = append(byID[item.InvoiceId], item)
	}
	for _, invoice := range invoices {
		invoice.Items = byID[invoice.Id]
		invoice.TopUpCount = len(invoice.Items)
		if invoice.TopUpCount == 0 && invoice.TopUpId > 0 {
			invoice.TopUpCount = 1
		}
	}
}

// BackfillInvoiceItems creates a snapshot item for historical single-top-up invoices.
func BackfillInvoiceItems() error {
	var invoices []*Invoice
	if err := DB.Where("top_up_id > 0").Find(&invoices).Error; err != nil {
		return err
	}
	for _, invoice := range invoices {
		var count int64
		if err := DB.Model(&InvoiceItem{}).Where("invoice_id = ?", invoice.Id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		topup := GetTopUpById(invoice.TopUpId)
		item := &InvoiceItem{InvoiceId: invoice.Id, TopUpId: invoice.TopUpId, TradeNo: invoice.TradeNo, Money: invoice.Money}
		if topup != nil {
			item.TradeNo, item.Money, item.PaymentMethod = topup.TradeNo, topup.Money, topup.PaymentMethod
		}
		if err := DB.Create(item).Error; err != nil {
			return fmt.Errorf("backfill invoice item %d: %w", invoice.Id, err)
		}
	}
	return nil
}

func fillInvoicePaymentMethods(invoices []*Invoice) error {
	if len(invoices) == 0 {
		return nil
	}
	fillInvoiceItems(invoices)
	for _, invoice := range invoices {
		if invoice != nil && len(invoice.Items) > 0 {
			invoice.PaymentMethod = invoice.Items[0].PaymentMethod
		}
	}
	topUpIds := make([]int, 0, len(invoices))
	for _, invoice := range invoices {
		if invoice != nil && invoice.TopUpId > 0 {
			topUpIds = append(topUpIds, invoice.TopUpId)
		}
	}
	if len(topUpIds) == 0 {
		return nil
	}
	var topups []TopUp
	if err := DB.Select("id", "payment_method").Where("id IN ?", topUpIds).Find(&topups).Error; err != nil {
		return err
	}
	paymentMethodByTopUpId := make(map[int]string, len(topups))
	for _, topup := range topups {
		paymentMethodByTopUpId[topup.Id] = topup.PaymentMethod
	}
	for _, invoice := range invoices {
		if invoice != nil {
			invoice.PaymentMethod = paymentMethodByTopUpId[invoice.TopUpId]
		}
	}
	return nil
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
