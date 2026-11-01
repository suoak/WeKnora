package types

import "context"

type legacyReconciliationReadKey struct{}

// WithLegacyReconciliationRead is issued only by the signed reconciliation
// controller after a real SQL Claim. It is NOT full reconciliation completion:
// checkpoints must not release source hold before the entire scan succeeds.
func WithLegacyReconciliationRead(ctx context.Context) context.Context {
	e, ok := SyncExecutionFromContext(ctx)
	if !ok || e.Generation < 1 {
		return ctx
	}
	return context.WithValue(ctx, legacyReconciliationReadKey{}, e)
}

// LegacyReconciliationRead matches only the currently claimed source execution.
func LegacyReconciliationRead(ctx context.Context, source string) bool {
	proof, ok := ctx.Value(legacyReconciliationReadKey{}).(SyncExecution)
	e, claimed := SyncExecutionFromContext(ctx)
	return ok && claimed && proof == e && e.DataSourceID == source && e.Generation > 0
}
