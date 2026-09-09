package types

import "time"

type PortalStatus string

const (
	PortalStatusDraft     PortalStatus = "draft"
	PortalStatusPublished PortalStatus = "published"
	PortalStatusArchived  PortalStatus = "archived"
)

func (s PortalStatus) IsValid() bool {
	return s == PortalStatusDraft || s == PortalStatusPublished || s == PortalStatusArchived
}

var BuiltinPortalStages = []string{
	"concept_market", "concept_product", "architecture", "design",
	"development", "testing", "lmt",
}

func IsBuiltinPortalStage(value string) bool {
	for _, stage := range BuiltinPortalStages {
		if value == stage {
			return true
		}
	}
	return false
}

type TenantPortalConfig struct {
	TenantID                  uint64       `json:"-" gorm:"primaryKey"`
	Status                    PortalStatus `json:"-" gorm:"type:varchar(16);not null;default:'draft'"`
	DisplayName               string       `json:"-" gorm:"type:varchar(128);not null"`
	Description               string       `json:"-" gorm:"type:text"`
	Category                  string       `json:"-" gorm:"type:varchar(64)"`
	ResponsibleTeam           string       `json:"-" gorm:"type:varchar(128)"`
	Contact                   string       `json:"-" gorm:"type:varchar(256)"`
	Featured                  bool         `json:"-" gorm:"not null;default:false"`
	DisplayOrder              int          `json:"-" gorm:"not null;default:0"`
	AllowAccessRequest        bool         `json:"-" gorm:"not null;default:false"`
	InteractionOrganizationID *string      `json:"-" gorm:"type:varchar(36)"`
	CreatedBy                 string       `json:"-" gorm:"type:varchar(36);not null"`
	UpdatedBy                 string       `json:"-" gorm:"type:varchar(36);not null"`
	PublishedAt               *time.Time   `json:"-"`
	CreatedAt                 time.Time    `json:"-"`
	UpdatedAt                 time.Time    `json:"-"`
}

func (TenantPortalConfig) TableName() string { return "tenant_portal_configs" }

type TenantPortalStage struct {
	TenantID     uint64    `json:"-" gorm:"primaryKey"`
	StageKey     string    `json:"-" gorm:"primaryKey;type:varchar(64)"`
	DisplayOrder int       `json:"-" gorm:"not null;default:0"`
	CreatedAt    time.Time `json:"-"`
}

func (TenantPortalStage) TableName() string { return "tenant_portal_stages" }

type TenantAccessRequestStatus string

const (
	TenantAccessRequestPending   TenantAccessRequestStatus = "pending"
	TenantAccessRequestApproved  TenantAccessRequestStatus = "approved"
	TenantAccessRequestRejected  TenantAccessRequestStatus = "rejected"
	TenantAccessRequestCancelled TenantAccessRequestStatus = "cancelled"
)

type TenantAccessRequest struct {
	ID              string                    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64                    `json:"tenant_id" gorm:"not null;index"`
	ApplicantUserID string                    `json:"applicant_user_id" gorm:"type:varchar(36);not null;index"`
	Source          string                    `json:"source" gorm:"type:varchar(32);not null;default:'portal'"`
	Status          TenantAccessRequestStatus `json:"status" gorm:"type:varchar(16);not null;default:'pending';index"`
	Reason          string                    `json:"reason" gorm:"type:text;not null"`
	RequestedRole   TenantRole                `json:"requested_role" gorm:"type:varchar(20);not null;default:'viewer'"`
	ReviewedBy      *string                   `json:"reviewed_by,omitempty" gorm:"type:varchar(36)"`
	ReviewedAt      *time.Time                `json:"reviewed_at,omitempty"`
	ReviewNote      string                    `json:"review_note,omitempty" gorm:"type:text"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
}

func (TenantAccessRequest) TableName() string { return "tenant_access_requests" }

type PortalAccessState string

const (
	PortalAccessMember    PortalAccessState = "member"
	PortalAccessSuspended PortalAccessState = "suspended"
	PortalAccessPending   PortalAccessState = "pending"
	PortalAccessNotMember PortalAccessState = "not_member"
)

type PortalInteractionAction string

const (
	PortalInteractionNone  PortalInteractionAction = "none"
	PortalInteractionEnter PortalInteractionAction = "enter"
)

// PortalSpaceResponse is deliberately independent from Tenant and
// TenantPortalConfig. Keep this allow-list projection small.
type PortalSpaceResponse struct {
	TenantID           uint64                  `json:"tenant_id"`
	DisplayName        string                  `json:"display_name"`
	Description        string                  `json:"description"`
	Category           string                  `json:"category"`
	ResponsibleTeam    string                  `json:"responsible_team"`
	Contact            string                  `json:"contact"`
	Stages             []string                `json:"stages"`
	Featured           bool                    `json:"featured"`
	KnowledgeBaseCount int64                   `json:"knowledge_base_count"`
	FileCount          int64                   `json:"file_count"`
	AccessState        PortalAccessState       `json:"access_state"`
	CurrentRole        *TenantRole             `json:"current_role"`
	CanRequestAccess   bool                    `json:"can_request_access"`
	InteractionAction  PortalInteractionAction `json:"interaction_action"`
}

type PortalMySpaceResponse struct {
	TenantID   uint64     `json:"tenant_id"`
	TenantName string     `json:"tenant_name"`
	Role       TenantRole `json:"role"`
}

type PortalStageResponse struct {
	Key          string `json:"key"`
	DisplayOrder int    `json:"display_order"`
}

type PortalAdminSpaceResponse struct {
	TenantID                  uint64       `json:"tenant_id"`
	TenantName                string       `json:"tenant_name"`
	Status                    PortalStatus `json:"status"`
	DisplayName               string       `json:"display_name"`
	Description               string       `json:"description"`
	Category                  string       `json:"category"`
	ResponsibleTeam           string       `json:"responsible_team"`
	Contact                   string       `json:"contact"`
	Stages                    []string     `json:"stages"`
	Featured                  bool         `json:"featured"`
	DisplayOrder              int          `json:"display_order"`
	AllowAccessRequest        bool         `json:"allow_access_request"`
	InteractionOrganizationID *string      `json:"interaction_organization_id,omitempty"`
	PublishedAt               *time.Time   `json:"published_at,omitempty"`
	CreatedAt                 *time.Time   `json:"created_at,omitempty"`
	UpdatedAt                 *time.Time   `json:"updated_at,omitempty"`
}

type PortalOrganizationOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PortalConfigUpdateRequest struct {
	DisplayName               string   `json:"display_name"`
	Description               string   `json:"description"`
	Category                  string   `json:"category"`
	ResponsibleTeam           string   `json:"responsible_team"`
	Contact                   string   `json:"contact"`
	Stages                    []string `json:"stages"`
	Featured                  bool     `json:"featured"`
	DisplayOrder              int      `json:"display_order"`
	AllowAccessRequest        bool     `json:"allow_access_request"`
	InteractionOrganizationID *string  `json:"interaction_organization_id"`
}
