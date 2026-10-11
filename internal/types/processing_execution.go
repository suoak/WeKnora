package types

import "context"

// ProcessingExecution is a trusted internal capability, never an API model.
// An empty execution ID is a generation-only enrichment guard; a main worker
// must carry its claimed nonce and a live database lease.
type ProcessingExecution struct {
	TenantID    uint64 `json:"-"`
	KnowledgeID string `json:"-"`
	Generation  int64  `json:"-"`
	ExecutionID string `json:"-"`
}

type processingExecutionContextKey struct{}

func WithProcessingExecution(ctx context.Context, execution ProcessingExecution) context.Context {
	return context.WithValue(ctx, processingExecutionContextKey{}, execution)
}

func ProcessingExecutionFromContext(ctx context.Context) (ProcessingExecution, bool) {
	execution, ok := ctx.Value(processingExecutionContextKey{}).(ProcessingExecution)
	return execution, ok
}
