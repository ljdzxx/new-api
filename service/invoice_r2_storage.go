package service

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const invoiceR2UploadTimeout = 5 * time.Minute

// BuildInvoiceObjectKey 生成发票文件的 R2 object key：<prefix>/<YYYYMMDD>/<userID>/<invoiceID><ext>
func BuildInvoiceObjectKey(userId int, invoiceId int, ext string) string {
	setting := operation_setting.GetInvoiceSetting()
	datePath := time.Now().Format("20060102")
	userPath := strconv.Itoa(userId)
	if userId <= 0 {
		userPath = "unknown"
	}
	name := fmt.Sprintf("%d%s", invoiceId, ext)
	return path.Join(setting.ObjectPrefix(), datePath, userPath, name)
}

// UploadInvoiceFileToR2 上传发票文件到 R2
func UploadInvoiceFileToR2(ctx context.Context, objectKey string, data []byte, contentType string) error {
	setting := operation_setting.GetInvoiceSetting()
	client, err := newInvoiceR2S3Client(setting)
	if err != nil {
		return err
	}

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(setting.R2Bucket),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("upload invoice to R2 failed: %w", err)
	}
	return nil
}

// GetInvoiceFilePresignedURL 生成发票文件的预签名下载 URL
func GetInvoiceFilePresignedURL(ctx context.Context, objectKey string) (string, error) {
	setting := operation_setting.GetInvoiceSetting()
	client, err := newInvoiceR2S3Client(setting)
	if err != nil {
		return "", err
	}

	presignClient := s3.NewPresignClient(client)
	presigned, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(setting.R2Bucket),
		Key:    aws.String(objectKey),
	}, func(options *s3.PresignOptions) {
		options.Expires = setting.URLExpireDuration()
	})
	if err != nil {
		return "", fmt.Errorf("presign R2 invoice URL failed: %w", err)
	}
	return presigned.URL, nil
}

// InvoiceR2Configured 发票 R2 存储是否已启用且配置完整
func InvoiceR2Configured() bool {
	setting := operation_setting.GetInvoiceSetting()
	return setting.R2Enabled && len(missingInvoiceR2Settings(setting, setting.Endpoint())) == 0
}

func newInvoiceR2S3Client(setting *operation_setting.InvoiceSetting) (*s3.Client, error) {
	endpoint := setting.Endpoint()
	if !setting.R2Enabled {
		return nil, fmt.Errorf("发票 R2 存储未启用，请先在系统设置-运营设置-发票设置中启用并配置")
	}
	if missing := missingInvoiceR2Settings(setting, endpoint); len(missing) > 0 {
		return nil, fmt.Errorf("发票 R2 存储已启用但配置不完整，缺少: %s", strings.Join(missing, ", "))
	}

	cfg := aws.Config{
		Region:      "auto",
		Credentials: credentials.NewStaticCredentialsProvider(setting.R2AccessKeyID, setting.R2SecretAccessKey, ""),
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	return client, nil
}

func missingInvoiceR2Settings(setting *operation_setting.InvoiceSetting, endpoint string) []string {
	var missing []string
	if strings.TrimSpace(endpoint) == "" {
		missing = append(missing, "R2 Endpoint or R2 Account ID")
	}
	if strings.TrimSpace(setting.R2Bucket) == "" {
		missing = append(missing, "R2 Bucket")
	}
	if strings.TrimSpace(setting.R2AccessKeyID) == "" {
		missing = append(missing, "R2 Access Key ID")
	}
	if strings.TrimSpace(setting.R2SecretAccessKey) == "" {
		missing = append(missing, "R2 Secret Access Key")
	}
	return missing
}
