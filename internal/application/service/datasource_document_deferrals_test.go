package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestDocumentDeferralOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result types.SyncResult
		status string
	}{
		{"deferred_only", types.SyncResult{Deferred: 1}, types.SyncLogStatusDeferred},
		{"partial_updated", types.SyncResult{Deferred: 1, Updated: 2}, types.SyncLogStatusPartial},
		{"partial_unchanged", types.SyncResult{Deferred: 1, Unchanged: 2}, types.SyncLogStatusPartial},
		{"partial_unsupported", types.SyncResult{Unsupported: 2}, types.SyncLogStatusPartial},
		{"success", types.SyncResult{Created: 2, Unchanged: 3}, types.SyncLogStatusSuccess},
		{"failed_only", types.SyncResult{Failed: 1}, types.SyncLogStatusFailed},
		{"partial_failure", types.SyncResult{Failed: 1, Unchanged: 2}, types.SyncLogStatusPartial},
	} {
		t.Run(tc.name,
			func(t *testing.T) {
				got,
					_ := streamingSyncOutcome(&tc.result)
				require.Equal(t,
					tc.status,
					got)
			})
	}
}
