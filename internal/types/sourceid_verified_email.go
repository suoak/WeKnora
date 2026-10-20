package types

import "time"

// SourceIDLinkDecision defines source idlink decision state.
// Policy decisions are not attestations of historical mailbox ownership.
// Protected internal records; never serialize to public APIs or general logs.
type SourceIDLinkDecision struct {
	ID               string `gorm:"primaryKey"`
	ReviewID         string
	LinkMode         string
	PolicyRef        string
	BindingPolicyRef string
	Decision         string
	CreatedAt        time.Time
}

// SourceIDLocalLinkPolicy defines source idlocal link policy state.
type SourceIDLocalLinkPolicy struct {
	ID               string `gorm:"primaryKey"`
	DecisionID       string
	LinkMode         string
	PolicyRef        string
	BindingPolicyRef string
}

// SourceIDLinkBlock defines source idlink block state.
// A negative exception, not a population allowlist or fabricated ownership.
type SourceIDLinkBlock struct {
	ID         string `gorm:"primaryKey"`
	Issuer     string
	UserID     string
	EmployeeNo string
	Subject    string
	Email      string
	Reason     string
	Actor      string
	Reference  string
	CreatedAt  time.Time
}

// SourceIDVerifiedEmailLogin defines source idverified email login state.
type SourceIDVerifiedEmailLogin struct {
	Issuer, Subject, EmployeeNo, Email string
	PolicyRef, BindingPolicyRef        string
	JITEnabled                         bool
	JITPolicyRef                       string
	JITDomains                         []string
}
