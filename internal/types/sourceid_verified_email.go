package types

import "time"

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

type SourceIDLocalLinkPolicy struct {
	ID               string `gorm:"primaryKey"`
	DecisionID       string
	LinkMode         string
	PolicyRef        string
	BindingPolicyRef string
}

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

type SourceIDVerifiedEmailLogin struct {
	Issuer, Subject, EmployeeNo, Email string
	PolicyRef, BindingPolicyRef        string
	JITEnabled                         bool
	JITPolicyRef                       string
	JITDomains                         []string
}
