package model

import (
	"context"

	"gorm.io/gorm"
)

// GetTokenForUsage reads current quota/status, bypassing the relay token cache.
func GetTokenForUsage(ctx context.Context, key string) (*Token, error) {
	var token Token
	err := DB.WithContext(ctx).Where(map[string]any{"key": key}).First(&token).Error
	return &token, err
}

// VisitTokenUsageLogs bounds memory while reading the dedicated log database.
// Filtering both IDs prevents exposing another user's usage after a key transfer.
func VisitTokenUsageLogs(ctx context.Context, userID, tokenID int, visit func(Log)) error {
	var batch []Log
	return LOG_DB.WithContext(ctx).Model(&Log{}).
		Select("id", "created_at", "model_name", "quota", "prompt_tokens", "completion_tokens", "use_time", "other").
		Where("user_id = ? AND token_id = ? AND type = ?", userID, tokenID, LogTypeConsume).
		FindInBatches(&batch, 1000, func(_ *gorm.DB, _ int) error {
			for _, entry := range batch {
				visit(entry)
			}
			return nil
		}).Error
}
