package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestUsageCompatStatsBatchesAndTimezone(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	oldDB, oldLocation, oldQuota := model.LOG_DB, time.Local, common.QuotaPerUnit
	model.LOG_DB, time.Local, common.QuotaPerUnit = db, time.UTC, 500000
	t.Cleanup(func() {
		model.LOG_DB, time.Local, common.QuotaPerUnit = oldDB, oldLocation, oldQuota
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	if err := db.AutoMigrate(&model.Log{}); err != nil {
		t.Fatal(err)
	}
	// New York's DST transition day has 23 hours. The reference filters using
	// local midnight, then groups dates using the server/database timezone.
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 8, 23, 0, 0, 0, location)
	start := time.Date(2026, 3, 8, 0, 0, 0, 0, location)
	end := start.AddDate(0, 0, 1)
	entries := make([]model.Log, 1001)
	for i := range entries {
		entries[i] = model.Log{UserId: 1, TokenId: 1, Type: model.LogTypeConsume, CreatedAt: now.Add(-time.Minute).Unix(), ModelName: "bulk", PromptTokens: 10, CompletionTokens: 5, Quota: 500000, UseTime: 2}
	}
	entries = append(entries,
		model.Log{UserId: 1, TokenId: 1, Type: model.LogTypeConsume, CreatedAt: start.Add(-time.Second).Unix(), ModelName: "before", PromptTokens: 1},
		model.Log{UserId: 1, TokenId: 1, Type: model.LogTypeConsume, CreatedAt: start.Unix(), ModelName: "start", PromptTokens: 2},
		model.Log{UserId: 1, TokenId: 1, Type: model.LogTypeConsume, CreatedAt: end.Unix(), ModelName: "end", PromptTokens: 3},
	)
	if err := db.CreateInBatches(entries, 100).Error; err != nil {
		t.Fatal(err)
	}
	summary, daily, models, err := BuildUsageCompatStats(context.Background(), 1, 1, now, start, end, 1, location)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total.Requests != 1004 || summary.Total.ActualCost != 1001 {
		t.Fatalf("lost batch: %#v", summary.Total)
	}
	if len(daily) != 2 || daily[0].Date != "2026-03-08" || daily[0].Requests != 1 || daily[1].Date != "2026-03-09" || daily[1].Requests != 1001 {
		t.Fatalf("wrong timezone/boundary grouping: %#v", daily)
	}
	if len(models) != 2 || models[0].Model != "bulk" || models[0].Requests != 1001 || models[1].Model != "start" {
		t.Fatalf("wrong half-open range/sort: %#v", models)
	}
	if summary.RPM != 200 || summary.TPM != 3003 {
		t.Fatalf("wrong 5-minute rates: %#v", summary)
	}
}
