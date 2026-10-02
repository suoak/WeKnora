package types

import (
	"context"
	"errors"
	"time"
)

var ErrStaleSyncExecution = errors.New("stale datasource sync execution")

// Same stale window used by startup recovery; timestamps are liveness only,
// never the ownership identity used to fence writes.
const SyncExecutionStaleAfter = 30 * time.Minute

// SyncExecution is a DB-issued attempt identity, never a timestamp or queue ID.
type SyncExecution struct {
	DataSourceID string
	SyncLogID    string
	TenantID     uint64
	Generation   int64
}

type syncExecutionContextKey struct{}

func WithSyncExecution(ctx context.Context, execution SyncExecution) context.Context {
	return context.WithValue(ctx, syncExecutionContextKey{}, execution)
}

func SyncExecutionFromContext(ctx context.Context) (SyncExecution, bool) {
	execution, ok := ctx.Value(syncExecutionContextKey{}).(SyncExecution)
	return execution, ok
}
