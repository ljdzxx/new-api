package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type invoiceR2ProbeClientMock struct {
	putInput    *s3.PutObjectInput
	deleteInput *s3.DeleteObjectInput
	putErr      error
	deleteErr   error
}

func (m *invoiceR2ProbeClientMock) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	m.putInput = input
	return &s3.PutObjectOutput{}, m.putErr
}

func (m *invoiceR2ProbeClientMock) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	m.deleteInput = input
	return &s3.DeleteObjectOutput{}, m.deleteErr
}

func TestInvoiceR2ConnectionProbeWritesAndDeletesObject(t *testing.T) {
	client := &invoiceR2ProbeClientMock{}
	setting := &operation_setting.InvoiceSetting{
		R2Bucket:       "files",
		R2ObjectPrefix: "invoices/",
	}

	err := testInvoiceR2ConnectionWithClient(context.Background(), client, setting)
	if err != nil {
		t.Fatalf("testInvoiceR2ConnectionWithClient() error = %v", err)
	}
	if client.putInput == nil || client.deleteInput == nil {
		t.Fatal("expected probe object to be written and deleted")
	}
	if got := aws.ToString(client.putInput.Bucket); got != "files" {
		t.Fatalf("PutObject bucket = %q, want %q", got, "files")
	}
	putKey := aws.ToString(client.putInput.Key)
	if !strings.HasPrefix(putKey, "invoices/.connection-probe-") {
		t.Fatalf("PutObject key = %q, want unique probe key under invoice prefix", putKey)
	}
	if got := aws.ToString(client.deleteInput.Key); got != putKey {
		t.Fatalf("DeleteObject key = %q, want %q", got, putKey)
	}
}

func TestInvoiceR2ConnectionProbeReportsWriteFailure(t *testing.T) {
	client := &invoiceR2ProbeClientMock{putErr: errors.New("forbidden")}
	setting := &operation_setting.InvoiceSetting{R2Bucket: "files"}

	err := testInvoiceR2ConnectionWithClient(context.Background(), client, setting)
	if err == nil || !strings.Contains(err.Error(), "无法向存储桶 \"files\" 写入测试对象") {
		t.Fatalf("error = %v, want actionable write failure", err)
	}
	if client.deleteInput != nil {
		t.Fatal("DeleteObject must not be called after PutObject failure")
	}
}

func TestInvoiceR2ConnectionProbeReportsDeleteFailure(t *testing.T) {
	client := &invoiceR2ProbeClientMock{deleteErr: errors.New("forbidden")}
	setting := &operation_setting.InvoiceSetting{R2Bucket: "files"}

	err := testInvoiceR2ConnectionWithClient(context.Background(), client, setting)
	if err == nil || !strings.Contains(err.Error(), "测试对象已写入，但无法从存储桶 \"files\" 删除") {
		t.Fatalf("error = %v, want actionable delete failure", err)
	}
}
