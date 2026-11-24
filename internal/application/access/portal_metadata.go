package access

import "time"

// PortalKBMetadata is a projection for an already authorized KB, not a grant.
// Creator IDs, email addresses and model/storage configuration are excluded.
type PortalKBMetadata struct {
	ID            string    `json:"-"`
	Description   string    `json:"description"`
	CreatorName   string    `json:"creator_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Type          string    `json:"type"`
	DocumentCount int       `json:"document_count"`
}
