package interfaces

import "context"

// SyncTaskInspector must fail closed on partial scans or backend errors.
// Keys are tenant/datasource/sync-log identities, not task IDs or generations.
type SyncTaskInspector interface {
	LiveSyncTasks(context.Context) (map[string]bool, error)
}
