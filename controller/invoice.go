package controller

import (
	"context"
	"fmt"
	"io"
	"net/mail"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// 可开票充值记录的开票状态
const (
	InvoiceTopUpStatusInsufficient = "insufficient" // 金额不足
	InvoiceTopUpStatusAvailable    = "available"    // 可申请
	InvoiceTopUpStatusApplied      = "applied"      // 已申请
	InvoiceTopUpStatusIssued       = "issued"       // 已开具
)

// invoiceAmountMeetsMinimum 判断单笔实付金额是否达到开票阈值（含等于）。
func invoiceAmountMeetsMinimum(amount, minAmount float64) bool {
	return amount >= minAmount
}

const invoiceFileMaxSize = 20 << 20 // 20MB

var invoiceAllowedExts = map[string]string{
	".pdf":  "application/pdf",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
}

// GetInvoiceConfig 用户获取开票相关配置（阈值、上线时间）
func GetInvoiceConfig(c *gin.Context) {
	setting := operation_setting.GetInvoiceSetting()
	profile, err := model.GetLatestUserInvoiceProfile(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var lastProfile any
	if profile != nil {
		lastProfile = gin.H{
			"title":  profile.Title,
			"tax_no": profile.TaxNo,
			"emails": profile.Emails,
		}
	}
	common.ApiSuccess(c, gin.H{
		"min_amount":           setting.MinAmount,
		"online_time":          setting.OnlineTime,
		"last_invoice_profile": lastProfile,
	})
}

// GetUserInvoiceTopUps 用户可参与开票的充值记录（含开票状态）
func GetUserInvoiceTopUps(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	setting := operation_setting.GetInvoiceSetting()

	topups, total, err := model.GetUserInvoiceableTopUps(
		userId, setting.OnlineTimestamp(), setting.MinAmount, pageInfo,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	topUpIds := make([]int, 0, len(topups))
	for _, topup := range topups {
		topUpIds = append(topUpIds, topup.Id)
	}
	invoiceMap, err := model.GetInvoicesByTopUpIds(topUpIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	items := make([]gin.H, 0, len(topups))
	for _, topup := range topups {
		status := InvoiceTopUpStatusAvailable
		var invoiceId int
		if invoice, ok := invoiceMap[topup.Id]; ok && invoice.Status != model.InvoiceStatusRejected {
			invoiceId = invoice.Id
			if invoice.Status == model.InvoiceStatusIssued {
				status = InvoiceTopUpStatusIssued
			} else {
				status = InvoiceTopUpStatusApplied
			}
		} else if !invoiceAmountMeetsMinimum(topup.Money, setting.MinAmount) {
			status = InvoiceTopUpStatusInsufficient
		}
		items = append(items, gin.H{
			"id":             topup.Id,
			"trade_no":       topup.TradeNo,
			"money":          topup.Money,
			"payment_method": topup.PaymentMethod,
			"complete_time":  topup.CompleteTime,
			"invoice_status": status,
			"invoice_id":     invoiceId,
		})
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

type InvoiceApplyRequest struct {
	TopUpId int    `json:"top_up_id"`
	Title   string `json:"title"`
	TaxNo   string `json:"tax_no"`
	Emails  string `json:"emails"`
}

// parseInvoiceEmails 解析收票邮箱：支持英文逗号、分号、空格分隔多个邮箱
func parseInvoiceEmails(raw string) ([]string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '，' || r == '；' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	seen := make(map[string]struct{})
	emails := make([]string, 0, len(fields))
	for _, field := range fields {
		email := strings.TrimSpace(field)
		if email == "" {
			continue
		}
		if _, err := mail.ParseAddress(email); err != nil {
			return nil, fmt.Errorf("收票邮箱格式不正确: %s", email)
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
	}
	if len(emails) == 0 {
		return nil, fmt.Errorf("请填写收票邮箱")
	}
	if len(emails) > 10 {
		return nil, fmt.Errorf("收票邮箱最多支持 10 个")
	}
	return emails, nil
}

// ApplyInvoice 用户提交开票申请
func ApplyInvoice(c *gin.Context) {
	userId := c.GetInt("id")
	var req InvoiceApplyRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.TaxNo = strings.TrimSpace(req.TaxNo)
	if req.TopUpId <= 0 || req.Title == "" {
		common.ApiErrorMsg(c, "请填写完整的公司抬头")
		return
	}
	if len([]rune(req.Title)) > 100 {
		common.ApiErrorMsg(c, "公司抬头过长")
		return
	}
	emails, err := parseInvoiceEmails(req.Emails)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}

	setting := operation_setting.GetInvoiceSetting()
	topUp := model.GetTopUpById(req.TopUpId)
	if topUp == nil || topUp.UserId != userId {
		common.ApiErrorMsg(c, "充值订单不存在")
		return
	}
	// 开票条件校验：支付成功 + 上线时间之后 + EPay 或兑换码（实付金额大于 0）+ 金额阈值
	if topUp.Status != common.TopUpStatusSuccess {
		common.ApiErrorMsg(c, "该订单未支付成功，无法申请开票")
		return
	}
	if topUp.CompleteTime < setting.OnlineTimestamp() {
		common.ApiErrorMsg(c, "该订单完成时间早于开票上线时间，无法申请开票")
		return
	}
	isEpay := topUp.PaymentProvider == model.PaymentProviderEpay
	isRedemption := topUp.PaymentMethod == model.PaymentMethodRedemption && topUp.Money > 0
	if !isEpay && !isRedemption {
		common.ApiErrorMsg(c, "该订单的支付方式不支持申请开票")
		return
	}
	if !invoiceAmountMeetsMinimum(topUp.Money, setting.MinAmount) {
		common.ApiErrorMsg(c, fmt.Sprintf("单笔充值金额达到 %v 才可申请开票", setting.MinAmount))
		return
	}

	existing, err := model.GetInvoiceByTopUpId(topUp.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if existing != nil && existing.Status != model.InvoiceStatusRejected {
		common.ApiErrorMsg(c, "该订单已提交过开票申请")
		return
	}

	now := common.GetTimestamp()
	if existing != nil {
		// 被拒绝的申请允许修改后重新提交
		existing.Title = req.Title
		existing.TaxNo = req.TaxNo
		existing.Emails = strings.Join(emails, ";")
		existing.Money = topUp.Money
		existing.Status = model.InvoiceStatusPending
		existing.Remark = ""
		existing.HandledTime = 0
		existing.UpdatedTime = now
		if err = existing.Update(); err != nil {
			common.ApiError(c, err)
			return
		}
	} else {
		invoice := &model.Invoice{
			UserId:      userId,
			TopUpId:     topUp.Id,
			TradeNo:     topUp.TradeNo,
			Money:       topUp.Money,
			Title:       req.Title,
			TaxNo:       req.TaxNo,
			Emails:      strings.Join(emails, ";"),
			Status:      model.InvoiceStatusPending,
			CreatedTime: now,
			UpdatedTime: now,
		}
		if err = invoice.Insert(); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	common.ApiSuccess(c, nil)
}

// GetUserInvoices 用户自己的开票申请记录
func GetUserInvoices(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	invoices, total, err := model.GetUserInvoices(userId, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(invoices)
	common.ApiSuccess(c, pageInfo)
}

// DownloadUserInvoice 用户下载已开具的发票（返回 R2 预签名 URL）
func DownloadUserInvoice(c *gin.Context) {
	userId := c.GetInt("id")
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	invoice, err := model.GetInvoiceById(id)
	if err != nil || invoice.UserId != userId {
		common.ApiErrorMsg(c, "开票申请不存在")
		return
	}
	if invoice.Status != model.InvoiceStatusIssued || invoice.FileKey == "" {
		common.ApiErrorMsg(c, "发票尚未开具")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	url, err := service.GetInvoiceFilePresignedURL(ctx, invoice.FileKey)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"url": url})
}

// AdminGetInvoices 管理员查询开票申请
func AdminGetInvoices(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	status, _ := strconv.Atoi(c.Query("status"))
	filter := model.InvoiceFilter{
		Keyword:  c.Query("keyword"),
		Username: c.Query("username"),
		Status:   status,
	}
	invoices, total, err := model.SearchInvoicesWithFilter(filter, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(invoices)
	common.ApiSuccess(c, pageInfo)
}

// AdminIssueInvoice 管理员开具发票：上传发票文件到 R2 并邮件通知用户
func AdminIssueInvoice(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	invoice, err := model.GetInvoiceById(id)
	if err != nil {
		common.ApiErrorMsg(c, "开票申请不存在")
		return
	}
	if invoice.Status != model.InvoiceStatusPending {
		common.ApiErrorMsg(c, "仅未处理的申请可以开具发票")
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		common.ApiErrorMsg(c, "请选择要上传的发票文件")
		return
	}
	if fileHeader.Size <= 0 || fileHeader.Size > invoiceFileMaxSize {
		common.ApiErrorMsg(c, "发票文件大小需在 20MB 以内")
		return
	}
	ext := strings.ToLower(path.Ext(fileHeader.Filename))
	contentType, ok := invoiceAllowedExts[ext]
	if !ok {
		common.ApiErrorMsg(c, "发票文件仅支持 PDF、JPG、PNG 格式")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, invoiceFileMaxSize+1))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	objectKey := service.BuildInvoiceObjectKey(invoice.UserId, invoice.Id, ext)
	if err = service.UploadInvoiceFileToR2(ctx, objectKey, data, contentType); err != nil {
		common.SysError(fmt.Sprintf("invoice R2 upload failed, effective config: %s, error: %v",
			service.InvoiceR2EffectiveConfigForLog(), err))
		common.ApiError(c, err)
		return
	}

	invoice.Status = model.InvoiceStatusIssued
	invoice.FileKey = objectKey
	invoice.HandledTime = common.GetTimestamp()
	invoice.UpdatedTime = invoice.HandledTime
	if err = invoice.Update(); err != nil {
		common.ApiError(c, err)
		return
	}

	// 发送邮件通知（正文附预签名下载链接）
	emailSent := true
	emailError := ""
	if err = sendInvoiceIssuedEmail(invoice); err != nil {
		emailSent = false
		emailError = err.Error()
		common.SysError(fmt.Sprintf(
			"failed to send invoice email: invoice_id=%d, user_id=%d, recipients=%s, file_key=%q, error_type=%T, error=%v",
			invoice.Id, invoice.UserId, maskInvoiceEmails(invoice.Emails), invoice.FileKey, err, err,
		))
	}
	common.ApiSuccess(c, gin.H{
		"email_sent":  emailSent,
		"email_error": emailError,
	})
}

// AdminResendInvoiceEmail re-sends the notification for an issued invoice.
func AdminResendInvoiceEmail(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	invoice, err := model.GetInvoiceById(id)
	if err != nil {
		common.ApiErrorMsg(c, "开票申请不存在")
		return
	}
	if invoice.Status != model.InvoiceStatusIssued {
		common.ApiErrorMsg(c, "仅已开具的发票可以重发邮件")
		return
	}
	if strings.TrimSpace(invoice.FileKey) == "" {
		common.ApiErrorMsg(c, "发票文件不存在，无法重发邮件")
		return
	}
	if err = sendInvoiceIssuedEmail(invoice); err != nil {
		common.SysError(fmt.Sprintf(
			"failed to resend invoice email: invoice_id=%d, user_id=%d, recipients=%s, file_key=%q, error_type=%T, error=%v",
			invoice.Id, invoice.UserId, maskInvoiceEmails(invoice.Emails), invoice.FileKey, err, err,
		))
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}

// AdminTestInvoiceR2Connection 测试发票 R2 配置（可用表单中未保存的值测试，空字段回落到已保存配置）
func AdminTestInvoiceR2Connection(c *gin.Context) {
	var req struct {
		AccountID    string `json:"account_id"`
		Bucket       string `json:"bucket"`
		Endpoint     string `json:"endpoint"`
		AccessKeyID  string `json:"access_key_id"`
		Secret       string `json:"secret"`
		ObjectPrefix string `json:"object_prefix"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	override := &operation_setting.InvoiceSetting{
		R2AccountID:       req.AccountID,
		R2Bucket:          req.Bucket,
		R2Endpoint:        req.Endpoint,
		R2AccessKeyID:     req.AccessKeyID,
		R2SecretAccessKey: req.Secret,
		R2ObjectPrefix:    req.ObjectPrefix,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := service.TestInvoiceR2Connection(ctx, override); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}

// AdminRejectInvoice 管理员拒绝开票申请
func AdminRejectInvoice(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	var req struct {
		Remark string `json:"remark"`
	}
	if err = common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	invoice, err := model.GetInvoiceById(id)
	if err != nil {
		common.ApiErrorMsg(c, "开票申请不存在")
		return
	}
	if invoice.Status != model.InvoiceStatusPending {
		common.ApiErrorMsg(c, "仅未处理的申请可以拒绝")
		return
	}
	invoice.Status = model.InvoiceStatusRejected
	invoice.Remark = strings.TrimSpace(req.Remark)
	invoice.HandledTime = common.GetTimestamp()
	invoice.UpdatedTime = invoice.HandledTime
	if err = invoice.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// sendInvoiceIssuedEmail 发票开具完成邮件通知，正文包含 R2 预签名下载链接
func sendInvoiceIssuedEmail(invoice *model.Invoice) error {
	if strings.TrimSpace(invoice.Emails) == "" {
		return fmt.Errorf("收票邮箱为空")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	downloadURL, err := service.GetInvoiceFilePresignedURL(ctx, invoice.FileKey)
	if err != nil {
		return fmt.Errorf("生成发票下载链接失败: %w", err)
	}
	expireHours := operation_setting.GetInvoiceSetting().R2URLExpireHours
	if expireHours <= 0 {
		expireHours = 24
	}
	subject := fmt.Sprintf("%s发票开具通知", common.SystemName)
	content := fmt.Sprintf(
		"<p>您好，您申请的发票已开具。</p>"+
			"<p>充值订单：<strong>%s</strong><br>"+
			"开票金额：<strong>%.2f</strong><br>"+
			"公司抬头：<strong>%s</strong></p>"+
			"<p>请点击以下链接下载发票文件（链接 %d 小时内有效）：</p>"+
			"<p><a href=\"%s\">下载发票</a></p>"+
			"<p>如链接已过期，请登录<a href=\"%s\">%s</a>，在「自助发票」页面的申请记录中重新获取下载链接。</p>",
		invoice.TradeNo, invoice.Money, invoice.Title, expireHours, downloadURL,
		system_setting.ServerAddress, common.SystemName)
	if err = common.SendEmail(subject, invoice.Emails, content); err != nil {
		return fmt.Errorf("发送发票邮件失败: %w", err)
	}
	return nil
}

func maskInvoiceEmails(emails string) string {
	items := strings.Split(emails, ";")
	for i, item := range items {
		items[i] = common.MaskEmail(strings.TrimSpace(item))
	}
	return strings.Join(items, ";")
}
