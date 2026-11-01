package types

import (
	"context"

	"gorm.io/gorm"
)

type reconciliationCommitKey struct{}

type reconciliationCommitProof struct {
	execution SyncExecution
	guard     func(*gorm.DB) error
}

// WithReconciliationCommitGuard binds a server-issued live policy check to the
// actual SQL execution. It cannot be supplied by a request header or task field.
func WithReconciliationCommitGuard(ctx context.Context, guard func(*gorm.DB) error) context.Context {
	e, ok := SyncExecutionFromContext(ctx)
	if !ok || e.Generation < 1 || guard == nil {
		return ctx
	}
	return context.WithValue(ctx, reconciliationCommitKey{}, reconciliationCommitProof{execution: e, guard: guard})
}

// CheckReconciliationPublication rechecks the purpose before private writes or
// full-reconciliation publication, without restricting terminal failure cleanup.
func CheckReconciliationPublication(tx *gorm.DB) error {
	ctx := tx.Statement.Context
	e, claimed := SyncExecutionFromContext(ctx)
	proof, ok := ctx.Value(reconciliationCommitKey{}).(reconciliationCommitProof)
	if !ok && !LegacyReconciliationRead(ctx, e.DataSourceID) {
		return nil
	}
	if !ok || !claimed || proof.execution != e || proof.guard == nil {
		return ErrLegacySourceRestricted
	}
	return proof.guard(tx)
}
