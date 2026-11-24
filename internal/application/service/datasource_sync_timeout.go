package service

import (
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
)

// dataSourceSyncTimeout keeps ordinary jobs bounded to two hours. An operator
// may allocate a longer, finite budget to a known large source without raising
// shared provider limits or changing any source admission/publication checks.
func dataSourceSyncTimeout(sourceID string) time.Duration {
	return datasource.SyncTaskTimeout(sourceID)
}
