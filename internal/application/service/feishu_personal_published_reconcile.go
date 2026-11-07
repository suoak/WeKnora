package service

import (
	"context"
	"encoding/json"

	"github.com/Tencent/WeKnora/internal/types"
)

// A consumed publication is never invalidated as an unfinished source task.
// Source reauthorization still applies to future work, independently of reads.
func (p *PersonalRecovery) reconcileExactPublishedSnapshot(
	ctx context.Context,
	m *types.FeishuPersonalImport,
	a *types.FeishuPersonalAttempt,
) (bool, error) {
	if a.Status != "indexed" {
		return false, nil
	}
	var rows []types.TaskPendingOp
	if err := p.service.db.WithContext(ctx).
		Where("scope=? AND scope_id=?", types.PersonalExactReceiptScope, a.LogicalKnowledgeID).
		Limit(2).Find(&rows).Error; err != nil {
		return true, err
	}
	if len(rows) == 0 {
		return false, nil // Older non-exact protocol keeps its existing recovery.
	}
	var receipt types.PersonalExactReceipt
	if len(rows) != 1 || rows[0].Op != "published" || rows[0].TenantID != a.CallerTenantID ||
		json.Unmarshal(rows[0].Payload, &receipt) != nil || receipt.AttemptID != a.ID || receipt.ImportID != m.ID {
		return true, errPersonalExactDocument
	}
	if _, err := p.prepareExactPublishedSnapshot(ctx, receipt); err != nil {
		return true, err
	}
	// This is background source maintenance, never a document read. Do not
	// change the published execution, version, chunks, vectors or receipt.
	if _, err := p.authorize(ctx, m, a); err != nil {
		if personalAuthorizationLost(err) {
			update := p.service.db.WithContext(ctx).Model(&types.FeishuPersonalImport{}).
				Where("id=? AND status<>'source_managed' AND EXISTS "+
					"(SELECT 1 FROM feishu_personal_resources r JOIN feishu_personal_attempts a "+
					"ON a.id=r.current_attempt_id WHERE r.import_id=feishu_personal_imports.id "+
					"AND r.generation=? AND r.active_version_id=? AND a.id=? AND a.status='indexed' "+
					"AND a.owner_nonce='' AND a.lease_until IS NULL)",
					m.ID, receipt.Generation, receipt.CandidateVersionID, a.ID).
				Update("status", "needs_attention")
			if update.Error != nil {
				return true, update.Error
			}
		}
		return true, err
	}
	return true, nil
}
