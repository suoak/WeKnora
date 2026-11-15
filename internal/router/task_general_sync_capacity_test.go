package router

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/testutil/syncdb"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGeneralSyncHistoricalRelayPostgres(t *testing.T) {
	db := syncdb.Open(t, "postgres")
	r := miniredis.RunT(t)
	opt := asynq.RedisClientOpt{Addr: r.Addr()}
	client := asynq.NewClient(opt)
	defer func() { require.NoError(t, client.Close()) }()
	raw := redis.NewClient(&redis.Options{Addr: r.Addr()})
	defer func() { require.NoError(t, raw.Close()) }()
	inspector := &asynqTaskInspector{redis: raw, inspector: asynq.NewInspectorFromRedisClient(raw)}
	source := &types.DataSource{
		ID: "source", TenantID: 1, KnowledgeBaseID: "kb", Type: types.ConnectorTypeGitLab,
		Status: types.DataSourceStatusActive, Config: types.JSON(`{}`),
	}
	require.NoError(t, db.Create(source).Error)
	log := &types.SyncLog{ID: "run", TenantID: 1, DataSourceID: "source", Status: types.SyncLogStatusRunning}
	require.NoError(t, db.Create(log).Error)
	body, err := json.Marshal(types.DataSourceSyncPayload{
		TenantID: 1, DataSourceID: "source", SyncLogID: "run", Trigger: "original-schedule", ForceFull: true,
	})
	require.NoError(t, err)
	headers := map[string]string{"traceparent": "preserve-original-trace"}
	_, err = client.Enqueue(asynq.NewTaskWithHeaders(types.TypeDataSourceSync, body, headers),
		asynq.Queue(types.QueueSync), asynq.TaskID("original-task"), asynq.MaxRetry(0), asynq.Timeout(123*time.Second))
	require.NoError(t, err)
	for i, expected := range []int{1, 0} {
		n, err := service.RecoverGeneralSyncCapacity(context.Background(), db, inspector, client)
		require.NoError(t, err, "sweep %d", i)
		require.Equal(t, expected, n)
	}
	for _, queue := range []string{types.QueueSync, types.QueueGeneralSync} {
		info, err := inspector.inspector.GetTaskInfo(queue, "original-task")
		require.NoError(t, err)
		require.Equal(t, body, info.Payload)
		require.Equal(t, headers, info.Headers)
		require.Zero(t, info.MaxRetry)
		require.Zero(t, info.Retried)
		require.Equal(t, 123*time.Second, info.Timeout)
		require.Equal(t, asynq.TaskStatePending, info.State)
	}
	require.NoError(t, inspector.inspector.ArchiveTask(types.QueueSync, "original-task"))
	live, err := inspector.LiveSyncTasks(context.Background())
	require.NoError(t, err)
	require.True(t, live[types.SyncTaskKey(1, "source", "run")], "new lane is a live delivery")
	_, err = client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, body),
		asynq.Queue(types.QueueSync), asynq.TaskID("paused-task"))
	require.NoError(t, err)
	require.NoError(t, inspector.inspector.PauseQueue(types.QueueSync))
	tasks, err := inspector.PendingSharedSyncDeliveries(context.Background())
	require.NoError(t, err)
	require.Empty(t, tasks, "explicit pause cannot be bypassed by the relay")
}

func TestGeneralSyncRecoveryRotatesBoundedPages(t *testing.T) {
	r := miniredis.RunT(t)
	raw := redis.NewClient(&redis.Options{Addr: r.Addr()})
	defer func() { require.NoError(t, raw.Close()) }()
	client := asynq.NewClientFromRedisClient(raw)
	inspector := &asynqTaskInspector{redis: raw, inspector: asynq.NewInspectorFromRedisClient(raw)}
	for i := range 101 {
		_, err := client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, nil),
			asynq.Queue(types.QueueSync), asynq.TaskID(fmt.Sprintf("pending-%d", i)))
		require.NoError(t, err)
	}
	seen := map[string]bool{}
	for _, size := range []int{100, 1} {
		tasks, err := inspector.PendingSharedSyncDeliveries(context.Background())
		require.NoError(t, err)
		require.Len(t, tasks, size)
		for _, task := range tasks {
			seen[task.ID] = true
		}
	}
	require.Len(t, seen, 101, "ineligible first-page tasks must not starve later pending work")
}

func TestGeneralSyncReservedCapacitySurvivesFeishuSaturation(t *testing.T) {
	t.Setenv("FEISHU_APPROVED_SYNC_LANE_ENABLED", "1")
	addr := os.Getenv("FEISHU_RECOVERY_REAL_REDIS_ADDR")
	if addr == "" {
		addr = miniredis.RunT(t).Addr()
	} else {
		require.Regexp(t, `^knowhub-recovery-redis-[a-f0-9]{12}:6379$`, addr)
	}
	opt := asynq.RedisClientOpt{Addr: addr}
	client := asynq.NewClient(opt)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	occupied := make(chan struct{}, 4)
	release := make(chan struct{})
	completed := make(chan struct{}, 1)
	var genericCalls atomic.Int64
	mux := asynq.NewServeMux()
	mux.HandleFunc(types.TypeDataSourceSync, func(ctx context.Context, task *asynq.Task) error {
		if string(task.Payload()) == "feishu" {
			if types.IsGeneralSyncWorker(ctx) {
				return types.ErrGeneralSyncCapacityReserved
			}
			occupied <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if !types.IsGeneralSyncWorker(ctx) {
			return types.ErrGeneralSyncCapacityReserved
		}
		genericCalls.Add(1)
		completed <- struct{}{}
		return nil
	})
	maintenance := asynq.NewServer(opt, asynq.Config{
		Concurrency: 4, Queues: types.QueueWeightsForPool(types.WorkerPoolMaintenance),
		TaskCheckInterval: 10 * time.Millisecond, ShutdownTimeout: time.Second,
	})
	require.NoError(t, maintenance.Start(mux))
	defer maintenance.Shutdown()
	defer close(release)
	for range 4 {
		_, err := client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, []byte("feishu")),
			asynq.Queue(types.QueueApprovedSync), asynq.MaxRetry(0))
		require.NoError(t, err)
	}
	for range 4 {
		select {
		case <-occupied:
		case <-time.After(5 * time.Second):
			t.Fatal("maintenance workers were not saturated")
		}
	}
	// A historical Feishu task in the old queue must remain untouched,
	// including deliveries with no retry budget left.
	old, err := client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, []byte("feishu")),
		asynq.Queue(types.QueueSync), asynq.MaxRetry(0))
	require.NoError(t, err)
	_, err = client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, []byte("gitlab")),
		asynq.Queue(types.QueueGeneralSync), asynq.MaxRetry(0))
	require.NoError(t, err)
	reserved := asynq.NewServer(opt, asynq.Config{
		Concurrency:       types.DefaultGeneralSyncWorkerConcurrency,
		Queues:            types.QueueWeightsForPool(types.WorkerPoolGeneralSync),
		TaskCheckInterval: 10 * time.Millisecond, ShutdownTimeout: time.Second,
		IsFailure: asynqIsFailure, RetryDelayFunc: asynqRetryDelayFunc,
	})
	require.NoError(t, reserved.Start(generalSyncMux(mux)))
	defer reserved.Shutdown()
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("general sync starved while Feishu workers were occupied")
	}
	require.EqualValues(t, 1, genericCalls.Load())
	inspector := asynq.NewInspector(opt)
	t.Cleanup(func() { require.NoError(t, inspector.Close()) })
	require.Eventually(t, func() bool {
		info, err := inspector.GetTaskInfo(types.QueueSync, old.ID)
		untouched := err == nil && info.State == asynq.TaskStatePending && info.Retried == 0
		return untouched && string(info.Payload) == "feishu"
	}, 2*time.Second, time.Millisecond)
	require.Zero(t, types.QueueWeightsForPool(types.WorkerPoolGeneralSync)[types.QueueApprovedSync])
}
