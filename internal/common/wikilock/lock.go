// Package wikilock serializes proof-backed Wiki generation and reset per KB.
package wikilock

import (
	"context"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/common/redislock"
	"github.com/redis/go-redis/v9"
)

var local sync.Map

// With shares one renewable lease between ingest, finalize and regeneration.
func With(ctx context.Context, client *redis.Client, kb string, fn func(context.Context) error) error {
	if client != nil {
		return redislock.WithRenewableLock(ctx, client, "wiki:proof:generation:"+kb,
			time.Minute, 15*time.Second, fn)
	}
	value, _ := local.LoadOrStore(kb, make(chan struct{}, 1))
	ch := value.(chan struct{})
	select {
	case ch <- struct{}{}:
		defer func() { <-ch }()
		return fn(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Detached retains values and cleanup time but still observes lost ownership.
func Detached(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	child, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	owner := redislock.OwnershipContext(ctx)
	if owner.Err() != nil {
		cancel()
	}
	stop := context.AfterFunc(owner, cancel)
	return child, func() { stop(); cancel() }
}
