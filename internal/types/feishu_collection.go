package types

import "time"

// FeishuCollection owns bounded discovery of one explicitly selected tree.
// Content updates remain owned by individual safe datasource executions.
type FeishuCollection struct {
	ID            string     `gorm:"primaryKey" json:"id"`
	OwnerID       string     `                  json:"-"`
	TenantID      uint64     `                  json:"-"`
	KBID          string     `                  json:"kb_id"`
	AuthMode      string     `                  json:"auth_mode"`
	ConnectionID  string     `                  json:"-"`
	ConfigVersion string     `                  json:"-"`
	Kind          string     `                  json:"kind"`
	RootID        string     `                  json:"root_id"`
	SpaceID       string     `                  json:"space_id"`
	State         string     `                  json:"state"`
	Frontier      string     `                  json:"-"`
	Cycle         int64      `                  json:"cycle"`
	Pages         int        `                  json:"pages"`
	Discovered    int        `                  json:"discovered"`
	Truncated     bool       `                  json:"truncated"`
	ErrorCategory string     `                  json:"error_category,omitempty"`
	LeaseOwner    string     `                  json:"-"`
	LeaseUntil    *time.Time `                  json:"-"`
	NextRunAt     time.Time  `                  json:"next_run_at"`
	CreatedAt     time.Time  `                  json:"created_at"`
	UpdatedAt     time.Time  `                  json:"updated_at"`
}

// TableName returns the collection discovery table.
func (FeishuCollection) TableName() string { return "feishu_collections" }

// FeishuCollectionPage retains each page's admitted batch and directory paths.
type FeishuCollectionPage struct {
	Links        []FeishuDiscoveredLink `gorm:"-" json:"links"`
	ID           string                 `gorm:"primaryKey" json:"id"`
	CollectionID string                 `                  json:"collection_id"`
	Cycle        int64                  `                  json:"cycle"`
	Ordinal      int                    `                  json:"ordinal"`
	BatchID      *string                `                  json:"batch_id,omitempty"`
	Provenance   string                 `                  json:"-"`
	CreatedAt    time.Time              `                  json:"created_at"`
}

// FeishuDiscoveredLink carries directory provenance, never credentials.
type FeishuDiscoveredLink struct {
	URL  string   `json:"url"`
	Path []string `json:"path"`
}

// TableName returns the retained discovery provenance table.
func (FeishuCollectionPage) TableName() string { return "feishu_collection_pages" }
