package service

import "github.com/Tencent/WeKnora/internal/types"

// legacyReceiptConfirmed accepts the repository's atomic-publication receipt.
// Chunks includes context parents; vectors/BM25 include only retrievable text.
// The repository independently checks the full manifest graph and public rows.
func legacyReceiptConfirmed(receipt *types.FeishuLegacyReceipt) bool {
	if receipt == nil {
		return false
	}
	indexed := receipt.IndexedChunks
	// Historical flat receipts had no explicit graph counts. Their only safe
	// compatible shape is the original one-vector-per-chunk contract.
	if indexed == 0 && receipt.ParentChunks == 0 && receipt.Vectors == receipt.Chunks {
		indexed = receipt.Chunks
	}
	return receipt.KnowledgeID != "" && receipt.ResourceID != "" &&
		receipt.AttemptID != "" && receipt.Generation > 0 && receipt.ManifestSHA256 != "" &&
		receipt.ParentChunks >= 0 && indexed > 0 && receipt.Chunks == receipt.ParentChunks+indexed &&
		receipt.Vectors == indexed && receipt.BM25Rows == indexed
}
