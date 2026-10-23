package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestLegacyReceiptParentCountsP1(t *testing.T) {
	valid := types.FeishuLegacyReceipt{
		KnowledgeID: "knowledge", ResourceID: "resource", AttemptID: "attempt",
		Generation: 1, ManifestSHA256: "manifest", Chunks: 190, ParentChunks: 19,
		IndexedChunks: 171, Vectors: 171, BM25Rows: 171,
	}
	require.True(t, legacyReceiptConfirmed(&valid), "19 context parents must not require vectors")
	flat := valid
	flat.Chunks = flat.Vectors
	flat.ParentChunks = 0
	flat.IndexedChunks = 0
	require.True(t, legacyReceiptConfirmed(&flat))
	require.False(t, legacyReceiptConfirmed(nil))
	for _, tc := range []struct {
		name   string
		mutate func(*types.FeishuLegacyReceipt)
	}{
		{"empty chunks", func(r *types.FeishuLegacyReceipt) { r.Chunks = 0 }},
		{"missing vectors", func(r *types.FeishuLegacyReceipt) { r.Vectors = 0; r.BM25Rows = 0 }},
		{"too many vectors", func(r *types.FeishuLegacyReceipt) { r.Vectors = r.Chunks + 1 }},
		{"bm25 mismatch", func(r *types.FeishuLegacyReceipt) { r.BM25Rows-- }},
		{"171 indexed 170 vectors", func(r *types.FeishuLegacyReceipt) { r.Vectors = 170; r.BM25Rows = 170 }},
		{"missing parent", func(r *types.FeishuLegacyReceipt) { r.ParentChunks-- }},
		{"missing indexed count", func(r *types.FeishuLegacyReceipt) { r.IndexedChunks = 0 }},
		{"missing owner", func(r *types.FeishuLegacyReceipt) { r.AttemptID = "" }},
		{"missing generation", func(r *types.FeishuLegacyReceipt) { r.Generation = 0 }},
		{"missing manifest", func(r *types.FeishuLegacyReceipt) { r.ManifestSHA256 = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			tc.mutate(&r)
			require.False(t, legacyReceiptConfirmed(&r))
		})
	}
}
