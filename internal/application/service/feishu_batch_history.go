package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// Historical admission failures do not describe the result of a later fenced
// recovery. Reuse the detail view's current publication verification, including
// generation and output manifests. Merely finding an op='published' row is not
// enough. This never writes old items or enrolls a synchronization source.
func (s *FeishuPersonalService) exactBatchHistoryStates(
	ctx context.Context, batch types.FeishuBatch, ids []string,
) (map[string]string, error) {
	states := make(map[string]string)
	if len(ids) == 0 || s.recovery == nil || !s.recovery.firstCreateOnly {
		return states, nil
	}
	var rows []types.FeishuPersonalImport
	if err := s.db.WithContext(ctx).Where("id IN ? AND user_id=? AND kb_id=? AND status<>?",
		ids, batch.OwnerID, batch.KBID, "source_managed").
		Where("EXISTS (SELECT 1 FROM task_pending_ops receipt WHERE receipt.scope=?"+
			" AND receipt.scope_id=feishu_personal_imports.knowledge_id)", types.PersonalExactReceiptScope).
		Where("EXISTS (SELECT 1 FROM knowledges k WHERE k.id=feishu_personal_imports.knowledge_id"+
			" AND k.tenant_id=? AND k.knowledge_base_id=? AND k.deleted_at IS NULL)", batch.TenantID, batch.KBID).
		Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return states, nil
	}
	var resources []types.FeishuPersonalResource
	if err := s.db.WithContext(ctx).Where("import_id IN ?", ids).Find(&resources).Error; err != nil {
		return nil, err
	}
	attempts := make(map[string]string, len(resources))
	for _, resource := range resources {
		attempts[resource.ImportID] = resource.CurrentAttemptID
	}
	for i := range rows {
		rows[i].AttemptID = attempts[rows[i].ID]
	}
	if err := s.recovery.firstCreateStatuses(ctx, rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.PublicationStatus != "published" || row.IndexStatus != "verified" {
			continue
		}
		states[row.ID] = "dispatched"
		if row.Status == "synced" || row.Status == "completed" {
			states[row.ID] = "succeeded"
		}
	}
	return states, nil
}
