package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type reparseReceiptHandler struct {
	deferralTestHandler
	confirmed bool
	err       error
	observed  []string
}

func (h *reparseReceiptHandler) ConfirmReparsedDocument(
	_ context.Context, root, externalID, version string,
) (bool, error) {
	if root != "root" || externalID != "bad" || version != "new" {
		return false, errors.New("wrong source listing identity")
	}
	return h.confirmed, h.err
}

func (h *reparseReceiptHandler) ObserveDocument(_, _ string, disposition string) {
	h.observed = append(h.observed, disposition)
}

func TestCompletedReparseReceiptAcknowledgesOnlyPublishedCurrentVersion(t *testing.T) {
	for _, current := range []bool{false, true} {
		ops := &deferralTestOps{nodes: []string{"bad"}}
		identity := types.SyncDocumentDeferral{SourceResourceID: "root", ExternalID: "bad"}
		plan := &documentPlan{failed: map[string]failedDocument{identity.Key(): {
			SourceResourceID: "root", ExternalID: "bad", SourceVersion: "new",
			Attempts: maxDocumentFailureAttempts, Code: "source_snapshot_changed",
		}}}
		cursor := plan.cursor(ops.EncodeCursor(map[string]map[string]string{
			"root": {"bad": "old"}, "overlap": {"bad": "old"},
		}, time.Now()))
		h := &reparseReceiptHandler{confirmed: current}
		roots := []string{"root"}
		if current {
			roots = append(roots, "overlap")
		}
		next, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{},
			roots, cursor, h, ops)
		require.NoError(t, err)
		require.Empty(t, ops.fetched, "a receipt must never trigger another export, Chunk or vector generation")
		require.Empty(t, h.items)
		if current {
			require.NotContains(t, next.ConnectorCursor, failedDocumentCursorKey)
			require.Equal(t, "new", ops.DecodeCursorTimes(next.ConnectorCursor)["root"]["bad"])
			require.Equal(t, "new", ops.DecodeCursorTimes(next.ConnectorCursor)["overlap"]["bad"])
			require.Contains(t, h.observed, "unchanged")
			require.NotContains(t, h.observed, "retry_exhausted")
		} else {
			require.Contains(t, next.ConnectorCursor, failedDocumentCursorKey)
			require.Equal(t, "old", ops.DecodeCursorTimes(next.ConnectorCursor)["root"]["bad"])
			require.Contains(t, h.observed, "retry_exhausted")
		}
	}
}

func TestCompletedReparseReceiptFenceFailureDoesNotAcknowledge(t *testing.T) {
	ops := &deferralTestOps{nodes: []string{"bad"}}
	identity := types.SyncDocumentDeferral{SourceResourceID: "root", ExternalID: "bad"}
	plan := &documentPlan{failed: map[string]failedDocument{identity.Key(): {
		SourceResourceID: "root", ExternalID: "bad", SourceVersion: "new",
		Attempts: maxDocumentFailureAttempts, Code: "source_snapshot_changed",
	}}}
	cursor := plan.cursor(ops.EncodeCursor(map[string]map[string]string{"root": {"bad": "old"}}, time.Now()))
	h := &reparseReceiptHandler{confirmed: true, err: types.ErrStaleSyncExecution}
	next, err := runSync(context.Background(), &Client{}, &types.DataSourceConfig{},
		[]string{"root"}, cursor, h, ops)
	require.ErrorIs(t, err, types.ErrStaleSyncExecution)
	require.Nil(t, next)
	require.Empty(t, h.checkpoints)
	require.Contains(t, cursor.ConnectorCursor, failedDocumentCursorKey)
}
