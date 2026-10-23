package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// StaleExecutionsPage preserves the legacy NULL-heartbeat fallback and compares
// instants on SQLite, whose timestamp text may carry different UTC offsets.
func (r *SyncLogRepository) StaleExecutionsPage(
	ctx context.Context,
	cutoff, after time.Time,
	afterID string,
) ([]*types.SyncLog, error) {
	clock, argument := "COALESCE(updated_at, started_at)", "?"
	if r.db.Name() == "sqlite" {
		clock, argument = "julianday("+clock+")", "julianday(?)"
	}
	q := r.db.WithContext(ctx).Where("status=? AND "+clock+"<"+argument, types.SyncLogStatusRunning, cutoff)
	if afterID != "" {
		q = q.Where("("+clock+">"+argument+" OR ("+clock+"="+argument+" AND id>?))", after, after, afterID)
	}
	var logs []*types.SyncLog
	err := q.Order(clock + ", id").Limit(200).Find(&logs).Error
	return logs, err
}

// ReconcileOrphan fences the exact observed generation AND heartbeat. A worker
// reclaim/heartbeat or a previous reconciler makes this a no-op. Never write a
// checkpoint, content, permission state or last-success timestamp here.
func (r *SyncLogRepository) ReconcileOrphan(ctx context.Context, snapshot *types.SyncLog) (bool, error) {
	return r.finalizeObservedExecution(ctx, snapshot, "Sync task no longer live in queue; checkpoint retained")
}

// FinalizeReleasedExecution terminalizes an exhausted delivery only if the
// observed released execution still owns the exact generation and heartbeat.
func (r *SyncLogRepository) FinalizeReleasedExecution(ctx context.Context, snapshot *types.SyncLog) (bool, error) {
	if snapshot.ExecutionClaimed {
		return false, nil
	}
	return r.finalizeObservedExecution(ctx, snapshot, "Sync retries exhausted; checkpoint retained")
}

func (r *SyncLogRepository) finalizeObservedExecution(
	ctx context.Context,
	snapshot *types.SyncLog,
	reason string,
) (bool, error) {
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
		observed := tx.Model(&types.SyncLog{}).
			Where("id=? AND data_source_id=? AND tenant_id=? AND status=? "+
				"AND execution_generation=? AND execution_claimed=?",
				snapshot.ID, snapshot.DataSourceID, snapshot.TenantID, types.SyncLogStatusRunning,
				snapshot.ExecutionGeneration, snapshot.ExecutionClaimed)
		if snapshot.UpdatedAt.IsZero() {
			observed = observed.Where("updated_at IS NULL AND started_at=?", snapshot.StartedAt)
		} else {
			observed = observed.Where("updated_at=?", snapshot.UpdatedAt)
		}
		result := observed.
			Updates(map[string]any{
				"status": types.SyncLogStatusFailed, "execution_claimed": false,
				"execution_generation": gorm.Expr("execution_generation + 1"), "finished_at": now, "updated_at": now,
				"error_message": reason,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		changed = true
		var newer int64
		if err := tx.Model(&types.SyncLog{}).Where("data_source_id=? AND id<>? AND (started_at>=? OR status=?)",
			snapshot.DataSourceID, snapshot.ID, snapshot.StartedAt, types.SyncLogStatusRunning,
		).Count(&newer).Error; err != nil {
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
