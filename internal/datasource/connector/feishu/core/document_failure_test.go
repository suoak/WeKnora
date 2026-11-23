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
	for _, code := range []string{
		"source_revision_content_conflict", "safe_staging_parser_semantic_row_too_large",
		"safe_staging_invalid_xlsx_package",
		"source_snapshot_changed",
		"safe_staging_parser_timeout", "safe_staging_parser_busy",
	} {
		t.Run(code, func(t *testing.T) { documentFailureContinuesAndRetries(t, code) })
	}
}

func documentFailureContinuesAndRetries(t *testing.T, code string) {
	t.Helper()
	ops := &deferralTestOps{nodes: []string{"bad", "good"}}
	h := &documentFailureHandler{failure: fmt.Errorf("wrapped: %w", &datasource.DocumentFailureError{
		Code: code, Cause: errors.New("old version retained"),
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
	final, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{},
		[]string{"root"}, &restarted, h2, ops2)
	require.NoError(t, err)
	require.Equal(t, []string{"bad"}, ops2.fetched)
	require.NotContains(t, final.ConnectorCursor, failedDocumentCursorKey)
}

func TestDocumentFailureFatalAndCheckpointFences(t *testing.T) {
	for _, kind := range []string{"unknown", "stale", "checkpoint", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			ops := &deferralTestOps{nodes: []string{"bad", "good"}}
			h := &documentFailureHandler{failure: &datasource.DocumentFailureError{
				Code: "source_revision_content_conflict",
			}}
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
	h := &documentFailureHandler{failure: &datasource.DocumentFailureError{
		Code: "safe_staging_versioned_image_staging_required",
	}}
	next, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{}, []string{"root"}, nil, h, ops)
	require.NoError(t, err)
	require.NotContains(t, ops.DecodeCursorTimes(next.ConnectorCursor)["root"], "bad")
	require.Contains(t, next.ConnectorCursor, failedDocumentCursorKey)
}

type transientDocumentHandler struct {
	deferralTestHandler
	unavailable bool
}

func (h *transientDocumentHandler) Emit(ctx context.Context, item types.FetchedItem) error {
	if item.Metadata["failure_stage"] == "fetch_document" && h.unavailable {
		return &datasource.DocumentFailureError{Code: "feishu_document_fetch_transient"}
	}
	return h.deferralTestHandler.Emit(ctx, item)
}

func TestDocumentFailureTransientFetchDoesNotBlockLaterDocuments(t *testing.T) {
	ops := &deferralTestOps{nodes: []string{"bad", "good"}, fail: "bad"}
	h := &transientDocumentHandler{unavailable: true}
	next, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{}, []string{"root"}, nil, h, ops)
	require.NoError(t, err)
	require.Len(t, h.items, 1)
	require.Equal(t, "good", h.items[0].ExternalID)
	require.NotContains(t, ops.DecodeCursorTimes(next.ConnectorCursor)["root"], "bad")
	require.Contains(t, next.ConnectorCursor, failedDocumentCursorKey)
	require.NotEmpty(t, h.checkpoints)
	resumed := &deferralTestOps{nodes: []string{"bad", "good"}}
	recovered := &transientDocumentHandler{}
	final, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{},
		[]string{"root"}, next, recovered, resumed)
	require.NoError(t, err)
	require.Equal(t, []string{"bad"}, resumed.fetched)
	require.NotContains(t, final.ConnectorCursor, failedDocumentCursorKey)
}

type unlistedDocumentHandler struct {
	transientDocumentHandler
	unresolved []string
}

func (h *unlistedDocumentHandler) ObserveDocument(_ string, id, disposition string) {
	if disposition == "pending_unlisted" {
		h.unresolved = append(h.unresolved, id)
	}
}

func TestDocumentFailureUnlistedRemainsPending(t *testing.T) {
	ctx := context.Background()
	ops := &deferralTestOps{nodes: []string{"bad", "good"}, fail: "bad"}
	first := &unlistedDocumentHandler{transientDocumentHandler: transientDocumentHandler{unavailable: true}}
	pending, err := runSync(ctx, &Client{}, &types.DataSourceConfig{}, []string{"root"}, nil, first, ops)
	require.NoError(t, err)
	nextHandler := &unlistedDocumentHandler{}
	next, err := runSync(ctx, &Client{}, &types.DataSourceConfig{}, []string{"root"}, pending, nextHandler,
		&deferralTestOps{nodes: []string{"good"}})
	require.NoError(t, err)
	require.Equal(t, []string{"bad"}, nextHandler.unresolved)
	require.Contains(t, next.ConnectorCursor, failedDocumentCursorKey)
	// An explicit authorization deferral takes precedence over the prior failure.
	deferred := &unlistedDocumentHandler{}
	deferred.policy = []types.SyncDocumentDeferral{deferralEntry()}
	_, err = runSync(ctx, &Client{}, &types.DataSourceConfig{}, []string{"root"}, next, deferred,
		&deferralTestOps{nodes: []string{"good"}})
	require.NoError(t, err)
	require.Empty(t, deferred.unresolved)
}

func TestDocumentFailureFiniteBudgetSurvivesRestart(t *testing.T) {
	ops := &deferralTestOps{nodes: []string{"bad", "B", "C"}}
	cursor := ops.EncodeCursor(
		map[string]map[string]string{"root": {"bad": "old"}}, time.Now())
	for attempt := 1; attempt <= maxDocumentFailureAttempts+1; attempt++ {
		current := &deferralTestOps{nodes: ops.nodes}
		h := &documentFailureHandler{failure: &datasource.DocumentFailureError{Code: "unsupported_file_type"}}
		next, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{},
			[]string{"root"}, cursor, h, current)
		require.NoError(t, err)
		if attempt == 1 {
			require.Equal(t, []string{"bad", "B", "C"}, current.fetched)
		} else if attempt <= maxDocumentFailureAttempts {
			require.Equal(t, []string{"bad"}, current.fetched)
		} else {
			require.Empty(t, current.fetched)
		}
		require.Equal(t, "old", ops.DecodeCursorTimes(next.ConnectorCursor)["root"]["bad"])
		raw, err := json.Marshal(next)
		require.NoError(t, err)
		cursor = &types.SyncCursor{}
		require.NoError(t, json.Unmarshal(raw, cursor))
	}
	plan, err := prepareDocumentPlan(context.Background(), cursor, []string{"root"}, &deferralTestHandler{})
	require.NoError(t, err)
	require.True(t, plan.exhausted("bad", "new"))
	require.False(t, plan.exhausted("bad", "newer"), "a modified source must become eligible")
}
