package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// ReconcileOrphan fences the exact observed generation AND heartbeat. A worker
// reclaim/heartbeat or a previous reconciler makes this a no-op. Never write a
// checkpoint, content, permission state or last-success timestamp here.
func (r *SyncLogRepository) ReconcileOrphan(ctx context.Context, snapshot *types.SyncLog) (bool, error) {
	return r.finalizeObservedExecution(ctx, snapshot, "Sync task no longer live in queue; checkpoint retained")
}

func (r *SyncLogRepository) FinalizeReleasedExecution(ctx context.Context, snapshot *types.SyncLog) (bool, error) {
	if snapshot.ExecutionClaimed {
		return false, nil
	}
	return r.finalizeObservedExecution(ctx, snapshot, "Sync retries exhausted; checkpoint retained")
}

func (r *SyncLogRepository) finalizeObservedExecution(ctx context.Context, snapshot *types.SyncLog, reason string) (bool, error) {
	changed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ds, err := lockSyncDataSource(tx, snapshot.DataSourceID, snapshot.TenantID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		result := tx.Model(&types.SyncLog{}).
			Where("id=? AND data_source_id=? AND tenant_id=? AND status=? AND execution_generation=? AND execution_claimed=? AND updated_at=?",
				snapshot.ID, snapshot.DataSourceID, snapshot.TenantID, types.SyncLogStatusRunning,
				snapshot.ExecutionGeneration, snapshot.ExecutionClaimed, snapshot.UpdatedAt).
			Updates(map[string]any{"status": types.SyncLogStatusFailed, "execution_claimed": false,
				"execution_generation": gorm.Expr("execution_generation + 1"), "finished_at": now, "updated_at": now,
				"error_message": reason})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		changed = true
		var newer int64
		if err := tx.Model(&types.SyncLog{}).Where("data_source_id=? AND id<>? AND (started_at>=? OR status=?)",
			snapshot.DataSourceID, snapshot.ID, snapshot.StartedAt, types.SyncLogStatusRunning).Count(&newer).Error; err != nil {
			return err
		}
		if newer != 0 {
			return nil
		}
		status := ds.Status
		if status == types.DataSourceStatusActive || status == types.DataSourceStatusError {
			status = types.DataSourceStatusError
		}
		return tx.Model(&types.DataSource{}).Where("id=?", ds.ID).Updates(map[string]any{
			"status": status, "error_message": reason, "updated_at": now,
		}).Error
	})
	return changed && err == nil, err
}
