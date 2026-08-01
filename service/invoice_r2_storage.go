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

// TestInvoiceR2Connection 测试发票 R2 配置是否可用。
// override 中非空字段会覆盖已保存的配置（密钥等不回显的字段传空时自动回落到已保存值）。
// 验证链路：配置完整性 -> HeadBucket（凭证/Endpoint/桶）-> 写入并删除探针对象（签名/写权限）。
func TestInvoiceR2Connection(ctx context.Context, override *operation_setting.InvoiceSetting) error {
	effective := *operation_setting.GetInvoiceSetting()
	if override != nil {
		if v := strings.TrimSpace(override.R2AccountID); v != "" {
			effective.R2AccountID = v
		}
		if v := strings.TrimSpace(override.R2Bucket); v != "" {
			effective.R2Bucket = v
		}
		if v := strings.TrimSpace(override.R2Endpoint); v != "" {
			effective.R2Endpoint = v
		}
		if v := strings.TrimSpace(override.R2AccessKeyID); v != "" {
			effective.R2AccessKeyID = v
		}
		if v := strings.TrimSpace(override.R2SecretAccessKey); v != "" {
			effective.R2SecretAccessKey = v
		}
		if v := strings.TrimSpace(override.R2ObjectPrefix); v != "" {
			effective.R2ObjectPrefix = v
		}
	}

	client, err := buildInvoiceR2S3Client(&effective)
	if err != nil {
		return err
	}

	if _, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(effective.R2Bucket),
	}); err != nil {
		return fmt.Errorf("无法访问存储桶 %q（请检查 R2 Account ID/Endpoint、Access Key ID 与 Secret 是否匹配、桶名称是否正确）: %w", effective.R2Bucket, err)
	}

	probeKey := path.Join(effective.ObjectPrefix(), ".connection-probe")
	if _, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(effective.R2Bucket),
		Key:         aws.String(probeKey),
		Body:        bytes.NewReader([]byte("ok")),
		ContentType: aws.String("text/plain"),
	}); err != nil {
		return fmt.Errorf("存储桶可访问但写入测试失败（请确认 R2 API Token 具有该桶的 Object Read & Write 权限）: %w", err)
	}
	_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(effective.R2Bucket),
		Key:    aws.String(probeKey),
	})
	return nil
}

func newInvoiceR2S3Client(setting *operation_setting.InvoiceSetting) (*s3.Client, error) {
	if !setting.R2Enabled {
		return nil, fmt.Errorf("发票 R2 存储未启用，请先在系统设置-运营设置-发票设置中启用并配置")
	}
	return buildInvoiceR2S3Client(setting)
}

func buildInvoiceR2S3Client(setting *operation_setting.InvoiceSetting) (*s3.Client, error) {
	endpoint := setting.Endpoint()
	if missing := missingInvoiceR2Settings(setting, endpoint); len(missing) > 0 {
		return nil, fmt.Errorf("发票 R2 存储配置不完整，缺少: %s", strings.Join(missing, ", "))
	}

	cfg := aws.Config{
		Region: "auto",
		Credentials: credentials.NewStaticCredentialsProvider(
			strings.TrimSpace(setting.R2AccessKeyID),
			strings.TrimSpace(setting.R2SecretAccessKey), ""),
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
		// Cloudflare R2 不支持新版 SDK 默认的 CRC32 请求校验和，
		// 需改为 WhenRequired，否则会出现 SignatureDoesNotMatch
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	return client, nil
}

// InvoiceR2EffectiveConfigForLog 返回脱敏后的有效配置，用于故障排查日志
func InvoiceR2EffectiveConfigForLog() string {
	setting := operation_setting.GetInvoiceSetting()
	accessKeyId := strings.TrimSpace(setting.R2AccessKeyID)
	maskedKey := ""
	if len(accessKeyId) > 4 {
		maskedKey = accessKeyId[:4] + "***"
	} else if accessKeyId != "" {
		maskedKey = "***"
	}
	return fmt.Sprintf("endpoint=%q bucket=%q prefix=%q access_key_id=%q secret_len=%d",
		setting.Endpoint(), setting.R2Bucket, setting.ObjectPrefix(), maskedKey,
		len(strings.TrimSpace(setting.R2SecretAccessKey)))
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
