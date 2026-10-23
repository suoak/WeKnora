package router

import (
	"context"
	"encoding/json"
	"os"
	"sync/atomic"
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
	"google.golang.org/protobuf/encoding/protowire"
)

func TestSyncLiveQueueProbeP0(t *testing.T) {
	srv := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	defer func() { require.NoError(t, rc.Close()) }()
	// Shared-connection wrappers own no connection; only rc may close it.
	inspector := &asynqTaskInspector{inspector: asynq.NewInspectorFromRedisClient(rc), redis: rc}
	client := asynq.NewClientFromRedisClient(rc)
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
	archivedSyncDatabaseRecoveryP0(t, "sqlite")
}

func TestArchivedSyncDatabaseRecoveryPostgresP0(t *testing.T) {
	archivedSyncDatabaseRecoveryP0(t, "postgres")
}

func archivedSyncDatabaseRecoveryP0(t *testing.T, dialect string) {
	db := syncdb.Open(t, dialect)
	srv := miniredis.RunT(t)
	addr := srv.Addr()
	if isolated := os.Getenv("FEISHU_P0_REDIS_ADDR"); isolated != "" {
		// Only the workflow's explicitly provided disposable loopback service.
		// Never inherit REDIS_ADDR or any application's credentials/config.
		require.Equal(t, "127.0.0.1:6379", isolated)
		addr = isolated
	}
	rc := redis.NewClient(&redis.Options{Addr: addr})
	defer func() { require.NoError(t, rc.Close()) }()
	// Both Asynq wrappers borrow rc; closing them would reject shared ownership.
	probe := &asynqTaskInspector{inspector: asynq.NewInspectorFromRedisClient(rc), redis: rc}
	client := asynq.NewClientFromRedisClient(rc)
	ds := &types.DataSource{
		ID:       uuid.NewString(),
		TenantID: 1,
		Status:   types.DataSourceStatusActive,
		Config:   types.JSON(`{}`),
	}
	require.NoError(t, db.Create(ds).Error)
	old := time.Now().UTC().Add(-2 * time.Hour)
	log := &types.SyncLog{
		ID:                  uuid.NewString(),
		DataSourceID:        ds.ID,
		TenantID:            1,
		Status:              types.SyncLogStatusRunning,
		ExecutionGeneration: 6,
		ExecutionClaimed:    true,
		StartedAt:           old,
		UpdatedAt:           old,
	}
	require.NoError(t, db.Create(log).Error)
	payload, _ := json.Marshal(types.DataSourceSyncPayload{DataSourceID: ds.ID, TenantID: 1, SyncLogID: log.ID})
	_, err := client.Enqueue(
		asynq.NewTask(types.TypeDataSourceSync, payload),
		asynq.TaskID(log.ID),
		asynq.Queue(types.QueueSync),
		asynq.MaxRetry(5),
	)
	require.NoError(t, err)
	defer func() { _ = probe.inspector.DeleteTask(types.QueueSync, log.ID) }()
	n, err := service.ReconcileDataSourceSyncs(context.Background(), db, probe)
	require.NoError(t, err)
	require.Zero(t, n, "pending delivery is still live, however old its DB row")
	// Reproduce the production trigger: a lost worker leaves an expired queue
	// lease on the final retry. Asynq's real recoverer archives it outside the
	// handler/middleware, so the application DB remains running without a sweep.
	ctx := context.Background()
	prefix := "asynq:{" + types.QueueSync + "}:"
	message, err := rc.HGet(ctx, prefix+"t:"+log.ID, "msg").Bytes()
	require.NoError(t, err)
	message = protowire.AppendTag(message, 6, protowire.VarintType)
	message = protowire.AppendVarint(message, 5)
	require.NoError(t, rc.HSet(ctx, prefix+"t:"+log.ID, "msg", message, "state", "active").Err())
	require.NoError(t, rc.LRem(ctx, prefix+"pending", 0, log.ID).Err())
	require.NoError(t, rc.LPush(ctx, prefix+"active", log.ID).Err())
	require.NoError(
		t,
		rc.ZAdd(ctx, prefix+"lease", redis.Z{Score: float64(time.Now().Add(-2 * time.Minute).Unix()), Member: log.ID}).
			Err(),
	)
	var handlerCalls atomic.Int64
	worker := asynq.NewServer(
		asynq.RedisClientOpt{Addr: addr},
		asynq.Config{Concurrency: 1, Queues: map[string]int{types.QueueSync: 1}, ShutdownTimeout: time.Second},
	)
	require.NoError(
		t,
		worker.Start(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { handlerCalls.Add(1); return nil })),
	)
	defer worker.Shutdown()
	require.Eventually(t, func() bool {
		info, err := probe.inspector.GetTaskInfo(types.QueueSync, log.ID)
		return err == nil && info.State == asynq.TaskStateArchived && info.LastErr == asynq.ErrLeaseExpired.Error()
	}, 5*time.Second, 20*time.Millisecond)
	require.Zero(t, handlerCalls.Load(), "recoverer must bypass the application handler")
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
