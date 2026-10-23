package router

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSyncLiveQueueProbeP0(t *testing.T) {
	srv := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	defer rc.Close()
	inspector := &asynqTaskInspector{inspector: asynq.NewInspectorFromRedisClient(rc), redis: rc}
	defer inspector.inspector.Close()
	client := asynq.NewClientFromRedisClient(rc)
	defer client.Close()
	ctx := context.Background()
	live, err := inspector.LiveSyncTasks(ctx)
	require.NoError(t, err)
	require.Empty(t, live)
	for _, state := range []string{"pending", "scheduled", "archived"} {
		p := types.DataSourceSyncPayload{DataSourceID: "source", TenantID: 1, SyncLogID: state}
		payload, _ := json.Marshal(p)
		opts := []asynq.Option{asynq.Queue(types.QueueSync), asynq.TaskID(state)}
		if state == "scheduled" {
			opts = append(opts, asynq.ProcessIn(time.Hour))
		}
		_, err := client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, payload), opts...)
		require.NoError(t, err)
		if state == "archived" {
			require.NoError(t, inspector.inspector.ArchiveTask(types.QueueSync, state))
		}
	}
	live, err = inspector.LiveSyncTasks(ctx)
	require.NoError(t, err)
	require.True(t, live[types.SyncTaskKey(1, "source", "pending")])
	require.True(t, live[types.SyncTaskKey(1, "source", "scheduled")])
	require.False(t, live[types.SyncTaskKey(1, "source", "archived")])
	srv.Close()
	_, err = inspector.LiveSyncTasks(ctx)
	require.Error(t, err)
}

func TestArchivedSyncDatabaseRecoveryP0(t *testing.T) {
	db := syncdb.Open(t, "sqlite")
	srv := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	defer rc.Close()
	probe := &asynqTaskInspector{inspector: asynq.NewInspectorFromRedisClient(rc), redis: rc}
	defer probe.inspector.Close()
	client := asynq.NewClientFromRedisClient(rc)
	defer client.Close()
	ds := &types.DataSource{ID: uuid.NewString(), TenantID: 1, Status: types.DataSourceStatusActive, Config: types.JSON(`{}`)}
	require.NoError(t, db.Create(ds).Error)
	old := time.Now().UTC().Add(-2 * time.Hour)
	log := &types.SyncLog{ID: uuid.NewString(), DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning, ExecutionGeneration: 6, ExecutionClaimed: true, StartedAt: old, UpdatedAt: old}
	require.NoError(t, db.Create(log).Error)
	payload, _ := json.Marshal(types.DataSourceSyncPayload{DataSourceID: ds.ID, TenantID: 1, SyncLogID: log.ID})
	_, err := client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, payload), asynq.TaskID(log.ID), asynq.Queue(types.QueueSync))
	require.NoError(t, err)
	n, err := service.ReconcileDataSourceSyncs(context.Background(), db, probe)
	require.NoError(t, err)
	require.Zero(t, n, "pending delivery is still live, however old its DB row")
	require.NoError(t, probe.inspector.ArchiveTask(types.QueueSync, log.ID))
	n, err = service.ReconcileDataSourceSyncs(context.Background(), db, probe)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	repo := repository.NewSyncLogRepository(db)
	got, err := repo.FindByID(context.Background(), log.ID)
	require.NoError(t, err)
	require.Equal(t, types.SyncLogStatusFailed, got.Status)
	require.EqualValues(t, 7, got.ExecutionGeneration)
	n, err = service.ReconcileDataSourceSyncs(context.Background(), db, probe)
	require.NoError(t, err)
	require.Zero(t, n)
}
