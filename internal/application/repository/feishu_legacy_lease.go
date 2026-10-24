package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// Renew preserves a live candidate while a fenced execution makes progress.
// An expired lease or lost source/claim/nonce/generation cannot be revived.
func (s *FeishuLegacyStore) Renew(ctx context.Context, ds *types.DataSource, owner *types.FeishuLegacyAttempt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ds == nil || owner == nil {
		return legacyFence("attempt_owner_unconfirmed")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, attempt, err := s.owned(ctx, tx, ds, owner)
		if err != nil {
			return err
		}
		return tx.Model(attempt).UpdateColumn("lease_until",
			gorm.Expr("GREATEST(lease_until, clock_timestamp() + interval '20 minutes')")).Error
	})
}
