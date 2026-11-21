package postgres

import (
	"database/sql"

	"gorm.io/gorm"
)

// Dimension EXISTS probes can spend more time compiling their canonical policy
// expressions than scanning the index. Keep the option inside this transaction;
// vector calculations and subsequent pooled users retain their configuration.
func portalDimensionQuerySession(db *gorm.DB, query func(*gorm.DB) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL jit = off").Error; err != nil {
			return err
		}
		return query(tx)
	}, &sql.TxOptions{ReadOnly: true})
}
