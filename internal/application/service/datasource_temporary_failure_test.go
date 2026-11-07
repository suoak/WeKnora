package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestTemporarySyncItemFailureRequiresCompleteKnownInventory(t *testing.T) {
	require.ErrorIs(t, temporarySyncItemFailure(&types.SyncResult{
		Total: 2, Failed: 2, Errors: []types.SyncItemError{
			{Code: "source_verification_unavailable"}, {Code: "embedding_budget_busy"},
		},
	}), feishusafety.ErrSourceValidationUnavailable)
	for _, result := range []*types.SyncResult{
		nil, {},
		{Total: 2, Failed: 1, Errors: []types.SyncItemError{{Code: "source_verification_unavailable"}}},
		{Total: 2, Failed: 2, Errors: []types.SyncItemError{{Code: "source_verification_unavailable"}}},
		{Total: 2, Failed: 2, Errors: []types.SyncItemError{
			{Code: "source_verification_unavailable"}, {Code: "sharing_approval_required"},
		}},
		{Total: 1, Failed: 1, Errors: []types.SyncItemError{{Code: "ingest_failed"}}},
	} {
		require.NoError(t, temporarySyncItemFailure(result))
	}
}
