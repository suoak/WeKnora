package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFailedReconciledSourceRestoresPersistedSchedule(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		for _, scenario := range []string{"restored", "still_held", "user_paused"} {
			t.Run(dialect+"/"+scenario, func(t *testing.T) {
				db := syncdb.Open(t, dialect)
				type inventoryRow struct {
					ID              string `gorm:"primaryKey"`
					TenantID        uint64
					KnowledgeBaseID string
					Channel         string
					Metadata        types.JSON `gorm:"type:jsonb"`
					DeletedAt       gorm.DeletedAt
				}
				require.NoError(t, db.Table("knowledges").AutoMigrate(&inventoryRow{}))
				require.NoError(t, db.AutoMigrate(&types.FeishuLegacyResource{}, &types.FeishuLegacyAttempt{}))
				repo := repository.NewDataSourceRepository(db)
				logs := repository.NewSyncLogRepository(db)
				ds := &types.DataSource{
					ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: uuid.NewString(),
					Type: "feishu", Status: types.DataSourceStatusError, LegacyAccessState: "hold",
					SyncSchedule: "0 */15 * * * *",
				}
				ctx := context.Background()
				require.NoError(t, repo.Create(ctx, ds))
				log := &types.SyncLog{DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning}
				created, err := logs.CreateIfNoRunning(ctx, log)
				require.NoError(t, err)
				require.True(t, created)
				execution, claimed, err := logs.(interfaces.SyncExecutionRepository).
					ClaimExecution(ctx, ds.ID, log.ID, 1)
				require.NoError(t, err)
				require.True(t, claimed)
				ctx = types.WithLegacyReceiptSet(types.WithSyncExecution(ctx, execution))
				if scenario != "still_held" {
					ctx = types.WithLegacyFullReconciliation(types.WithLegacyReconciliationRead(ctx))
					ctx = types.WithReconciliationCommitGuard(ctx, func(*gorm.DB) error { return nil })
				}
				if scenario == "user_paused" {
					require.NoError(t, db.Model(ds).Update("status", types.DataSourceStatusPaused).Error)
				}
				schedule := datasource.NewScheduler(repo, logs, nil)
				defer schedule.Stop()
				svc := &DataSourceService{dsRepo: repo, syncLogRepo: logs, scheduler: schedule}
				result := &types.SyncResult{Total: 1, Failed: 1}
				require.NoError(t, svc.updateSyncRunResult(ctx, ds, log, result, nil,
					types.SyncLogStatusFailed, "1 document failed", false))
				current, err := repo.FindByID(ctx, ds.ID)
				require.NoError(t, err)
				storedRun, err := logs.FindByID(ctx, log.ID)
				require.NoError(t, err)
				require.Equal(t, types.SyncLogStatusFailed, storedRun.Status)
				require.Equal(t, 1, storedRun.ItemsFailed)
				if scenario == "restored" {
					require.Equal(t, "active", current.LegacyAccessState)
					require.Equal(t, types.DataSourceStatusError, current.Status)
					require.Equal(t, 1, schedule.EntryCount(), "verified recovery must restore automatic retries")
				} else {
					require.Zero(t, schedule.EntryCount(), "hold and user pause remain authoritative")
				}
			})
		}
	}
}
