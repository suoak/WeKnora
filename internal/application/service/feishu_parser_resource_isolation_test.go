package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser/anydoc"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type resourceIsolationWriter struct {
	interfaces.KnowledgeService
	err   error
	items []string
}

func (w *resourceIsolationWriter) StageFeishuLegacyItem(_ context.Context, _ *types.DataSource,
	item *types.FetchedItem, _ []string,
) (*types.FeishuLegacyReceipt, error) {
	w.items = append(w.items, item.ExternalID)
	if item.ExternalID == "oversized" {
		return nil, w.err
	}
	return &types.FeishuLegacyReceipt{
		Created: true, ResourceID: "healthy-resource", KnowledgeID: "healthy-knowledge", AttemptID: "attempt",
		Generation: 1, ManifestSHA256: "synthetic-complete-manifest", Chunks: 1, Vectors: 1, BM25Rows: 1,
	}, nil
}

func TestFeishuParserResourceLimitDoesNotPoisonLaterDocument(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	ctx := types.WithLegacyReceiptSet(t.Context())
	writer := &resourceIsolationWriter{err: anydoc.ErrResourceLimit}
	h := newStreamHandler(&DataSourceService{knowledgeService: writer},
		&types.DataSource{ID: "source", Type: "feishu_drive"}, &types.SyncResult{}, &types.SyncLog{})
	err := h.Emit(ctx, types.FetchedItem{ExternalID: "oversized", Content: []byte("synthetic")})
	var local *datasource.DocumentFailureError
	require.ErrorAs(t, err, &local)
	require.Equal(t, "safe_staging_parser_resource_limit", local.Code)
	require.ErrorIs(t, err, anydoc.ErrResourceLimit)
	require.Equal(t, local.Code, h.result.Errors[0].Code)
	require.Contains(t, h.result.Errors[0].Message, "split or simplify")
	require.NoError(t, h.Emit(ctx, types.FetchedItem{ExternalID: "healthy", Content: []byte("complete source")}))
	require.Equal(t, []string{"oversized", "healthy"}, writer.items)
	require.Equal(t, 1, h.result.Failed)
	require.Equal(t, 1, h.result.Created)
	require.Nil(t, h.ingestErr)
	receipts := types.LegacyReceipts(ctx)
	require.Len(t, receipts, 1)
	require.Equal(t, "healthy-resource", receipts[0].ResourceID)
}

func TestFeishuParserUnknownRefusalStillStopsLaterDocument(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	writer := &resourceIsolationWriter{err: errors.New(anydoc.ErrResourceLimit.Error())}
	h := newStreamHandler(&DataSourceService{knowledgeService: writer},
		&types.DataSource{ID: "source", Type: "feishu_drive"}, &types.SyncResult{}, &types.SyncLog{})
	ctx := types.WithLegacyReceiptSet(t.Context())
	require.Error(t, h.Emit(ctx, types.FetchedItem{ExternalID: "oversized", Content: []byte("synthetic")}))
	require.Error(t, h.Emit(ctx, types.FetchedItem{ExternalID: "healthy", Content: []byte("complete source")}))
	require.Equal(t, []string{"oversized"}, writer.items)
	require.Empty(t, types.LegacyReceipts(ctx))
}
