// Package limiter provides a distributed, per-key concurrency governor for
// outbound model-provider calls. The shared finite resource is the model
// provider (its request/concurrency budget), so concurrency is capped at the
// model-client layer — keyed by model ID — rather than at the asynq queue layer
// (queue weights are scheduling priority, not throttling).
//
// The Redis implementation is a self-healing distributed semaphore built on a
// sorted set: each held slot is a ZSET member (unique token) scored by its
// lease expiry. Acquire atomically prunes expired leases, counts live holders,
// and admits a new one only while under the limit. A background heartbeat
// refreshes the lease so long calls keep their slot; a crashed holder's lease
// simply expires and is reclaimed. Every backend error fails OPEN (the call is
// allowed) so a limiter/Redis outage can never halt model traffic.
package limiter

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ModelConcurrencyLimiter caps the number of concurrent in-flight calls per
// key (typically a model ID) across all processes sharing the same backend.
type ModelConcurrencyLimiter interface {
	// Acquire blocks until a slot for key is available or ctx is done. It
	// returns a release func that MUST be invoked to free the slot. On any
	// backend error (or ctx cancellation) it fails open: release is a no-op and
	// err is nil, so callers proceed without a slot rather than dropping the
	// call.
	Acquire(ctx context.Context, key string, limit int) (release func(), err error)
}

// RuntimeStat is a point-in-time view of a model semaphore. Active is
// cluster-wide for the Redis backend and process-local in Lite mode. Waiting is
// deliberately process-local: waiters block in application processes and are
// not represented in Redis.
type RuntimeStat struct {
	ModelID string `json:"model_id"`
	Name    string `json:"name"`
	Active  int64  `json:"active"`
	Waiting int64  `json:"waiting"`
	Limit   int    `json:"limit"`
}

type runtimeInspectable interface {
	RuntimeStats(context.Context) ([]RuntimeStat, error)
}

type trackedSemaphore struct {
	limit   atomic.Int64
	waiting atomic.Int64
	name    atomic.Value // string
}

// noop is the release returned on the fail-open / passthrough paths.
func noop() {}

const (
	// defaultLeaseTTL is the crash-recovery window, not a request timeout.
	// Live calls refresh their lease every ttl/3, so even a very long provider
	// request keeps its slot. Keeping this short prevents an app/container
	// restart from leaving an entire model budget apparently occupied for
	// minutes while the replacement workers are blocked behind dead holders.
	defaultLeaseTTL = 30 * time.Second
	// defaultPollInterval is how often a waiting acquirer re-checks for a free
	// slot. Small enough to stay responsive, large enough to avoid hammering
	// Redis under contention.
	defaultPollInterval = 200 * time.Millisecond
	// keyPrefix namespaces the semaphore ZSETs in Redis.
	keyPrefix = "weknora:modelsem:"
)

// acquireScript reserves available capacity for the oldest waiting calls, so
// a continuously re-entering workflow cannot starve an older image/OCR call.
// Both holders and waiters have leases; crashed waiters cannot block capacity
// indefinitely. Returns 1 on admission, 0 while waiting.
//
//	KEYS = holders, waiting order, waiting leases, sequence
//	ARGV[1] = now (unix ms)
//	ARGV[2] = limit
//	ARGV[3] = caller token
//	ARGV[4] = lease TTL (ms)
var acquireScript = redis.NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
local expired = redis.call('ZRANGEBYSCORE', KEYS[3], '-inf', ARGV[1])
for _, token in ipairs(expired) do
    redis.call('ZREM', KEYS[2], token)
    redis.call('ZREM', KEYS[3], token)
end
if not redis.call('ZSCORE', KEYS[2], ARGV[3]) then
    local sequence = redis.call('INCR', KEYS[4])
    redis.call('ZADD', KEYS[2], sequence, ARGV[3])
end
redis.call('ZADD', KEYS[3], ARGV[1] + ARGV[4], ARGV[3])
for index = 2, 4 do
    redis.call('PEXPIRE', KEYS[index], ARGV[4] * 2)
end
local free = tonumber(ARGV[2]) - redis.call('ZCARD', KEYS[1])
local rank = redis.call('ZRANK', KEYS[2], ARGV[3])
if rank < free then
    redis.call('ZREM', KEYS[2], ARGV[3])
    redis.call('ZREM', KEYS[3], ARGV[3])
    redis.call('ZADD', KEYS[1], ARGV[1] + ARGV[4], ARGV[3])
    redis.call('PEXPIRE', KEYS[1], ARGV[4] * 2)
    return 1
end
return 0
`)

type redisLimiter struct {
	rdb          *redis.Client
	ttl          time.Duration
	pollInterval time.Duration
	tracked      sync.Map // model ID -> *trackedSemaphore
}

// NewRedisLimiter builds a distributed limiter backed by rdb. A nil client
// yields a limiter that always fails open.
func NewRedisLimiter(rdb *redis.Client) ModelConcurrencyLimiter {
	return &redisLimiter{
		rdb:          rdb,
		ttl:          defaultLeaseTTL,
		pollInterval: defaultPollInterval,
	}
}

func (l *redisLimiter) Acquire(ctx context.Context, key string, limit int) (func(), error) {
	if l == nil || l.rdb == nil || limit <= 0 || key == "" {
		return noop, nil
	}

	zkey := keyPrefix + key
	orderKey, waitingLeaseKey := zkey+":waiting", zkey+":waiting-leases"
	keys := []string{zkey, orderKey, waitingLeaseKey, zkey + ":sequence"}
	entry, _ := l.tracked.LoadOrStore(key, &trackedSemaphore{})
	tracked := entry.(*trackedSemaphore)
	tracked.limit.Store(int64(limit))
	tracked.waiting.Add(1)
	defer tracked.waiting.Add(-1)
	token := uuid.NewString()
	ttlMs := l.ttl.Milliseconds()
	queued := false
	defer func() {
		if queued {
			// Remove only this caller's waiting reservation. A crashed caller
			// or Redis outage falls back to the bounded waiting-lease expiry.
			cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			pipe := l.rdb.Pipeline()
			pipe.ZRem(cleanup, orderKey, token)
			pipe.ZRem(cleanup, waitingLeaseKey, token)
			_, _ = pipe.Exec(cleanup)
		}
	}()

	// Reuse a single timer across poll iterations rather than allocating a new
	// one via time.After each loop: under sustained contention a waiter can
	// spin thousands of times, and every time.After timer lives until it fires.
	// Start it stopped so the first Reset below arms it cleanly.
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	for {
		now := time.Now().UnixMilli()
		res, err := acquireScript.Run(ctx, l.rdb, keys,
			now, limit, token, ttlMs).Int()
		if err != nil {
			// Fail open: a limiter outage must never block model traffic.
			logger.Warnf(ctx, "[ModelLimiter] acquire failed for key=%s, failing open: %v", key, err)
			return noop, nil
		}
		if res == 1 {
			queued = false
			return l.hold(zkey, token), nil
		}
		queued = true

		timer.Reset(l.pollInterval)
		select {
		case <-ctx.Done():
			// Fail open on cancellation too: let the inner call observe the
			// cancelled context and return its own error, rather than us
			// synthesising one here.
			return noop, nil
		case <-timer.C:
		}
	}
}

func (l *redisLimiter) RuntimeStats(ctx context.Context) ([]RuntimeStat, error) {
	stats := make([]RuntimeStat, 0)
	if l == nil || l.rdb == nil {
		return stats, nil
	}
	var firstErr error
	now := time.Now().UnixMilli()
	l.tracked.Range(func(rawKey, rawValue any) bool {
		modelID := rawKey.(string)
		tracked := rawValue.(*trackedSemaphore)
		active, err := l.rdb.ZCount(ctx, keyPrefix+modelID, strconv.FormatInt(now+1, 10), "+inf").Result()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return true
		}
		name, _ := tracked.name.Load().(string)
		stats = append(stats, RuntimeStat{ModelID: modelID, Name: name, Active: active, Waiting: tracked.waiting.Load(), Limit: int(tracked.limit.Load())})
		return true
	})
	sort.Slice(stats, func(i, j int) bool { return stats[i].ModelID < stats[j].ModelID })
	return stats, firstErr
}

func (l *redisLimiter) SetModelName(modelID, name string) {
	if modelID == "" || name == "" {
		return
	}
	entry, _ := l.tracked.LoadOrStore(modelID, &trackedSemaphore{})
	entry.(*trackedSemaphore).name.Store(name)
}

// hold starts a heartbeat that refreshes the lease and returns an idempotent
// release that stops the heartbeat and drops the slot.
func (l *redisLimiter) hold(zkey, token string) func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(l.ttl / 3)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				now := time.Now().UnixMilli()
				// Detached ctx: the heartbeat must outlive request ctx up to
				// release. Best-effort; a failed refresh just risks early
				// reclamation, which the limit already tolerates.
				//
				// Refresh BOTH the member lease score AND the ZSET key's own
				// TTL. The acquire script only PEXPIREs the key on admission,
				// so a semaphore that stays saturated with no slot turnover
				// would otherwise let the whole key expire after ttl*2 —
				// dropping every live lease and admitting over the limit. The
				// heartbeat pushes the key TTL out in lockstep with the lease.
				bg := context.Background()
				_ = l.rdb.ZAdd(bg, zkey, redis.Z{
					Score:  float64(now + l.ttl.Milliseconds()),
					Member: token,
				}).Err()
				_ = l.rdb.PExpire(bg, zkey, l.ttl*2).Err()
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			_ = l.rdb.ZRem(context.Background(), zkey, token).Err()
		})
	}
}
