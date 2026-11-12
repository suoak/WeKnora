package router

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceCleanupRunsWhileSyncCapacityIsFull(t *testing.T) {
	addr := os.Getenv("FEISHU_RECOVERY_REAL_REDIS_ADDR")
	if addr == "" {
		t.Skip("requires isolated real Redis")
	}
	require.Regexp(t, `^knowhub-recovery-redis-[a-f0-9]{12}:6379$`, addr)
	t.Setenv("FEISHU_APPROVED_SYNC_LANE_ENABLED", "1")
	opt := asynq.RedisClientOpt{Addr: addr}
	client := asynq.NewClient(opt)
	defer func() { require.NoError(t, client.Close()) }()
	occupied := make(chan struct{}, 4)
	release := make(chan struct{})
	deleted := make(chan struct{}, 1)
	mux := asynq.NewServeMux()
	mux.HandleFunc(types.TypeDataSourceSync, func(ctx context.Context, _ *asynq.Task) error {
		occupied <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	mux.HandleFunc(types.TypeKnowledgeListDelete, func(context.Context, *asynq.Task) error {
		deleted <- struct{}{}
		return nil
	})
	maintenance := asynq.NewServer(opt, asynq.Config{
		Concurrency: 4, Queues: types.QueueWeightsForPool(types.WorkerPoolMaintenance),
		TaskCheckInterval: 100 * time.Millisecond, ShutdownTimeout: time.Second,
	})
	require.NoError(t, maintenance.Start(mux))
	defer maintenance.Shutdown()
	defer close(release)
	for range 4 {
		_, err := client.Enqueue(asynq.NewTask(types.TypeDataSourceSync, nil),
			asynq.Queue(types.QueueApprovedSync), asynq.MaxRetry(0))
		require.NoError(t, err)
	}
	for range 4 {
		select {
		case <-occupied:
		case <-time.After(8 * time.Second):
			t.Fatal("sync workers not saturated")
		}
	}
	_, err := client.Enqueue(asynq.NewTask(types.TypeKnowledgeListDelete, nil),
		asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(0))
	require.NoError(t, err)
	select {
	case <-deleted:
		t.Fatal("occupied workers unexpectedly handled cleanup")
	case <-time.After(200 * time.Millisecond):
	}
	post := asynq.NewServer(opt, asynq.Config{
		Concurrency: 2, Queues: types.QueueWeightsForPool(types.WorkerPoolPostProcess),
		TaskCheckInterval: 100 * time.Millisecond, ShutdownTimeout: time.Second,
	})
	require.NoError(t, post.Start(mux))
	defer post.Shutdown()
	select {
	case <-deleted:
	case <-time.After(8 * time.Second):
		t.Fatal("cleanup starved behind active syncs")
	}
	require.Zero(t, types.QueueWeightsForPool(types.WorkerPoolPostProcess)[types.QueueApprovedSync])
	require.Zero(t, types.QueueWeightsForPool(types.WorkerPoolPostProcess)[types.QueueSync])
}
