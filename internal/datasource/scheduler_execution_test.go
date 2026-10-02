package datasource

import (
	"context"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSyncSchedulerSQLite(t *testing.T)   { syncSchedulerSuite(t, "sqlite") }
func TestSyncSchedulerPostgres(t *testing.T) { syncSchedulerSuite(t, "postgres") }

func syncSchedulerSuite(t *testing.T, dialect string) {
	db := syncdb.Open(t, dialect)
	dsRepo := repository.NewDataSourceRepository(db)
	logs := repository.NewSyncLogRepository(db)
	for _, mixed := range []bool{false, true} {
		ds := &types.DataSource{
			ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Name: "sync",
			Type: types.ConnectorTypeFeishu, Status: types.DataSourceStatusActive,
		}
		require.NoError(t, dsRepo.Create(context.Background(), ds))
		queue := &fakeTaskEnqueuer{}
		scheduler := NewScheduler(dsRepo, logs, queue)
		var wg sync.WaitGroup
		errors := make(chan error, 20)
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if mixed && i%2 == 0 {
					// Exact shared admission primitive called by ManualSync.
					_, err := logs.CreateIfNoRunning(context.Background(), &types.SyncLog{
						DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning,
					})
					errors <- err
				} else {
					scheduler.triggerSync(ds.ID, 1)
				}
			}(i)
		}
		wg.Wait()
		close(errors)
		for err := range errors {
			require.NoError(t, err)
		}
		stored, err := logs.FindByDataSource(context.Background(), ds.ID, 100, 0)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		if !mixed {
			require.EqualValues(t, 1, queue.count.Load())
		}
	}
}
