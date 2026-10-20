package types

import "time"

// SourceIDBindingReview defines source idbinding review state.
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

// SourceIDBindingAudit defines source idbinding audit state.
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

// SourceIDSession defines source idsession state.
type SourceIDSession struct {
	ID               string `gorm:"primaryKey;size:36"`
	Issuer           string
	SessionIndexHash string
	UserID           string
	Revoked          bool
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

// SourceIDLocalSession defines source idlocal session state.
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

type (
	// SourceIDAdmissionOutcome defines source idadmission outcome state.
	SourceIDAdmissionOutcome string
	// SourceIDTokenOperationKey defines source idtoken operation key state.
	SourceIDTokenOperationKey struct{}
)

const (
	// SourceIDAdmissionAllow identifies source idadmission allow.
	SourceIDAdmissionAllow SourceIDAdmissionOutcome = "ALLOW"
	// SourceIDAdmissionDeny refuses an unverified authorization transaction.
	SourceIDAdmissionDeny SourceIDAdmissionOutcome = "DENY"
	// SourceIDAdmissionUnknown represents missing or unavailable admission evidence.
	SourceIDAdmissionUnknown SourceIDAdmissionOutcome = "UNKNOWN"
)

// SourceIDAdmissionRequest defines source idadmission request state.
// These structures carry protected identity input only in process memory.
// Never log/serialize a request, claims map, adapter error or provider response.
type SourceIDAdmissionRequest struct {
	Issuer, Subject, EmployeeNo, Stage string
	VerifiedClaims                     map[string]interface{}
	AuthorizationVerified              bool
	PromptLoginRequested               bool
	AuthorizationVerifiedAt            time.Time
}

// SourceIDAdmissionDecision defines source idadmission decision state.
type SourceIDAdmissionDecision struct {
	Outcome               SourceIDAdmissionOutcome
	Source, ContractRef   string
	CheckedAt, ValidUntil time.Time
	RecheckSupported      bool
}

// SourceIDPilotAccount defines source idpilot account state.
// Protected, independently verified account ownership. Never serialize/log.
type SourceIDPilotAccount struct {
	Email      string `json:"-"`
	EmployeeNo string `json:"-"`
}

// SourceIDLogout persists a tombstone, including when logout precedes callback.
type SourceIDLogout struct {
	Key       string `gorm:"primaryKey"`
	CreatedAt time.Time
}

// OIDCTransaction defines oidctransaction state.
type OIDCTransaction struct {
	Key       string `gorm:"primaryKey"`
	Nonce     string
	Verifier  string
	Result    string
	ExpiresAt time.Time
	Consumed  bool
}

// TableName returns the authorization transaction table.
func (OIDCTransaction) TableName() string { return "oidc_transactions" }
