package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuFinalSnapshotRecoveryRechecksBeforePublishing(t *testing.T) {
	old, fresh := snapshotRecoveryItems()
	observations, parses, publications := 0, 0, 0
	old.CurrentSnapshot = func(context.Context) (*types.FetchedItem, error) {
		observations++
		return fresh, nil
	}
	receipt, err := recoverFeishuSnapshot(t.Context(), old, func(
		candidate *types.FetchedItem,
	) (*types.FeishuLegacyReceipt, error) {
		parses++
		if err := verifyFinalFeishuSnapshot(t.Context(), candidate); err != nil {
			return nil, err
		}
		publications++
		return recoveryReceipt(), nil
	})
	require.NoError(t, err)
	require.True(t, legacyReceiptConfirmed(receipt))
	require.Equal(t, 2, observations)
	require.Equal(t, 2, parses)
	require.Equal(t, 1, publications, "the rejected original is never published")
	require.True(t, old.AutoRecovered)
	require.Equal(t, fresh.Content, old.Content)
}

func TestFeishuFinalSnapshotContinuedChangesRemainDocumentLocal(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	old, fresh := snapshotRecoveryItems()
	old.ExternalID, fresh.ExternalID = "oversized", "oversized"
	observations, publications := 0, 0
	old.CurrentSnapshot = func(context.Context) (*types.FetchedItem, error) {
		observations++
		current := *fresh
		current.Title += string(rune('a' + observations))
		return &current, nil
	}
	_, err := recoverFeishuSnapshot(t.Context(), old, func(
		candidate *types.FetchedItem,
	) (*types.FeishuLegacyReceipt, error) {
		if err := verifyFinalFeishuSnapshot(t.Context(), candidate); err != nil {
			return nil, err
		}
		publications++
		return recoveryReceipt(), nil
	})
	require.ErrorIs(t, err, repository.ErrLegacyVersionFence)
	require.Equal(t, 2, observations)
	require.Zero(t, publications)
	require.False(t, old.AutoRecovered)
	code := feishuDocumentFailureCode(&types.DataSource{Type: "feishu"}, old, err)
	require.Equal(t, "source_snapshot_changed", code)
	writer := &resourceIsolationWriter{err: err}
	h := newStreamHandler(&DataSourceService{knowledgeService: writer},
		&types.DataSource{ID: "source", Type: "feishu"}, &types.SyncResult{}, &types.SyncLog{})
	ctx := types.WithLegacyReceiptSet(t.Context())
	err = h.Emit(ctx, *old)
	var local *datasource.DocumentFailureError
	require.ErrorAs(t, err, &local)
	require.Equal(t, "source_snapshot_changed", local.Code)
	require.NoError(t, h.Emit(ctx, types.FetchedItem{ExternalID: "healthy", Content: []byte("healthy source")}))
	require.Nil(t, h.ingestErr)
	require.Len(t, types.LegacyReceipts(ctx), 1)
}

func TestFeishuFinalSnapshotDoesNotConvertPermissionOrBindingErrors(t *testing.T) {
	old, fresh := snapshotRecoveryItems()
	denied := errors.New("source permission denied")
	old.CurrentSnapshot = func(context.Context) (*types.FetchedItem, error) { return nil, denied }
	require.ErrorIs(t, verifyFinalFeishuSnapshot(t.Context(), old), denied)
	old.CurrentSnapshot = func(context.Context) (*types.FetchedItem, error) {
		current := *fresh
		current.SourceResourceID = "different-root"
		return &current, nil
	}
	err := verifyFinalFeishuSnapshot(t.Context(), old)
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	require.Empty(t, feishuDocumentFailureCode(&types.DataSource{Type: "feishu"}, old, err))
	old.CurrentSnapshot = nil
	require.ErrorIs(t, verifyFinalFeishuSnapshot(t.Context(), old), ErrFeishuStagingRequired)
}
