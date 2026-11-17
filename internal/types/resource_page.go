package types

// ResourceListOptions opts into bounded resource listing. Cursor is opaque to callers.
type ResourceListOptions struct {
	ParentID string
	SpaceKey string
	RevealID string
	Cursor   string
	Query    string
	Limit    int
}

type ResourcePage struct {
	Items      []Resource `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}
