package types

import "time"

// FeishuConnection defines feishu connection state.
// Credentials are never serialized through API responses.
type FeishuConnection struct {
	UserID           string    `gorm:"primaryKey" json:"-"`
	AppID            string    `json:"-"`
	OpenID           string    `json:"-"`
	Tokens           string    `json:"-"`
	ExpiresAt        time.Time `json:"-"`
	RefreshExpiresAt time.Time `json:"-"`
	UpdatedAt        time.Time `json:"-"`
}

// FeishuOAuthState defines feishu oauth state state.
type FeishuOAuthState struct {
	Hash          string `gorm:"primaryKey"`
	UserID        string
	ConfigVersion string
	ExpiresAt     time.Time
	Claimed       bool
}

// TableName returns the database table name.
func (FeishuConnection) TableName() string { return "feishu_connections" }

// TableName returns the database table name.
func (FeishuOAuthState) TableName() string { return "feishu_oauth_states" }

// TableName returns the database table name.
func (FeishuPersonalImport) TableName() string { return "feishu_personal_imports" }

// FeishuPersonalImport defines feishu personal import state.
type FeishuPersonalImport struct {
	DataSourceID       string    `gorm:"-" json:"data_source_id,omitempty"`
	SourceStatus       string    `gorm:"-" json:"source_status,omitempty"`
	AuthMode           string    `gorm:"default:user_oauth" json:"auth_mode"`
	ApplicationID      string    `json:"-"`
	IndexStatus        string    `gorm:"-" json:"index_status,omitempty"`
	WikiStatus         string    `gorm:"-" json:"wiki_status,omitempty"`
	ErrorCategory      string    `gorm:"-" json:"error_category,omitempty"`
	LogicalKnowledgeID string    `gorm:"-" json:"logical_knowledge_id,omitempty"`
	AttemptID          string    `gorm:"-" json:"attempt_id,omitempty"`
	ID                 string    `gorm:"primaryKey" json:"id"`
	UserID             string    `json:"imported_by"`
	AppID              string    `json:"-"`
	OpenID             string    `json:"-"`
	KBID               string    `json:"kb_id"`
	DocumentToken      string    `json:"resource_id"`
	ResourceType       string    `json:"resource_type"`
	SourceModified     string    `json:"source_modified"`
	Coverage           string    `json:"coverage"`
	URL                string    `json:"url"`
	KnowledgeID        string    `json:"knowledge_id"`
	Revision           int64     `json:"revision"`
	Status             string    `json:"status"`
	SyncLease          string    `json:"-"`
	UpdatedAt          time.Time `json:"updated_at"`
}
