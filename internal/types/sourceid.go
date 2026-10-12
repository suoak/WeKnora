package types

import "time"

// ExternalIdentity never replaces a local user's primary key or permissions.
type ExternalIdentity struct {
	ID          string `gorm:"primaryKey;size:36"`
	Provider    string `gorm:"not null"`
	Issuer      string `gorm:"not null"`
	Subject     string `gorm:"not null"`
	EmployeeNo  string `gorm:"not null"`
	UserID      string `gorm:"not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
	LastLoginAt time.Time
}

// SourceIDEnrollment is an operator-reviewed mapping, never inferred from email.
type SourceIDEnrollment struct {
	ID              string `gorm:"primaryKey;size:36"`
	Issuer          string
	EmployeeNo      string
	UserID          string
	ReviewedBy      string
	ReviewReference string
	CreatedAt       time.Time
}

type SourceIDSession struct {
	ID               string `gorm:"primaryKey;size:36"`
	Issuer           string
	SessionIndexHash string
	UserID           string
	Revoked          bool
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

// SourceIDLogout persists a tombstone, including when logout precedes callback.
type SourceIDLogout struct {
	Key       string `gorm:"primaryKey"`
	CreatedAt time.Time
}

type OIDCTransaction struct {
	Key       string `gorm:"primaryKey"`
	Nonce     string
	Verifier  string
	Result    string
	ExpiresAt time.Time
	Consumed  bool
}

func (OIDCTransaction) TableName() string { return "oidc_transactions" }
