package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/clause"
)

func TestSyncHeartbeatPublicationContentionPostgres(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	repo := &SyncLogRepository{db: db}
	ctx := context.Background()
	ds := &types.DataSource{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "kb",
		Name: "heartbeat fixture", Type: types.ConnectorTypeFeishu, Status: types.DataSourceStatusActive}
	require.NoError(t, db.Create(ds).Error)
	log := &types.SyncLog{DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning}
	ok, err := repo.CreateIfNoRunning(ctx, log)
	require.NoError(t, err)
	require.True(t, ok)
	execution, ok, err := repo.ClaimExecution(ctx, ds.ID, log.ID, 1)
	require.NoError(t, err)
	require.True(t, ok)
	var initial types.SyncLog
	require.NoError(t, db.First(&initial, "id=?", log.ID).Error)

	// Use the same lock order and log ownership predicate as the publication
	// fence, without indexing any business content or sleeping for a minute.
	tx := db.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { _ = tx.Rollback().Error })
	_, err = lockSyncDataSource(tx, ds.ID, ds.TenantID)
	require.NoError(t, err)
	var locked types.SyncLog
	require.NoError(t, ownedSyncLog(tx, execution).
		Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked).Error)

	// Reproduce the previous query: its independent heartbeat waits behind its
	// own worker's publication transaction and exhausts its cancellation budget.
	oldCtx, cancelOld := context.WithTimeout(ctx, 100*time.Millisecond)
	old := ownedSyncLog(db.WithContext(oldCtx), execution).Update("updated_at", time.Now().UTC())
	require.Error(t, old.Error)
	require.ErrorIs(t, oldCtx.Err(), context.DeadlineExceeded)
	cancelOld()

	beatCtx, cancelBeat := context.WithTimeout(ctx, time.Second)
	defer cancelBeat()
	ok, err = repo.HeartbeatExecution(beatCtx, execution)
	require.NoError(t, err)
	require.True(t, ok)
	var during types.SyncLog
	require.NoError(t, db.First(&during, "id=?", log.ID).Error)
	require.Equal(t, initial.UpdatedAt, during.UpdatedAt, "contention does not fabricate a renewal")

	wrong := execution
	wrong.Generation++
	ok, err = repo.HeartbeatExecution(beatCtx, wrong)
	require.NoError(t, err)
	require.False(t, ok, "a locked row cannot authorize another generation")
	require.NoError(t, tx.Commit().Error)
	ok, err = repo.HeartbeatExecution(beatCtx, execution)
	require.NoError(t, err)
	require.True(t, ok)
	var renewed types.SyncLog
	require.NoError(t, db.First(&renewed, "id=?", log.ID).Error)
	require.True(t, renewed.UpdatedAt.After(initial.UpdatedAt))

	log.Status = types.SyncLogStatusSuccess
	ok, err = repo.WriteExecution(ctx, execution, ds, log, true)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.HeartbeatExecution(beatCtx, execution)
	require.NoError(t, err)
	require.False(t, ok, "a released generation cannot be revived")
}
