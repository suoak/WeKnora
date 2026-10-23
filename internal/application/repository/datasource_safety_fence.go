package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// FenceLegacySyncExecutions closes legacy runs without deleting queued tasks or
// changing source status, permissions, checkpoints, schedules or published data.
// The datasource-first lock order serializes against claim and atomic publication.
// A repeated stop is harmless; every previously owned generation is invalidated.
func FenceLegacySyncExecutions(ctx context.Context, db *gorm.DB) (int64, error) {
	var sources []types.DataSource
	if err := db.WithContext(ctx).Select("id", "tenant_id").
		Where("type IN ? AND deleted_at IS NULL", []string{"feishu", "feishu_drive", "lark", "lark_drive"}).
		Where("id IN (?)", db.Model(&types.SyncLog{}).Select("data_source_id").
			Where("status=?", types.SyncLogStatusRunning)).
		Order("id").Find(&sources).Error; err != nil {
		return 0, err
	}
	var total int64
	for _, source := range sources {
		var changed int64
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if _, err := lockSyncDataSource(tx, source.ID, source.TenantID); err != nil {
				return err
			}
			now := time.Now().UTC()
			result := tx.Model(&types.SyncLog{}).
				Where("data_source_id=? AND tenant_id=? AND status=?",
					source.ID, source.TenantID, types.SyncLogStatusRunning).
				Updates(map[string]any{
					"status": types.SyncLogStatusCanceled, "finished_at": now, "updated_at": now,
					"execution_generation": gorm.Expr("execution_generation + 1"),
					"execution_claimed":    false, "error_message": "Feishu safety stop; checkpoint retained",
				})
			changed = result.RowsAffected
			return result.Error
		})
		if err != nil {
			return total, err
		}
		total += changed
	}
	return total, nil
}
