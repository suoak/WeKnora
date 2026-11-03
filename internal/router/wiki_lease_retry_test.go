package router

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWikiLeaseContentionPreservesOriginalTriggerAndRetryBudget(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	queue := asynq.NewClientFromRedisClient(client)
	t.Cleanup(func() { _ = queue.Close() })
	worker := asynq.NewServerFromRedisClient(client, asynq.Config{
		Concurrency: 1, Queues: map[string]int{types.QueueWiki: 1},
		IsFailure: asynqIsFailure, TaskCheckInterval: 10 * time.Millisecond,
		DelayedTaskCheckInterval: 10 * time.Millisecond, ShutdownTimeout: time.Second,
		RetryDelayFunc: func(int, error, *asynq.Task) time.Duration { return 0 },
		LogLevel:       asynq.FatalLevel,
	})
	var attempts atomic.Int32
	done := make(chan struct{}, 1)
	bad := make(chan string, 4)
	err := worker.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		id, _ := asynq.GetTaskID(ctx)
		retried, _ := asynq.GetRetryCount(ctx)
		if id != "original-finalize" || retried != 0 {
			bad <- "original identity or retry count changed"
		}
		if attempts.Add(1) <= 3 {
			return service.ErrWikiIngestConcurrent
		}
		done <- struct{}{}
		return nil
	}))
	require.NoError(t, err)
	t.Cleanup(worker.Shutdown)
	_, err = queue.Enqueue(asynq.NewTask("wiki:finalize", []byte(`{}`)),
		asynq.Queue(types.QueueWiki), asynq.TaskID("original-finalize"), asynq.MaxRetry(1))
	require.NoError(t, err)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("contended trigger exhausted retries instead of yielding")
	}
	select {
	case failure := <-bad:
		t.Fatal(failure)
	default:
	}
	require.EqualValues(t, 4, attempts.Load())
	require.True(t, asynqIsFailure(errors.New("sync execution failed")))
	require.False(t, asynqIsFailure(nil))
}
