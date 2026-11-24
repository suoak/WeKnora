package datasource

import (
	"encoding/json"
	"os"
	"time"
)

// SyncTaskTimeout is shared by manual, cron and catch-up sync producers.
// Ordinary sources retain two hours; named overrides can allocate a finite
// budget up to 24 hours without changing provider limits or source authority.
func SyncTaskTimeout(sourceID string) time.Duration {
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
