package service

import (
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The binding transaction already proves a completed, authorized migration.
// An active source with its verified published version can continue under live
// OAuth/source permissions and KB write ACLs. Neither temporary migration
// approval nor expiring sync logs are ongoing publication authority. This check
// cannot release hold/paused/revoked or accept an incomplete candidate.
func requireCompletedSourceRebind(tx *gorm.DB, item sourceRebindItem) error {
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
	if len(resources) == 0 {
		// First-create handoff may not have needed a versioned update yet. Use
		// its exact current index receipt, never an arbitrary historic receipt.
		var first types.FeishuPersonalResource
		if err := tx.Where("import_id = ? AND logical_knowledge_id = ?", ds.ID, item.Import.KnowledgeID).
			First(&first).Error; err != nil || first.CurrentAttemptID == "" {
			return ErrFeishuSharingApproval
		}
		var completed int64
		if err := tx.Model(&types.Knowledge{}).Where(
			"id = ? AND tenant_id = ? AND knowledge_base_id = ? AND parse_status = ?",
			item.Import.KnowledgeID, ds.TenantID, ds.KnowledgeBaseID, types.ParseStatusCompleted,
		).Count(&completed).Error; err != nil {
			return err
		}
		if completed != 1 {
			return ErrFeishuSharingApproval
		}
		if err := tx.Model(&types.TaskPendingOp{}).Where(
			"tenant_id = ? AND scope = ? AND scope_id = ? AND dedup_key = ? AND op = ?",
			ds.TenantID, personalIndexReceiptScope, item.Import.KnowledgeID,
			"index-"+first.CurrentAttemptID, "verified",
		).
			Count(&completed).Error; err != nil {
			return err
		}
		if completed != 1 {
			return ErrFeishuSharingApproval
		}
		return nil
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
	return nil
}
