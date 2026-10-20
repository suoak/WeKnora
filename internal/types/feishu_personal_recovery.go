package types

import "time"

// FeishuPersonalResource defines feishu personal resource state.
// These records are private protocol state, never normal searchable Knowledge.
// ActiveVersionID is changed only by a future receipt-checked activation CAS.
type FeishuPersonalResource struct {
	ImportID           string `gorm:"primaryKey"`
	LogicalKnowledgeID string
	ActiveVersionID    string
	CandidateVersionID string
	CurrentAttemptID   string
	Generation         int64
}

// TableName returns the database table name.
func (FeishuPersonalResource) TableName() string { return "feishu_personal_resources" }

// FeishuPersonalGrantEpoch defines feishu personal grant epoch state.
type FeishuPersonalGrantEpoch struct {
	UserID string `gorm:"primaryKey"`
	Epoch  string
}

// TableName returns the database table name.
func (FeishuPersonalGrantEpoch) TableName() string { return "feishu_personal_grant_epochs" }

// FeishuPersonalAttempt defines feishu personal attempt state.
type FeishuPersonalAttempt struct {
	AuthMode              string `gorm:"default:user_oauth"`
	ApplicationID         string
	ID                    string `gorm:"primaryKey"`
	ImportID              string
	LogicalKnowledgeID    string
	CandidateVersionID    string
	Revision              string
	ExpectedActiveVersion string
	CallerTenantID        uint64
	AppConfigVersion      string
	GrantEpoch            string
	Status                string
	OwnerNonce            string
	Fence                 int64
	LeaseUntil            *time.Time
	RetryAfter            *time.Time
	RetryCount            int
	LastErrorCategory     string
	ManifestKey           string
	ManifestHash          string
	ManifestSize          int64
	ManifestName          string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// TableName returns the database table name.
func (FeishuPersonalAttempt) TableName() string { return "feishu_personal_attempts" }
