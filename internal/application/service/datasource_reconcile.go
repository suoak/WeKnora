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
	repo := repository.NewSyncLogRepository(db).(*repository.SyncLogRepository)
	cutoff := time.Now().UTC().Add(-types.SyncExecutionStaleAfter)
	var afterTime time.Time
	afterID := ""
	var live map[string]bool
	count := 0
	for {
		logs, err := repo.StaleExecutionsPage(ctx, cutoff, afterTime, afterID)
		if err != nil {
			return count, err
		}
		if len(logs) == 0 {
			return count, nil
		}
		if live == nil {
			var err error
			live, err = inspector.LiveSyncTasks(ctx)
			if err != nil {
				return count, fmt.Errorf("sync queue reconciliation probe: %w", err)
			}
			if live == nil {
				return count, fmt.Errorf("sync queue probe returned no snapshot")
			}
		}
		// Keyset pagination prevents old, legitimately queued rows at the front
		// of the result from starving later orphaned rows on every sweep.
		last := logs[len(logs)-1]
		afterTime, afterID = last.UpdatedAt, last.ID
		if afterTime.IsZero() {
			afterTime = last.StartedAt
		}
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
		if len(logs) < 200 {
			return count, nil
		}
	}
}
