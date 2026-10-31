package service

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/types"
)

// This reports execution metadata to the already-authorized import owner. It
// never grants document read authority and never loads body/profile/summary.
// Current source authorization is independently rechecked on every content use.
func (p *PersonalRecovery) decorateExactIndexStatus(
	ctx context.Context, tenant uint64, row *types.FeishuPersonalImport,
) (bool, error) {
	var ops []types.TaskPendingOp
	if err := p.service.db.WithContext(ctx).Where("scope=? AND scope_id=?",
		types.PersonalExactReceiptScope, row.KnowledgeID).Limit(2).Find(&ops).Error; err != nil {
		return false, err
	}
	if len(ops) == 0 {
		return false, nil
	}
	var receipt types.PersonalExactReceipt
	if len(ops) != 1 || ops[0].TenantID != tenant ||
		json.Unmarshal(ops[0].Payload, &receipt) != nil || receipt.ImportID != row.ID ||
		receipt.AttemptID != row.AttemptID || receipt.KnowledgeID != row.KnowledgeID ||
		receipt.KnowledgeBaseID != row.KBID || receipt.TenantID != tenant {
		return true, errPersonalExactDocument
	}
	if row.Status == "needs_attention" {
		row.IndexStatus, row.WikiStatus = "attention", "not_started"
		return true, nil
	}
	if ops[0].Op != "published" {
		// A staging receipt proves ownership, not that parsing or indexing ran.
		row.IndexStatus, row.WikiStatus = "pending", "not_started"
		return true, nil
	}
	if err := p.verifyExactPublishedDatabase(ctx, receipt); err != nil {
		return true, err
	}
	row.IndexStatus = "verified"
	// Exact publication currently supports the ordinary document index only.
	// Do not claim a Wiki pipeline completed without its own receipt.
	var kb struct{ IndexingStrategy types.IndexingStrategy }
	if err := p.service.db.WithContext(ctx).Table("knowledge_bases").
		Select("indexing_strategy").Where("id=? AND tenant_id=? AND deleted_at IS NULL",
		row.KBID, tenant).Take(&kb).Error; err != nil {
		return true, err
	}
	if kb.IndexingStrategy.WikiEnabled {
		row.Status, row.WikiStatus = "indexed", "not_started"
	} else {
		row.Status, row.WikiStatus = "synced", "disabled"
	}
	return true, nil
}
