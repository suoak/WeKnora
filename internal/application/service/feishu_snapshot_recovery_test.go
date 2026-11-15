package service

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func snapshotRecoveryItems() (*types.FetchedItem, *types.FetchedItem) {
	old := &types.FetchedItem{
		ExternalID: "document", SourceResourceID: "root", URL: "https://example.invalid/document",
		FileName: "original.docx", Title: "original", Content: []byte("original package"),
		UpdatedAt: time.Date(2026, 11, 15, 1, 0, 0, 0, time.UTC),
		Metadata: map[string]string{
			"obj_type": "docx", "obj_token": "object", "node_token": "node", "space_id": "space",
		},
		ReplacesSubtree: true,
		CurrentSnapshot: func(context.Context) (*types.FetchedItem, error) { return nil, nil },
	}
	fresh := *old
	fresh.CurrentSnapshot = nil
	fresh.Metadata = maps.Clone(old.Metadata)
	fresh.SubtreeKeep = append([]string(nil), old.SubtreeKeep...)
	fresh.UpdatedAt = old.UpdatedAt.Add(time.Second)
	fresh.Content, fresh.Title, fresh.FileName = []byte("current package"), "renamed", "renamed.docx"
	return old, &fresh
}

func snapshotChanged(current *types.FetchedItem) error {
	return &feishuSnapshotChangedError{
		cause: &repository.LegacyFenceError{Reason: "latest_source_snapshot_changed"}, current: current,
	}
}

func recoveryReceipt() *types.FeishuLegacyReceipt {
	return &types.FeishuLegacyReceipt{
		ResourceID: "resource", AttemptID: "attempt", KnowledgeID: "knowledge", ManifestSHA256: "manifest",
		Generation: 1, Chunks: 1, Vectors: 1, BM25Rows: 1,
	}
}

func TestFeishuSnapshotRecoveryRevalidatesFreshCandidate(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		old, fresh := snapshotRecoveryItems()
		calls := 0
		r, err := recoverFeishuSnapshot(t.Context(), old, func(
			candidate *types.FetchedItem,
		) (*types.FeishuLegacyReceipt, error) {
			calls++
			if calls == 1 {
				return nil, snapshotChanged(fresh)
			}
			require.Equal(t, fresh.Content, candidate.Content)
			require.NotNil(t, candidate.CurrentSnapshot)
			require.False(t, candidate.AutoRecovered)
			if duplicate {
				return recoveryReceipt(), types.NewDuplicateFileError(&types.Knowledge{ID: "knowledge"})
			}
			return recoveryReceipt(), nil
		})
		if !duplicate {
			require.NoError(t, err)
		}
		require.NotNil(t, r)
		require.Equal(t, 2, calls)
		require.True(t, old.AutoRecovered)
		require.Equal(t, fresh.Content, old.Content)
		require.Equal(t, fresh.UpdatedAt, old.UpdatedAt)
		require.Equal(t, fresh.Title, old.Title)
	}
}

func TestFeishuSnapshotRecoveryPreservesBinding(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	for _, fault := range []string{
		"external", "root", "url", "obj_type", "obj_token", "node_token", "space_id",
		"deleted", "attachment", "children", "existing_children", "subtree", "revision",
		"empty", "file_type", "callback",
	} {
		t.Run(fault, func(t *testing.T) {
			old, fresh := snapshotRecoveryItems()
			switch fault {
			case "external":
				fresh.ExternalID = "another"
			case "root":
				fresh.SourceResourceID = "another"
			case "url":
				fresh.URL = "https://example.invalid/another"
			case "obj_type", "obj_token", "node_token", "space_id":
				fresh.Metadata[fault] = "another"
			case "deleted":
				fresh.IsDeleted = true
			case "attachment":
				fresh.Metadata["attachment"] = "true"
			case "children":
				fresh.SubtreeKeep = []string{"document#attachment"}
			case "existing_children":
				old.SubtreeKeep = []string{"document#attachment"}
				fresh.SubtreeKeep = old.SubtreeKeep
			case "subtree":
				fresh.ReplacesSubtree = false
			case "revision":
				fresh.UpdatedAt = old.UpdatedAt.Add(-time.Second)
			case "empty":
				fresh.Content = nil
			case "file_type":
				fresh.FileName = "different.pdf"
			case "callback":
				old.CurrentSnapshot = nil
			}
			calls := 0
			_, err := recoverFeishuSnapshot(t.Context(), old,
				func(*types.FetchedItem) (*types.FeishuLegacyReceipt, error) {
					calls++
					return nil, snapshotChanged(fresh)
				})
			require.ErrorIs(t, err, repository.ErrLegacyVersionFence)
			require.Equal(t, 1, calls)
			require.False(t, old.AutoRecovered)
			require.Equal(t, "original package", string(old.Content))
			code := feishuDocumentFailureCode(&types.DataSource{Type: "feishu"}, old, err)
			if fault == "children" || fault == "existing_children" || fault == "subtree" {
				require.Equal(t, "source_snapshot_changed", code)
			} else {
				require.Empty(t, code, "a changed identity or denied observation cannot become a local content retry")
			}
		})
	}
}

func TestFeishuSnapshotRecoveryIsBoundedAndRequiresReceipt(t *testing.T) {
	for _, fault := range []string{"unstable", "storage", "unconfirmed", "canceled", "lookalike"} {
		t.Run(fault, func(t *testing.T) {
			old, fresh := snapshotRecoveryItems()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			_, err := recoverFeishuSnapshot(ctx, old, func(*types.FetchedItem) (*types.FeishuLegacyReceipt, error) {
				calls++
				if calls == 1 {
					if fault == "canceled" {
						cancel()
					}
					if fault == "lookalike" {
						return nil, &repository.LegacyFenceError{Reason: "latest_source_snapshot_changed"}
					}
					return nil, snapshotChanged(fresh)
				}
				if fault == "unconfirmed" {
					return &types.FeishuLegacyReceipt{}, nil
				}
				if fault == "storage" {
					return nil, errors.New("durable write failed")
				}
				return nil, snapshotChanged(fresh)
			})
			if fault != "unconfirmed" {
				require.Error(t, err)
			}
			if fault == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
			expected := 2
			if fault == "canceled" || fault == "lookalike" {
				expected = 1
			}
			require.Equal(t, expected, calls)
			require.False(t, old.AutoRecovered)
			require.Equal(t, "original package", string(old.Content))
		})
	}
}

type snapshotRecoveryWriter struct {
	interfaces.KnowledgeService
	unconfirmed bool
}

func (w *snapshotRecoveryWriter) StageFeishuLegacyItem(ctx context.Context, _ *types.DataSource,
	item *types.FetchedItem, _ []string,
) (*types.FeishuLegacyReceipt, error) {
	_, fresh := snapshotRecoveryItems()
	calls := 0
	return recoverFeishuSnapshot(ctx, item, func(*types.FetchedItem) (*types.FeishuLegacyReceipt, error) {
		calls++
		if calls == 1 {
			return nil, snapshotChanged(fresh)
		}
		if w.unconfirmed {
			return &types.FeishuLegacyReceipt{}, nil
		}
		return recoveryReceipt(), nil
	})
}

func TestFeishuSnapshotRecoveryCountsOnlyAcceptedDocuments(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	for _, unconfirmed := range []bool{false, true} {
		ctx := types.WithLegacyReceiptSet(t.Context())
		writer := &snapshotRecoveryWriter{unconfirmed: unconfirmed}
		h := newStreamHandler(&DataSourceService{knowledgeService: writer},
			&types.DataSource{ID: "source", Type: "feishu"}, &types.SyncResult{}, &types.SyncLog{})
		item, _ := snapshotRecoveryItems()
		err := h.Emit(ctx, *item)
		if unconfirmed {
			require.Error(t, err)
			require.Zero(t, h.result.AutoRecovered)
			require.Empty(t, types.LegacyReceipts(ctx))
		} else {
			require.NoError(t, err)
			require.Equal(t, 1, h.result.AutoRecovered)
			require.Len(t, types.LegacyReceipts(ctx), 1)
		}
	}
}
