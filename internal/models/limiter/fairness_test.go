package limiter

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type admissionOrderRole struct{}

type admissionOrderHook struct {
	olderBlocked, resumeOlder, newerBlocked chan struct{}
	olderOnce, newerOnce                    sync.Once
}

func (h *admissionOrderHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *admissionOrderHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *admissionOrderHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if err != nil || cmd.Name() != "eval" && cmd.Name() != "evalsha" {
			return err
		}
		result, ok := cmd.(*redis.Cmd)
		if !ok {
			return err
		}
		n, parseErr := result.Int()
		if parseErr != nil || n != 0 {
			return err
		}
		switch ctx.Value(admissionOrderRole{}) {
		case "older":
			h.olderOnce.Do(func() {
				close(h.olderBlocked)
				select {
				case <-h.resumeOlder:
				case <-ctx.Done():
				}
			})
		case "newer":
			h.newerOnce.Do(func() { close(h.newerBlocked) })
		}
		return err
	}
}

// Pause an older caller immediately after Redis confirms it is waiting. A
// caller arriving after the holder releases must not steal that available slot.
func TestRedisLimiterOlderWaiterPrecedesFreshCaller(t *testing.T) {
	l, _, rdb := newTestLimiter(t, time.Minute, time.Millisecond)
	checkOlderWaiterAdmission(t, l, rdb, "shared-model")
}

func TestRedisLimiterRealRedisFairAdmission(t *testing.T) {
	address := os.Getenv("MODEL_LIMITER_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("isolated real Redis fixture not configured")
	}
	rdb := redis.NewClient(&redis.Options{Addr: address, MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	model := "fairness-test-" + uuid.NewString()
	root := keyPrefix + model
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = rdb.Del(ctx, root, root+":waiting", root+":waiting-leases", root+":sequence").Err()
	})
	l := NewRedisLimiter(rdb).(*redisLimiter)
	l.pollInterval = time.Millisecond
	checkOlderWaiterAdmission(t, l, rdb, model)
}

func checkOlderWaiterAdmission(t *testing.T, l *redisLimiter, rdb *redis.Client, model string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	holder, err := l.Acquire(ctx, model, 1)
	require.NoError(t, err)
	defer holder()
	hook := &admissionOrderHook{
		olderBlocked: make(chan struct{}), resumeOlder: make(chan struct{}), newerBlocked: make(chan struct{}),
	}
	rdb.AddHook(hook)
	olderAcquired, newerAcquired := make(chan struct{}), make(chan struct{})
	olderRelease, newerRelease := make(chan struct{}), make(chan struct{})
	start := func(role string, acquired, releaseAllowed chan struct{}) {
		go func() {
			release, acquireErr := l.Acquire(context.WithValue(ctx, admissionOrderRole{}, role), model, 1)
			if acquireErr != nil {
				return
			}
			defer release()
			close(acquired)
			select {
			case <-releaseAllowed:
			case <-ctx.Done():
			}
		}()
	}
	start("older", olderAcquired, olderRelease)
	select {
	case <-hook.olderBlocked:
	case <-ctx.Done():
		t.Fatal("older caller never reached the occupied semaphore")
	}
	holder()
	start("newer", newerAcquired, newerRelease)
	select {
	case <-newerAcquired:
		t.Fatal("fresh caller overtook an older model waiter")
	case <-hook.newerBlocked:
	case <-ctx.Done():
		t.Fatal("fresh caller did not reach admission")
	}
	close(hook.resumeOlder)
	select {
	case <-olderAcquired:
	case <-ctx.Done():
		t.Fatal("older waiter was starved")
	}
	require.EqualValues(t, 1, zcard(t, rdb, model))
	select {
	case <-newerAcquired:
		t.Fatal("concurrency cap was exceeded")
	default:
	}
	close(olderRelease)
	select {
	case <-newerAcquired:
	case <-ctx.Done():
		t.Fatal("next waiter did not inherit the freed slot")
	}
	close(newerRelease)
}

func TestRedisLimiterCancelledWaiterDoesNotBlockNext(t *testing.T) {
	l, _, rdb := newTestLimiter(t, time.Minute, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	holder, err := l.Acquire(ctx, "cancelled-waiter", 1)
	require.NoError(t, err)
	defer holder()
	wait, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		release, _ := l.Acquire(wait, "cancelled-waiter", 1)
		release()
		close(done)
	}()
	require.Eventually(t, func() bool {
		return rdb.ZCard(ctx, keyPrefix+"cancelled-waiter:waiting").Val() == 1
	}, time.Second, time.Millisecond)
	stop()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("cancelled admission did not return")
	}
	holder()
	release, err := l.Acquire(ctx, "cancelled-waiter", 1)
	require.NoError(t, err)
	defer release()
	require.NoError(t, ctx.Err(), "cancelled waiter retained free capacity")
	require.EqualValues(t, 1, zcard(t, rdb, "cancelled-waiter"))
}

func TestRedisLimiterExpiredWaiterCannotReserveCapacity(t *testing.T) {
	l, _, rdb := newTestLimiter(t, time.Minute, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	root := keyPrefix + "expired-waiter"
	// Simulate a waiting process that crashed without running its cleanup.
	require.NoError(t, rdb.ZAdd(ctx, root+":waiting", redis.Z{Score: 1, Member: "crashed"}).Err())
	require.NoError(t, rdb.ZAdd(ctx, root+":waiting-leases",
		redis.Z{Score: float64(time.Now().Add(-time.Second).UnixMilli()), Member: "crashed"}).Err())
	require.NoError(t, rdb.Set(ctx, root+":sequence", 1, time.Minute).Err())
	release, err := l.Acquire(ctx, "expired-waiter", 1)
	require.NoError(t, err)
	defer release()
	require.NoError(t, ctx.Err())
	require.EqualValues(t, 1, zcard(t, rdb, "expired-waiter"))
	require.Zero(t, rdb.ZCard(ctx, root+":waiting").Val())
	require.Zero(t, rdb.ZCard(ctx, root+":waiting-leases").Val())
}
