package types

import "time"

// Email is a protected candidate snapshot, never a login credential or proof of ownership.
type SourceIDBindingReview struct {
	ID              string `gorm:"primaryKey;size:36"`
	Issuer          string
	EmployeeNo      string
	Subject         string
	UserID          string
	Email           string
	Status          string
	ReviewedBy      string
	ReviewReference string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SourceIDBindingAudit struct {
	ID              string `gorm:"primaryKey;size:36"`
	ReviewID        string
	Action          string
	ReviewedBy      string
	ReviewReference string
	CreatedAt       time.Time
}

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

// Companion to SourceIDSession. Local sessions have an empty external index;
// it is never manufactured from an employee or OIDC subject identifier.
type SourceIDLocalSession struct {
	ID                  string `gorm:"primaryKey;size:36"`
	BindingID           string
	Subject             string
	PolicyRef           string
	AdmissionPolicyRef  string
	AdmissionValidUntil time.Time
	LastSeenAt          time.Time
	IdleSeconds         int64
	AccessSeconds       int64
	RefreshEnabled      bool
	AdmissionSource     string
	AdmissionCheckedAt  time.Time
	RecheckSupported    bool
	Issuer              string    `gorm:"-"`
	EmployeeNo          string    `gorm:"-"`
	AbsoluteExpiresAt   time.Time `gorm:"-"`
	Revoked             bool      `gorm:"-"`
}

type SourceIDAdmissionOutcome string
type SourceIDTokenOperationKey struct{}

const (
	SourceIDAdmissionAllow   SourceIDAdmissionOutcome = "ALLOW"
	SourceIDAdmissionDeny    SourceIDAdmissionOutcome = "DENY"
	SourceIDAdmissionUnknown SourceIDAdmissionOutcome = "UNKNOWN"
)

// These structures carry protected identity input only in process memory.
// Never log/serialize a request, claims map, adapter error or provider response.
type SourceIDAdmissionRequest struct {
	Issuer, Subject, EmployeeNo, Stage string
	VerifiedClaims                     map[string]interface{}
	AuthorizationVerified              bool
	PromptLoginRequested               bool
	AuthorizationVerifiedAt            time.Time
}
type SourceIDAdmissionDecision struct {
	Outcome               SourceIDAdmissionOutcome
	Source, ContractRef   string
	CheckedAt, ValidUntil time.Time
	RecheckSupported      bool
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
