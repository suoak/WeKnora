package types

import "time"

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

func (SourceIDJITRegistration) TableName() string { return "source_id_jit_registrations" }
