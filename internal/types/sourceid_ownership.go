package types

import "time"

// SourceIDAccountOwnership defines source idaccount ownership state.
// Protected account-ownership evidence, not another user directory. Never
// expose these records in application/CLI logs, public APIs or config output.
type SourceIDAccountOwnership struct {
	ID           string    `gorm:"primaryKey" json:"-"`
	Issuer       string    `json:"-"`
	EmployeeNo   string    `json:"-"`
	UserID       string    `json:"-"`
	Email        string    `json:"-"`
	Status       string    `json:"-"`
	Revision     int64     `json:"-"`
	PolicyRef    string    `json:"-"`
	OwnershipRef string    `json:"-"`
	SourceRef    string    `json:"-"`
	VerifiedAt   time.Time `json:"-"`
	ValidUntil   time.Time `json:"-"`
	CreatedAt    time.Time `json:"-"`
	UpdatedAt    time.Time `json:"-"`
}

// TableName returns the database table name.
func (SourceIDAccountOwnership) TableName() string { return "source_id_account_ownership" }

// SourceIDOwnershipAudit defines source idownership audit state.
type SourceIDOwnershipAudit struct {
	ID          string    `gorm:"primaryKey" json:"-"`
	OwnershipID string    `json:"-"`
	Revision    int64     `json:"-"`
	Actor       string    `json:"-"`
	Action      string    `json:"-"`
	CreatedAt   time.Time `json:"-"`
}

// TableName returns the database table name.
func (SourceIDOwnershipAudit) TableName() string { return "source_id_ownership_audits" }
