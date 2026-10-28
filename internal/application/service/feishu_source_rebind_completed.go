package service

import (
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This is evidence of completed recovery, never permission to release a hold.
// Counts alone, a ready candidate, or a successful first-import attempt cannot
// waive an expired migration window. The active atomic receipt must be valid,
// and a fenced sync must have finished after the binding transaction committed.
func requireCompletedSourceRebind(tx *gorm.DB, item sourceRebindItem, applied time.Time) error {
	var ds types.DataSource
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND tenant_id = ? AND knowledge_base_id = ?",
		item.Source.ID, item.Source.TenantID, item.Source.KnowledgeBaseID).First(&ds).Error; err != nil {
		return err
	}
	if !sourceMayRenewConsent(&ds) {
		return ErrFeishuSharingApproval
	}
	var resources []types.FeishuLegacyResource
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where(
		"data_source_id = ? AND tenant_id = ? AND knowledge_base_id = ? AND knowledge_id = ?",
		ds.ID, ds.TenantID, ds.KnowledgeBaseID, item.Import.KnowledgeID).Limit(2).Find(&resources).Error; err != nil {
		return err
	}
	if len(resources) != 1 || resources[0].State != "active" || resources[0].ActiveAttempt == "" {
		return ErrFeishuSharingApproval
	}
	r := resources[0]
	var attempt types.FeishuLegacyAttempt
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND resource_id = ? AND status = ?",
		r.ActiveAttempt, r.ID, "published").First(&attempt).Error; err != nil {
		return ErrFeishuSharingApproval
	}
	var receipt types.FeishuLegacyReceipt
	if json.Unmarshal(attempt.Receipt, &receipt) != nil || !legacyReceiptConfirmed(&receipt) ||
		receipt.ResourceID != r.ID || receipt.AttemptID != attempt.ID || receipt.KnowledgeID != r.KnowledgeID ||
		receipt.Generation != attempt.Generation || receipt.PublishedAt.IsZero() || receipt.Dimension <= 0 {
		return ErrFeishuSharingApproval
	}
	var completed int64
	if err := tx.Model(&types.SyncLog{}).Where(
		"data_source_id = ? AND tenant_id = ? AND status = ? AND execution_claimed = "+
			"? AND execution_generation > 0 AND finished_at >= ?",
		ds.ID, ds.TenantID, types.SyncLogStatusSuccess, false, applied).Count(&completed).Error; err != nil {
		return err
	}
	if completed == 0 {
		return ErrFeishuSharingApproval
	}
	return nil
}
