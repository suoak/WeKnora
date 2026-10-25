package types

import "time"

// FeishuBatch binds durable batch admission to one owner, KB and identity.
type FeishuBatch struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	OwnerID        string    `                  json:"-"`
	TenantID       uint64    `                  json:"-"`
	KBID           string    `                  json:"kb_id"`
	AuthMode       string    `                  json:"auth_mode"`
	ConnectionID   string    `                  json:"-"`
	ConfigVersion  string    `                  json:"-"`
	IdempotencyKey string    `                  json:"-"`
	PayloadHash    string    `                  json:"-"`
	CreatedAt      time.Time `                  json:"created_at"`
}

// TableName returns the durable batch control table.
func (FeishuBatch) TableName() string { return "feishu_import_batches" }

// FeishuBatchItem records one independently leased import admission.
type FeishuBatchItem struct {
	ID            string     `gorm:"primaryKey" json:"id"`
	BatchID       string     `                  json:"batch_id"`
	Ordinal       int        `                  json:"ordinal"`
	URL           string     `                  json:"url"`
	Canonical     string     `                  json:"-"`
	Status        string     `                  json:"status"`
	ErrorCategory string     `                  json:"error_category,omitempty"`
	ImportID      string     `                  json:"import_id,omitempty"`
	RetryCount    int        `                  json:"retry_count"`
	LeaseOwner    string     `                  json:"-"`
	LeaseUntil    *time.Time `                  json:"-"`
	UpdatedAt     time.Time  `                  json:"updated_at"`
}

// TableName returns the batch item control table.
func (FeishuBatchItem) TableName() string { return "feishu_import_batch_items" }

// FeishuSharingApproval records intentional publication of a personal resource to one
// KB and one source revision. It is never inferred from write access alone.
type FeishuSharingApproval struct {
	ID            string    `gorm:"primaryKey" json:"id"`
	OwnerID       string    `                  json:"owner_id"`
	KBID          string    `                  json:"kb_id"`
	AppID         string    `                  json:"-"`
	ResourceType  string    `                  json:"resource_type"`
	ResourceToken string    `                  json:"-"`
	Revision      string    `                  json:"revision"`
	ConfigVersion string    `                  json:"-"`
	State         string    `                  json:"state"`
	ApprovedBy    string    `                  json:"approved_by"`
	Reason        string    `                  json:"reason"`
	CreatedAt     time.Time `                  json:"created_at"`
	ExpiresAt     time.Time `                  json:"expires_at"`
}

// TableName returns the retained sharing approval audit table.
func (FeishuSharingApproval) TableName() string { return "feishu_sharing_approvals" }
