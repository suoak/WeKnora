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

// ResourcePage returns one bounded resource page and its opaque continuation.
type ResourcePage struct {
	Items      []Resource `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}
