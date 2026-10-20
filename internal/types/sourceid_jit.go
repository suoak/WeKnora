package types

import "time"

// SourceIDJITRegistration defines source idjitregistration state.
// Internal provenance for newly registered accounts, never an employee export.
type SourceIDJITRegistration struct {
	ID         string    `json:"-"`
	Issuer     string    `json:"-"`
	EmployeeNo string    `json:"-"`
	Email      string    `json:"-"`
	UserID     string    `json:"-"`
	PolicyRef  string    `json:"-"`
	CreatedAt  time.Time `json:"-"`
}

// TableName returns the database table name.
func (SourceIDJITRegistration) TableName() string { return "source_id_jit_registrations" }
