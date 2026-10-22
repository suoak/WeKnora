package types

import (
	"errors"
	"time"
)

const (
	EnterprisePolicyPending    = "pending_review"
	EnterprisePolicyApproved   = "approved"
	EnterprisePolicyRevoked    = "revoked"
	EnterprisePolicyInherit    = "inherit"
	EnterprisePolicyRestricted = "restricted"
	EnterprisePolicyMCPBlocked = "mcp_blocked"
	EnterprisePolicyDeny       = "deny"
)

var ErrEnterprisePolicyConflict = errors.New("enterprise policy revision conflict")

// EnterprisePolicy contains only trusted policy data, never source metadata.
// A missing record is pending review. Zero values never authorize reading.
type EnterprisePolicy struct {
	State              string
	Policy             string
	ContentClass       string
	SourceACLState     string
	InheritNewOrdinary bool
	Revision           int64
	ProposedBy         string
	ReviewedBy         *string
	ReviewedAt         *time.Time
	UpdatedBy          string
	UpdatedAt          time.Time
	Reason             string
	ResourceVersion    string
}

type EnterprisePolicyTarget struct {
	Kind string
	ID   string
}

// Storage identifies the sole policy record for a persisted resource.
func (t EnterprisePolicyTarget) Storage() (table, column string, ok bool) {
	switch t.Kind {
	case "space":
		return "enterprise_space_policies", "tenant_id", t.ID != ""
	case "kb":
		return "kb_publication_policies", "knowledge_base_id", t.ID != ""
	case "knowledge":
		return "knowledge_access_policies", "knowledge_id", t.ID != ""
	default:
		return "", "", false
	}
}

type EnterprisePolicyChange struct {
	Target             EnterprisePolicyTarget
	Action             string // propose, approve, revoke
	ExpectedRevision   int64
	ActorID            string // Filled by the authorization component, never from HTTP input.
	Policy             string
	ContentClass       string
	SourceACLState     string
	InheritNewOrdinary bool
	Reason             string
}

// EnterpriseCredential is a projection: it never reads APIKey or KeyHash.
type EnterpriseCredential struct {
	ID           uint64
	OwnerUserID  *string
	ScopeType    APIKeyScopeType
	Capabilities StringArray `gorm:"-"`
	RevokedAt    *time.Time
	ExpiresAt    *time.Time
	FullAccess   bool
}

type EnterpriseDocumentState struct {
	ID              string
	TenantID        uint64
	KnowledgeBaseID string
	Type            string
	ParseStatus     string
	EnableStatus    string
	Channel         string
	CurrentVersion  string
	EnterprisePolicy
}

type EnterpriseReadSnapshot struct {
	UserID          string
	UserActive      bool
	Credential      EnterpriseCredential
	TenantID        uint64
	TenantActive    bool
	PortalPublished bool
	KBID            string
	KBType          string
	KBTemporary     bool
	Space           EnterprisePolicy
	KB              EnterprisePolicy
	Documents       []EnterpriseDocumentState
	// Captured before legacy approval-version normalization. Explicit public
	// withdrawal remains a deny even if the resource has since changed.
	PortalPublicBlocked bool
}

// ExplicitPortalPublicBlock is an exception to Published-default public access,
// not an approval requirement. Missing/pending historical review is not a deny.
func ExplicitPortalPublicBlock(p EnterprisePolicy) bool {
	return p.State == EnterprisePolicyRevoked || p.Policy == EnterprisePolicyRestricted || p.Policy == EnterprisePolicyMCPBlocked || p.Policy == EnterprisePolicyDeny || p.ContentClass == "mixed" || p.SourceACLState == "revoked"
}
