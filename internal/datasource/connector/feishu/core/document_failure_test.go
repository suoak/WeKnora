package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type documentFailureHandler struct {
	deferralTestHandler
	failure error
}

func (h *documentFailureHandler) Emit(ctx context.Context, item types.FetchedItem) error {
	if item.ExternalID == "bad" && h.failure != nil {
		return h.failure
	}
	return h.deferralTestHandler.Emit(ctx, item)
}

func TestDocumentFailureContinuesAndRetriesAfterRestart(t *testing.T) {
	ops := &deferralTestOps{nodes: []string{"bad", "good"}}
	h := &documentFailureHandler{failure: fmt.Errorf("wrapped: %w", &datasource.DocumentFailureError{
		Code: "source_revision_content_conflict", Cause: errors.New("old version retained"),
	})}
	initial := ops.EncodeCursor(map[string]map[string]string{"root": {"bad": "old"}}, time.Now())
	next, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{}, []string{"root"}, initial, h, ops)
	require.NoError(t, err)
	require.Len(t, h.items, 1)
	require.Equal(t, "good", h.items[0].ExternalID)
	require.Equal(t, "old", ops.DecodeCursorTimes(next.ConnectorCursor)["root"]["bad"])
	require.Equal(t, "new", ops.DecodeCursorTimes(next.ConnectorCursor)["root"]["good"])
	require.Contains(t, next.ConnectorCursor, failedDocumentCursorKey)
	require.NotContains(t, next.ConnectorCursor, pendingDocumentCursorKey)
	require.NotEmpty(t, h.checkpoints, "failure must be durably recorded before later documents")
	// Even when the source timestamp equals the accepted one, pending failures force a read.
	times := ops.DecodeCursorTimes(next.ConnectorCursor)
	times["root"]["bad"] = "new"
	next.ConnectorCursor["space_node_times"] = times
	encoded, err := json.Marshal(next)
	require.NoError(t, err)
	var restarted types.SyncCursor
	require.NoError(t, json.Unmarshal(encoded, &restarted))
	ops2 := &deferralTestOps{nodes: []string{"bad", "good"}}
	h2 := &documentFailureHandler{}
	final, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{}, []string{"root"}, &restarted, h2, ops2)
	require.NoError(t, err)
	require.Equal(t, []string{"bad"}, ops2.fetched)
	require.NotContains(t, final.ConnectorCursor, failedDocumentCursorKey)
}

func TestDocumentFailureFatalAndCheckpointFences(t *testing.T) {
	for _, kind := range []string{"unknown", "stale", "checkpoint", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			ops := &deferralTestOps{nodes: []string{"bad", "good"}}
			h := &documentFailureHandler{failure: &datasource.DocumentFailureError{Code: "source_revision_content_conflict"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "unknown":
				h.failure = errors.New("unknown failure")
			case "stale":
				h.failure = types.ErrStaleSyncExecution
			case "checkpoint":
				h.checkpointErr = types.ErrStaleSyncExecution
			case "canceled":
				cancel()
			}
			next, err := runSync(ctx, &Client{}, &types.DataSourceConfig{}, []string{"root"}, nil, h, ops)
			require.Error(t, err)
			require.Nil(t, next)
			require.NotContains(t, ops.fetched, "good")
		})
	}
}

func TestDocumentFailureNewItemDoesNotAdvanceCursor(t *testing.T) {
	ops := &deferralTestOps{nodes: []string{"bad", "good"}}
	h := &documentFailureHandler{failure: &datasource.DocumentFailureError{Code: "safe_staging_versioned_image_staging_required"}}
	next, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{}, []string{"root"}, nil, h, ops)
	require.NoError(t, err)
	require.NotContains(t, ops.DecodeCursorTimes(next.ConnectorCursor)["root"], "bad")
	require.Contains(t, next.ConnectorCursor, failedDocumentCursorKey)
}
