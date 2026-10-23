package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

// ReconcileDataSourceSyncs is a bounded, idempotent safety net for SIGKILL,
// failed terminal DB writes, queue archival and old orphaned runs. Redis errors
// preserve all state; a timestamp alone never proves absence from Redis.
func ReconcileDataSourceSyncs(ctx context.Context, db *gorm.DB, inspector interfaces.SyncTaskInspector) (int, error) {
	if inspector == nil {
		return 0, nil
	}
	var logs []*types.SyncLog
	if err := db.WithContext(ctx).Where("status=? AND updated_at<?", types.SyncLogStatusRunning,
		time.Now().UTC().Add(-types.SyncExecutionStaleAfter)).Order("updated_at").Limit(200).Find(&logs).Error; err != nil {
		return 0, err
	}
	if len(logs) == 0 {
		return 0, nil
	}
	live, err := inspector.LiveSyncTasks(ctx)
	if err != nil {
		return 0, fmt.Errorf("sync queue reconciliation probe: %w", err)
	}
	repo := repository.NewSyncLogRepository(db).(*repository.SyncLogRepository)
	count := 0
	for _, log := range logs {
		if live[types.SyncTaskKey(log.TenantID, log.DataSourceID, log.ID)] {
			continue
		}
		changed, err := repo.ReconcileOrphan(ctx, log)
		if err != nil {
			return count, err
		}
		if changed {
			count++
		}
	}
	return count, nil
}
