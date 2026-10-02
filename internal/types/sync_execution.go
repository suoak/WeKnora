package types

import (
	"context"
	"errors"
	"time"
)

// ErrStaleSyncExecution indicates that the attempt no longer owns the sync execution.
var ErrStaleSyncExecution = errors.New("stale datasource sync execution")

// SyncExecutionStaleAfter is the stale window used by startup recovery; timestamps are liveness only,
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

// WithSyncExecution attaches the DB-issued attempt identity to the context.
func WithSyncExecution(ctx context.Context, execution SyncExecution) context.Context {
	return context.WithValue(ctx, syncExecutionContextKey{}, execution)
}

// SyncExecutionFromContext retrieves the DB-issued attempt identity, if present.
func SyncExecutionFromContext(ctx context.Context) (SyncExecution, bool) {
	execution, ok := ctx.Value(syncExecutionContextKey{}).(SyncExecution)
	return execution, ok
}
