package router

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusynccontrol"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestAutomaticCanaryAdmissionPreservesOriginalTaskAndBusinessRetryBudget(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	queue := asynq.NewClientFromRedisClient(client)
	t.Cleanup(func() { _ = queue.Close() })
	worker := asynq.NewServerFromRedisClient(client, asynq.Config{
		Concurrency: 1, Queues: map[string]int{types.QueueApprovedSync: 1},
		IsFailure: asynqIsFailure, TaskCheckInterval: 10 * time.Millisecond,
		DelayedTaskCheckInterval: 10 * time.Millisecond, ShutdownTimeout: time.Second,
		RetryDelayFunc: func(int, error, *asynq.Task) time.Duration { return 0 },
		LogLevel:       asynq.FatalLevel,
	})
	var attempts, business atomic.Int32
	done := make(chan struct{}, 1)
	bad := make(chan string, 8)
	err := worker.Start(asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		id, _ := asynq.GetTaskID(ctx)
		retried, _ := asynq.GetRetryCount(ctx)
		maximum, _ := asynq.GetMaxRetry(ctx)
		if id != "original-source-request" || string(task.Payload()) != `{"source":"original"}` || maximum != 1 {
			bad <- "source request identity, payload or maximum retry changed"
		}
		if attempts.Add(1) <= 3 {
			if retried != 0 {
				bad <- "Canary admission consumed a source retry"
			}
			return errors.Join(feishusynccontrol.ErrNotReady, feishusynccontrol.ErrAutomaticCanaryPending)
		}
		count := business.Add(1)
		if retried != int(count)-1 {
			bad <- "a real source failure did not consume its original retry budget"
		}
		if count == 1 {
			return errors.New("actual connector failure")
		}
		done <- struct{}{}
		return nil
	}))
	require.NoError(t, err)
	t.Cleanup(worker.Shutdown)
	_, err = queue.Enqueue(asynq.NewTask("datasource:sync", []byte(`{"source":"original"}`)),
		asynq.Queue(types.QueueApprovedSync), asynq.TaskID("original-source-request"), asynq.MaxRetry(1))
	require.NoError(t, err)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("authorized intent exhausted retries before completing Canary admission")
	}
	select {
	case failure := <-bad:
		t.Fatal(failure)
	default:
	}
	require.EqualValues(t, 5, attempts.Load())
	require.EqualValues(t, 2, business.Load())
	require.True(t, asynqIsFailure(feishusynccontrol.ErrNotReady))
	require.True(t, asynqIsFailure(errors.New("SYSTEM_CANARY_OTHER_SOURCE")))
	require.True(t, asynqIsFailure(context.Canceled))
	require.Equal(t, 30*time.Second, asynqRetryDelayFunc(0, feishusynccontrol.ErrAutomaticCanaryPending, nil))
}
