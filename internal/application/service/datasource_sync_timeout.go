package service

import (
	"encoding/json"
	"os"
	"time"
)

// dataSourceSyncTimeout keeps ordinary jobs bounded to two hours. An operator
// may allocate a longer, finite budget to a known large source without raising
// shared provider limits or changing any source admission/publication checks.
func dataSourceSyncTimeout(sourceID string) time.Duration {
	const fallback = 2 * time.Hour
	var overrides map[string]string
	if json.Unmarshal([]byte(os.Getenv("DATASOURCE_SYNC_TIMEOUT_OVERRIDES_JSON")), &overrides) != nil {
		return fallback
	}
	duration, err := time.ParseDuration(overrides[sourceID])
	if err != nil || duration < fallback || duration > 24*time.Hour {
		return fallback
	}
	return duration
}
