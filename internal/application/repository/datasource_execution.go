package repository

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ interfaces.SyncExecutionRepository = (*SyncLogRepository)(nil)

func staleSyncHeartbeatSQL(db *gorm.DB) string {
	if db.Dialector.Name() == "sqlite" {
		// SQLite stores timestamps as text. Compare instants, not mixed UTC/
		// local-time representations from legacy rows or driver defaults.
		return "julianday(COALESCE(updated_at, started_at)) < julianday(?)"
	}
	return "COALESCE(updated_at, started_at) < ?"
}

// lockSyncDataSource is always the first lock, before any sync-log lock.
// SQLite must acquire its writer lock BEFORE reading, not upgrade a snapshot.
func lockSyncDataSource(tx *gorm.DB, dsID string, tenantID uint64) (*types.DataSource, error) {
	q := tx.Model(&types.DataSource{}).Where("id = ? AND tenant_id = ?", dsID, tenantID)
	if tx.Dialector.Name() == "sqlite" {
		locked := q.UpdateColumn("id", gorm.Expr("id"))
		if locked.Error != nil {
			return nil, locked.Error
		}
		if locked.RowsAffected != 1 {
			return nil, gorm.ErrRecordNotFound
		}
	} else {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var ds types.DataSource
	if err := q.First(&ds).Error; err != nil {
		return nil, err
	}
	return &ds, nil
}

func (r *SyncLogRepository) ClaimExecution(ctx context.Context, dsID, logID string, tenantID uint64) (types.SyncExecution, bool, error) {
	owner := types.SyncExecution{DataSourceID: dsID, SyncLogID: logID, TenantID: tenantID}
	if dsID == "" || logID == "" || tenantID == 0 {
		return owner, false, errors.New("invalid sync execution identity")
	}
	claimed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockSyncDataSource(tx, dsID, tenantID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		result := tx.Model(&types.SyncLog{}).
			Where("id = ? AND data_source_id = ? AND tenant_id = ? AND status = ?",
				logID, dsID, tenantID, types.SyncLogStatusRunning).
			Where("execution_claimed = ? OR "+staleSyncHeartbeatSQL(tx), false, time.Now().UTC().Add(-types.SyncExecutionStaleAfter)).
			Updates(map[string]interface{}{"execution_generation": gorm.Expr("execution_generation + 1"),
				"execution_claimed": true, "finished_at": nil, "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		var log types.SyncLog
		if err := tx.Select("execution_generation").First(&log, "id = ?", logID).Error; err != nil {
			return err
		}
		owner.Generation = log.ExecutionGeneration
		claimed = true
		return nil
	})
	return owner, claimed && err == nil, err
}

func ownedSyncLog(tx *gorm.DB, e types.SyncExecution) *gorm.DB {
	return tx.Model(&types.SyncLog{}).
		Where("id = ? AND data_source_id = ? AND tenant_id = ? AND status = ? AND execution_generation = ? AND execution_claimed = ?",
			e.SyncLogID, e.DataSourceID, e.TenantID, types.SyncLogStatusRunning, e.Generation, true)
}

// WriteExecution atomically fences BOTH the log and datasource metadata. A
// failed CAS is a no-op. Database errors roll back both writes. No callback or
// external API runs here. A retry release keeps status=running but clears claim;
// its next claim increments generation. Terminal runs cannot be reclaimed.
func (r *SyncLogRepository) WriteExecution(ctx context.Context, e types.SyncExecution, ds *types.DataSource, log *types.SyncLog, release bool) (bool, error) {
	if ds == nil || log == nil || e.Generation <= 0 || ds.ID != e.DataSourceID || log.ID != e.SyncLogID ||
		ds.TenantID != e.TenantID || log.TenantID != e.TenantID || log.DataSourceID != e.DataSourceID {
		return false, errors.New("invalid sync execution write")
	}
	if !release && log.Status != types.SyncLogStatusRunning {
		return false, errors.New("checkpoint must remain running")
	}
	written := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := lockSyncDataSource(tx, e.DataSourceID, e.TenantID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		updated := ownedSyncLog(tx, e).Updates(map[string]interface{}{
			"status": log.Status, "finished_at": log.FinishedAt, "items_total": log.ItemsTotal,
			"items_created": log.ItemsCreated, "items_updated": log.ItemsUpdated,
			"items_deleted": log.ItemsDeleted, "items_skipped": log.ItemsSkipped, "items_failed": log.ItemsFailed,
			"error_message": log.ErrorMessage, "result": log.Result, "execution_claimed": !release, "updated_at": time.Now().UTC(),
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			return nil
		}
		status := ds.Status
		// A user's pause during execution must not be overwritten by an old snapshot.
		if current.Status == types.DataSourceStatusPaused {
			status = current.Status
		}
		if err := tx.Model(&types.DataSource{}).Where("id = ?", e.DataSourceID).Updates(map[string]interface{}{
			"status": status, "last_sync_at": ds.LastSyncAt, "last_sync_cursor": ds.LastSyncCursor,
			"last_sync_result": ds.LastSyncResult, "error_message": ds.ErrorMessage, "updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		written = true
		return nil
	})
	return written && err == nil, err
}

// Heartbeat only locks the sync-log row, never subsequently the datasource.
// It cannot revive a released, recovered, deleted or superseded attempt.
func (r *SyncLogRepository) HeartbeatExecution(ctx context.Context, e types.SyncExecution) (bool, error) {
	if e.Generation <= 0 {
		return false, errors.New("invalid sync execution generation")
	}
	result := ownedSyncLog(r.db.WithContext(ctx), e).Update("updated_at", time.Now().UTC())
	return result.RowsAffected == 1 && result.Error == nil, result.Error
}
