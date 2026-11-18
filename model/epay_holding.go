package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const epayHoldingSeconds int64 = 30 * 60

var ErrEpayHolding = errors.New("已有待支付的易支付订单，请先完成或取消")

// One row per user. Writing this row before any transaction reads serializes
// checkouts across channels and scenes, including on SQLite (no FOR UPDATE).
type EpayCheckoutSlot struct {
	UserID   int    `gorm:"primaryKey;autoIncrement:false"`
	TradeNo  string `gorm:"type:varchar(255)"`
	FormJSON string `gorm:"type:text"`
}

type EpayPendingOrder struct {
	Status                string              `json:"status"`
	TradeNo               string              `json:"trade_no"`
	EpayPayCNY            string              `json:"amount_cny"`
	CreateTime            int64               `json:"-"`
	ExpiresAt             int64               `json:"expires_at"`
	EpayChannelId         int                 `json:"-"`
	EpayChannelRevisionId int                 `json:"-"`
	EpayChannelName       string              `json:"channel_name,omitempty"`
	PaymentMethod         string              `json:"payment_method,omitempty"`
	Money                 float64             `json:"-"`
	Scene                 types.PaymentScene  `json:"scene"`
	Form                  *types.CheckoutForm `json:"form,omitempty" gorm:"-"`
}

func pendingEpayOrders(tx *gorm.DB, table any, userID int) *gorm.DB {
	return tx.Model(table).Where("user_id = ? AND payment_provider = ? AND status = ?", userID, PaymentProviderEpay, common.TopUpStatusPending)
}

func epayOrderExpired(created, now int64) bool {
	return created <= now-epayHoldingSeconds
}

func findPendingEpayOrder(tx *gorm.DB, userID int, now int64) (*EpayPendingOrder, error) {
	var first *EpayPendingOrder
	for _, scene := range []types.PaymentScene{types.PaymentSceneTopup, types.PaymentSceneSubscription} {
		var table any = &TopUp{}
		if scene == types.PaymentSceneSubscription {
			table = &SubscriptionOrder{}
		}
		var row EpayPendingOrder
		res := pendingEpayOrders(tx, table, userID).Where("create_time > ?", now-epayHoldingSeconds).Select("trade_no, epay_pay_cny, create_time, epay_channel_id, epay_channel_revision_id, epay_channel_name, payment_method, money").Order("create_time, id").Limit(1).Scan(&row)
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 0 {
			continue
		}
		row.Scene = scene
		row.Status = common.TopUpStatusPending
		row.ExpiresAt = row.CreateTime + epayHoldingSeconds
		if first == nil || row.CreateTime < first.CreateTime {
			first = &row
		}
	}
	return first, nil
}

func reserveEpayCheckout(tx *gorm.DB, userID int, tradeNo, formJSON string) error {
	if userID <= 0 {
		return errors.New("无效的用户")
	}
	slot := EpayCheckoutSlot{UserID: userID, TradeNo: tradeNo, FormJSON: formJSON}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"trade_no", "form_json"})}).Create(&slot).Error
}

func checkEpayPendingLimit(tx *gorm.DB, userID int, now int64) error {
	for _, table := range []any{&TopUp{}, &SubscriptionOrder{}} {
		if err := pendingEpayOrders(tx, table, userID).Where("create_time <= ?", now-epayHoldingSeconds).Update("status", common.TopUpStatusExpired).Error; err != nil {
			return err
		}
	}
	pending, err := findPendingEpayOrder(tx, userID, now)
	if err != nil {
		return err
	}
	if pending != nil {
		return ErrEpayHolding
	}
	return nil
}

func CurrentEpayOrder(userID int) (*EpayPendingOrder, error) {
	order, err := findPendingEpayOrder(DB, userID, time.Now().Unix())
	if err != nil || order == nil {
		return order, err
	}
	return attachEpayForm(userID, order)
}

// Look up the original checkout even after it has released its holding slot.
func GetEpayOrderStatus(userID int, scene types.PaymentScene, tradeNo string) (*EpayPendingOrder, error) {
	var table any
	q := DB.Where("user_id = ? AND trade_no = ? AND payment_provider = ?", userID, tradeNo, PaymentProviderEpay)
	switch scene {
	case types.PaymentSceneTopup:
		table = &TopUp{}
		q = q.Where("amount > ?", 0) // Exclude subscription audit rows.
	case types.PaymentSceneSubscription:
		table = &SubscriptionOrder{}
	default:
		return nil, ErrTopUpNotFound
	}
	var order EpayPendingOrder
	res := q.Model(table).Select("trade_no, status, epay_pay_cny, create_time, epay_channel_name, payment_method").Limit(1).Scan(&order)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrTopUpNotFound
	}
	order.Scene, order.ExpiresAt = scene, order.CreateTime+epayHoldingSeconds
	// A settlement started just before the deadline may still be committing.
	// Withhold the form at the deadline, but report only committed statuses.
	if order.Status == common.TopUpStatusPending && !epayOrderExpired(order.CreateTime, time.Now().Unix()) {
		return attachEpayForm(userID, &order)
	}
	return &order, nil
}

func attachEpayForm(userID int, order *EpayPendingOrder) (*EpayPendingOrder, error) {
	var slot EpayCheckoutSlot
	res := DB.Where("user_id = ? AND trade_no = ?", userID, order.TradeNo).Limit(1).Find(&slot)
	if res.Error != nil {
		return nil, res.Error
	}
	if slot.FormJSON != "" {
		var form types.CheckoutForm
		if err := common.UnmarshalJsonStr(slot.FormJSON, &form); err != nil {
			return nil, err
		}
		order.Form = &form
	}
	return order, nil
}

// Only pending orders belonging to this user can transition. A stale browser
// tab cannot cancel a replacement order. Payment transactions check the same state.
func CancelEpayOrder(userID int, tradeNo string, scene types.PaymentScene) error {
	var table any
	switch scene {
	case types.PaymentSceneTopup:
		table = &TopUp{}
	case types.PaymentSceneSubscription:
		table = &SubscriptionOrder{}
	default:
		return ErrTopUpStatusInvalid
	}
	r := pendingEpayOrders(DB, table, userID).Where("trade_no = ? AND create_time > ?", tradeNo, time.Now().Unix()-epayHoldingSeconds).Update("status", common.TopUpStatusCancelled)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrTopUpStatusInvalid
	}
	return nil
}

// ExpireDueEpayOrders updates history and releases daily reservations in bounded
// batches. Settlement checks the deadline independently under the order lock.
func ExpireDueEpayOrders(now int64, batchSize int) error {
	for _, table := range []any{&TopUp{}, &SubscriptionOrder{}} {
		due := func() *gorm.DB {
			return DB.Model(table).Where("payment_provider = ? AND status = ? AND create_time <= ?", PaymentProviderEpay, common.TopUpStatusPending, now-epayHoldingSeconds)
		}
		for {
			var ids []int
			if err := due().Order("id").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
				return err
			}
			if len(ids) == 0 {
				break
			}
			if err := due().Where("id IN ?", ids).Update("status", common.TopUpStatusExpired).Error; err != nil {
				return err
			}
			if len(ids) < batchSize {
				break
			}
		}
	}
	return nil
}
