package postgres

import (
	"database/sql"

	"gorm.io/gorm"
)

// Dimension EXISTS probes can spend more time compiling their canonical policy
// expressions than scanning the index. Keep the option inside this transaction;
// vector calculations and subsequent pooled users retain their configuration.
func portalDimensionQuerySession(db *gorm.DB, query func(*gorm.DB) error) error {
	return portalReadQuerySession(db, query, "SET LOCAL jit = off")
}

// Common natural-language terms can push an exact canonical BM25 plan above
// the expression JIT threshold. Compiling its correlated proofs can exceed the
// entire mode budget. This changes execution only inside this read transaction.
func portalKeywordQuerySession(db *gorm.DB, query func(*gorm.DB) error) error {
	return portalReadQuerySession(db, query, "SET LOCAL jit = off")
}

// Vector expression JIT remains useful; bound its compilation effort instead
// of disabling expression execution for large, exact-distance scans.
func portalVectorQuerySession(db *gorm.DB, query func(*gorm.DB) error) error {
	return portalReadQuerySession(db, query,
		"SET LOCAL jit_inline_above_cost = -1", "SET LOCAL jit_optimize_above_cost = -1")
}

func portalReadQuerySession(db *gorm.DB, query func(*gorm.DB) error, settings ...string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, statement := range settings {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return query(tx)
	}, &sql.TxOptions{ReadOnly: true})
}
