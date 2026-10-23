package container

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

func startDataSourceSyncReconciliation(db *gorm.DB, tasks interfaces.TaskInspector, cleaner interfaces.ResourceCleaner) {
	probe, ok := tasks.(interfaces.SyncTaskInspector)
	if !ok {
		return
	} // Lite has no Redis; existing execution/restart fencing applies.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			attempt, stop := context.WithTimeout(ctx, 45*time.Second)
			n, err := service.ReconcileDataSourceSyncs(attempt, db, probe)
			stop()
			if err != nil && ctx.Err() == nil {
				logger.Warnf(ctx, "[SyncReconciliation] %v", err)
			}
			if n > 0 {
				logger.Infof(ctx, "[SyncReconciliation] finalized %d orphaned runs", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	cleaner.RegisterWithName("DataSourceSyncReconciliation", func() error { cancel(); <-done; return nil })
}
