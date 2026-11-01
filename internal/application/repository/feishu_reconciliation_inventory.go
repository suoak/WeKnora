package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// A completed remote traversal is insufficient when historical public rows have
// no current-run receipt. Never clear a KB-wide hold while silently abandoning
// old, untracked Feishu documents or documents with unprovable source ownership.
func validateSourceReconciliationInventory(tx *gorm.DB, ctx context.Context, e types.SyncExecution) error {
	var resourceIDs []string
	for _, receipt := range types.LegacyReceipts(ctx) {
		resourceIDs = append(resourceIDs, receipt.ResourceID)
	}
	var missing int64
	err := tx.Raw(`SELECT count(*) FROM knowledges k
 WHERE k.tenant_id=? AND k.knowledge_base_id=(SELECT knowledge_base_id FROM data_sources WHERE id=?)
 AND k.deleted_at IS NULL AND (
  (k.metadata->>'datasource_id'=? AND NOT EXISTS (
    SELECT 1 FROM feishu_legacy_resources r WHERE r.knowledge_id=k.id
    AND r.tenant_id=k.tenant_id AND r.knowledge_base_id=k.knowledge_base_id
    AND r.data_source_id=? AND r.state='active' AND r.id IN ?))
  OR (k.channel IN ('feishu','feishu_drive','lark','lark_drive')
    AND NOT EXISTS (
     SELECT 1 FROM feishu_legacy_resources r WHERE r.knowledge_id=k.id
     AND r.tenant_id=k.tenant_id AND r.knowledge_base_id=k.knowledge_base_id)))`,
		e.TenantID, e.DataSourceID, e.DataSourceID, e.DataSourceID, resourceIDs).Scan(&missing).Error
	if err != nil {
		return err
	}
	if missing != 0 {
		return legacyFence("source_reconciliation_inventory_incomplete")
	}
	return nil
}
