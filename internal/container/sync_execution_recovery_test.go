package container

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSyncRecoverySQLite(t *testing.T)   { syncRecoverySuite(t, "sqlite") }
func TestSyncRecoveryPostgres(t *testing.T) { syncRecoverySuite(t, "postgres") }

func syncRecoverySuite(t *testing.T, dialect string) {
	t.Setenv("REDIS_ADDR", "test-service-not-contacted")
	db := syncdb.Open(t, dialect)
	ctx := context.Background()
	dsRepo := repository.NewDataSourceRepository(db)
	logs := repository.NewSyncLogRepository(db)
	executions := logs.(interface {
		ClaimExecution(context.Context, string, string, uint64) (types.SyncExecution, bool, error)
		WriteExecution(context.Context, types.SyncExecution, *types.DataSource, *types.SyncLog, bool) (bool, error)
	})
	ds := &types.DataSource{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb", Name: "recovery", Type: types.ConnectorTypeFeishu, Status: types.DataSourceStatusActive}
	require.NoError(t, dsRepo.Create(ctx, ds))
	log := &types.SyncLog{DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning}
	ok, err := logs.CreateIfNoRunning(ctx, log)
	require.NoError(t, err)
	require.True(t, ok)
	old, ok, err := executions.ClaimExecution(ctx, ds.ID, log.ID, 1)
	require.NoError(t, err)
	require.True(t, ok)
	// A live two-hour job must not be recovered just because its start is old.
	require.NoError(t, db.Model(&types.SyncLog{}).Where("id = ?", log.ID).UpdateColumn("started_at", time.Now().Add(-2*time.Hour)).Error)
	resetPendingTasks(db)
	live, err := logs.FindByID(ctx, log.ID)
	require.NoError(t, err)
	require.True(t, live.ExecutionClaimed)
	// Simulate crashed worker heartbeat, then run the REAL startup hook.
	require.NoError(t, db.Model(&types.SyncLog{}).Where("id = ?", log.ID).UpdateColumn("updated_at", time.Now().Add(-2*time.Hour)).Error)
	resetPendingTasks(db)
	recovered, err := logs.FindByID(ctx, log.ID)
	require.NoError(t, err)
	require.Equal(t, types.SyncLogStatusFailed, recovered.Status)
	require.False(t, recovered.ExecutionClaimed)
	require.Greater(t, recovered.ExecutionGeneration, old.Generation)
	next := &types.SyncLog{DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning}
	ok, err = logs.CreateIfNoRunning(ctx, next)
	require.NoError(t, err)
	require.True(t, ok)
	current, ok, err := executions.ClaimExecution(ctx, ds.ID, next.ID, 1)
	require.NoError(t, err)
	require.True(t, ok)
	ds.LastSyncCursor = types.JSON(`{"current":true}`)
	next.Status = types.SyncLogStatusSuccess
	ok, err = executions.WriteExecution(ctx, current, ds, next, true)
	require.NoError(t, err)
	require.True(t, ok)
	for _, release := range []bool{false, true} {
		log.Status = types.SyncLogStatusRunning
		if release {
			log.Status = types.SyncLogStatusSuccess
		}
		ds.LastSyncCursor = types.JSON(`{"stale":true}`)
		ok, err = executions.WriteExecution(ctx, old, ds, log, release)
		require.NoError(t, err)
		require.False(t, ok)
	}
	stored, err := dsRepo.FindByID(ctx, ds.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{"current":true}`, stored.LastSyncCursor.ToString())
}
