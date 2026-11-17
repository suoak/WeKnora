package types

// DataSourceSystemAccountInfo deliberately carries no identity or secret.
type DataSourceSystemAccountInfo struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url,omitempty"`
}
