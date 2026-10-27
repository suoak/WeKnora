package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// sourceManagedProgress observes the published version, not the obsolete
// first-create worker. A pending replacement does not retire that version.
// Reading progress never resumes a source or changes an index/publication.
func (s *FeishuPersonalService) sourceManagedProgress(ctx context.Context,
	item *types.FeishuBatchItem, m *types.FeishuPersonalImport,
) error {
	item.Stage = "publication_pending"
	item.IndexVerified = false
	if item.Status == "existing" || item.Status == "succeeded" {
		item.Status = "dispatched"
	}
	var k types.Knowledge
	db := s.db.WithContext(ctx)
	if err := db.Where("id = ? AND knowledge_base_id = ?", m.KnowledgeID, m.KBID).First(&k).Error; err != nil {
		return err
	}
	item.DocumentName, item.ParseStatus = k.Title, k.ParseStatus
	if err := db.Model(&types.Chunk{}).Where("knowledge_id = ?", k.ID).Count(&item.ChunkCount).Error; err != nil {
		return err
	}
	if err := db.Table("embeddings").Where("knowledge_id = ? AND embedding IS NOT NULL", k.ID).
		Count(&item.VectorCount).Error; err != nil {
		return err
	}
	// Join the active publication and its receipt in one database snapshot.
	// Do not read a candidate's manifest or use row counts as a success receipt.
	var rows []struct {
		ID, ActiveAttempt, Status string
		Generation                int64
		Receipt                   json.RawMessage
	}
	if err := db.Table("feishu_legacy_resources AS r").
		Select("r.id, r.active_attempt, a.status, a.generation, a.receipt").
		Joins("LEFT JOIN feishu_legacy_attempts AS a ON a.id = r.active_attempt AND a.resource_id = r.id").
		Where("r.tenant_id = ? AND r.knowledge_base_id = ? AND r.data_source_id = ? AND r.knowledge_id = ?",
			k.TenantID, m.KBID, m.ID, k.ID).Limit(2).Scan(&rows).Error; err != nil {
		return err
	}
	var published time.Time
	if len(rows) == 1 {
		r := rows[0]
		var receipt types.FeishuLegacyReceipt
		if r.Status == "published" && json.Unmarshal(r.Receipt, &receipt) == nil &&
			legacyReceiptConfirmed(&receipt) && receipt.ResourceID == r.ID &&
			receipt.AttemptID == r.ActiveAttempt && receipt.KnowledgeID == k.ID &&
			receipt.Generation == r.Generation && receipt.Dimension > 0 && !receipt.PublishedAt.IsZero() {
			item.IndexVerified, published = true, receipt.PublishedAt
		}
	} else if len(rows) == 0 && m.AttemptID != "" {
		// Immediately after handoff, the first-create receipt is still the
		// publication authority. Never fall back to it after a source has a
		// versioned resource record with a missing/invalid active receipt.
		var receipts []types.TaskPendingOp
		if err := db.Where("tenant_id = ? AND scope = ? AND scope_id = ? AND dedup_key = ? AND op = ?",
			k.TenantID, personalIndexReceiptScope, k.ID, "index-"+m.AttemptID, "verified").
			Limit(1).Find(&receipts).Error; err != nil {
			return err
		}
		if len(receipts) == 1 {
			item.IndexVerified, published = true, receipts[0].EnqueuedAt
		}
	}
	if item.IndexVerified && k.ParseStatus == types.ParseStatusCompleted &&
		item.Status != "failed" && item.Status != "blocked" {
		item.Status, item.Stage, item.ErrorCategory = "succeeded", "completed", ""
		item.LastProgressAt = &published
	}
	return nil
}
