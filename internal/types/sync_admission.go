package types

import (
	"context"

	"gorm.io/gorm"
)

type syncAdmissionKey struct{}

type syncAdmissionGuard struct {
	source, run string
	tenant      uint64
	check       func(*gorm.DB, SyncExecution) error
}

// WithSyncAdmissionGuard attaches a server-owned continuation check, never an
// authority flag from a task/header. Claim invokes it inside the datasource-first
// transaction after assigning the actual generation and rolls back on failure.
func WithSyncAdmissionGuard(ctx context.Context, source, run string, tenant uint64,
	check func(*gorm.DB, SyncExecution) error,
) context.Context {
	return context.WithValue(
		ctx,
		syncAdmissionKey{},
		syncAdmissionGuard{source: source, run: run, tenant: tenant, check: check},
	)
}

// CheckSyncAdmission verifies the server-owned continuation inside the real claim transaction.
func CheckSyncAdmission(tx *gorm.DB, execution SyncExecution) error {
	proof, present := tx.Statement.Context.Value(syncAdmissionKey{}).(syncAdmissionGuard)
	if !present {
		return nil // Existing non-system-service callers preserve their protocol.
	}
	if proof.check == nil || proof.source != execution.DataSourceID || proof.run != execution.SyncLogID ||
		proof.tenant != execution.TenantID || execution.Generation < 1 {
		return ErrStaleSyncExecution
	}
	return proof.check(tx, execution)
}
