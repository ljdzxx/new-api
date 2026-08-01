package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupInvoiceTest(t *testing.T) {
	t.Helper()
	if common.UsingSQLite {
		require.NoError(t, ensureUsersTableSQLite())
	} else {
		require.NoError(t, DB.AutoMigrate(&User{}))
	}
	require.NoError(t, DB.AutoMigrate(&TopUp{}, &Invoice{}))
	require.NoError(t, DB.Exec("DELETE FROM invoices").Error)
	require.NoError(t, DB.Exec("DELETE FROM top_ups").Error)
	require.NoError(t, DB.Exec("DELETE FROM users").Error)
}

func insertInvoiceTopUp(t *testing.T, userId int, provider, method, status string, money float64, completeTime int64) *TopUp {
	t.Helper()
	e2eTopUpSeq++
	topup := &TopUp{
		UserId:          userId,
		Amount:          1,
		Money:           money,
		TradeNo:         fmt.Sprintf("INV-TEST-%d-%d", time.Now().UnixNano(), e2eTopUpSeq),
		PaymentMethod:   method,
		PaymentProvider: provider,
		CreateTime:      completeTime,
		CompleteTime:    completeTime,
		Status:          status,
	}
	require.NoError(t, DB.Create(topup).Error)
	return topup
}

func TestGetUserInvoiceableTopUpsFiltering(t *testing.T) {
	setupInvoiceTest(t)
	user := createRegisteredUser(t, "invoice_user")
	other := createRegisteredUser(t, "invoice_other")

	onlineTs := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local).Unix()
	after := onlineTs + 3600
	before := onlineTs - 3600

	// 符合条件：EPay 且支付成功（金额达标 / 不达标都应出现在列表中）
	eligibleEpay := insertInvoiceTopUp(t, user.Id, PaymentProviderEpay, "alipay", common.TopUpStatusSuccess, 500, after)
	eligibleEpayLowAmount := insertInvoiceTopUp(t, user.Id, PaymentProviderEpay, "wxpay", common.TopUpStatusSuccess, 100, after)
	// 符合条件：兑换码兑换且实付金额大于 0
	eligibleRedemption := insertInvoiceTopUp(t, user.Id, PaymentProviderMall, PaymentMethodRedemption, common.TopUpStatusSuccess, 50, after)

	// 不符合：未支付成功
	insertInvoiceTopUp(t, user.Id, PaymentProviderEpay, "alipay", common.TopUpStatusPending, 500, after)
	// 不符合：非 EPay 通道
	insertInvoiceTopUp(t, user.Id, PaymentProviderStripe, "stripe", common.TopUpStatusSuccess, 500, after)
	// 不符合：兑换码实付金额为 0
	insertInvoiceTopUp(t, user.Id, PaymentProviderMall, PaymentMethodRedemption, common.TopUpStatusSuccess, 0, after)
	// 不符合：上线时间之前完成
	insertInvoiceTopUp(t, user.Id, PaymentProviderEpay, "alipay", common.TopUpStatusSuccess, 500, before)
	// 不符合：其他用户
	insertInvoiceTopUp(t, other.Id, PaymentProviderEpay, "alipay", common.TopUpStatusSuccess, 500, after)

	pageInfo := &common.PageInfo{Page: 1, PageSize: 10}
	topups, total, err := GetUserInvoiceableTopUps(user.Id, onlineTs, pageInfo)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)

	ids := make(map[int]bool)
	for _, topup := range topups {
		ids[topup.Id] = true
	}
	assert.True(t, ids[eligibleEpay.Id])
	assert.True(t, ids[eligibleEpayLowAmount.Id])
	assert.True(t, ids[eligibleRedemption.Id])
}

func TestInvoiceUniqueTopUpAndReapply(t *testing.T) {
	setupInvoiceTest(t)
	user := createRegisteredUser(t, "invoice_reapply")

	onlineTs := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local).Unix()
	topup := insertInvoiceTopUp(t, user.Id, PaymentProviderEpay, "alipay", common.TopUpStatusSuccess, 500, onlineTs+3600)

	now := common.GetTimestamp()
	invoice := &Invoice{
		UserId:      user.Id,
		TopUpId:     topup.Id,
		TradeNo:     topup.TradeNo,
		Money:       topup.Money,
		Title:       "测试公司",
		TaxNo:       "91110000TEST00000X",
		Emails:      "a@example.com;b@example.com",
		Status:      InvoiceStatusPending,
		CreatedTime: now,
		UpdatedTime: now,
	}
	require.NoError(t, invoice.Insert())

	// 同一订单不允许创建第二条申请
	dup := &Invoice{
		UserId:  user.Id,
		TopUpId: topup.Id,
		TradeNo: topup.TradeNo,
		Status:  InvoiceStatusPending,
	}
	assert.Error(t, dup.Insert())

	// 拒绝后可通过更新原记录重新申请
	existing, err := GetInvoiceByTopUpId(topup.Id)
	require.NoError(t, err)
	require.NotNil(t, existing)
	existing.Status = InvoiceStatusRejected
	require.NoError(t, existing.Update())

	existing.Status = InvoiceStatusPending
	existing.Title = "重新提交的公司"
	require.NoError(t, existing.Update())

	reloaded, err := GetInvoiceByTopUpId(topup.Id)
	require.NoError(t, err)
	assert.Equal(t, InvoiceStatusPending, reloaded.Status)
	assert.Equal(t, "重新提交的公司", reloaded.Title)
}

func TestGetLatestUserInvoiceProfile(t *testing.T) {
	setupInvoiceTest(t)
	user := createRegisteredUser(t, "invoice_profile")
	other := createRegisteredUser(t, "invoice_profile_other")

	firstTopUp := insertInvoiceTopUp(t, user.Id, PaymentProviderEpay, "alipay", common.TopUpStatusSuccess, 500, 100)
	latestTopUp := insertInvoiceTopUp(t, user.Id, PaymentProviderEpay, "alipay", common.TopUpStatusSuccess, 600, 200)
	otherTopUp := insertInvoiceTopUp(t, other.Id, PaymentProviderEpay, "alipay", common.TopUpStatusSuccess, 700, 300)

	for _, invoice := range []*Invoice{
		{UserId: user.Id, TopUpId: firstTopUp.Id, Title: "旧抬头", TaxNo: "OLD", Emails: "old@example.com", UpdatedTime: 100},
		{UserId: user.Id, TopUpId: latestTopUp.Id, Title: "最新抬头", TaxNo: "NEW", Emails: "new@example.com", UpdatedTime: 200},
		{UserId: other.Id, TopUpId: otherTopUp.Id, Title: "其他用户", TaxNo: "OTHER", Emails: "other@example.com", UpdatedTime: 300},
	} {
		require.NoError(t, invoice.Insert())
	}

	profile, err := GetLatestUserInvoiceProfile(user.Id)
	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Equal(t, "最新抬头", profile.Title)
	assert.Equal(t, "NEW", profile.TaxNo)
	assert.Equal(t, "new@example.com", profile.Emails)

	emptyProfile, err := GetLatestUserInvoiceProfile(999999)
	require.NoError(t, err)
	assert.Nil(t, emptyProfile)
}

func TestSearchInvoicesWithFilterIncludesPaymentMethod(t *testing.T) {
	setupInvoiceTest(t)
	user := createRegisteredUser(t, "invoice_payment_method")
	topup := insertInvoiceTopUp(t, user.Id, PaymentProviderEpay, "wxpay", common.TopUpStatusSuccess, 500, 100)
	require.NoError(t, (&Invoice{
		UserId: user.Id, TopUpId: topup.Id, TradeNo: topup.TradeNo, Status: InvoiceStatusPending,
	}).Insert())

	invoices, total, err := SearchInvoicesWithFilter(InvoiceFilter{}, &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, invoices, 1)
	assert.Equal(t, "wxpay", invoices[0].PaymentMethod)
}
