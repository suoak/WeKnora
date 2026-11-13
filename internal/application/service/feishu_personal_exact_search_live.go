package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// A retained work receipt is not a live searchable document. In particular,
// deleted imports must not send ordinary IM searches into the personal viewer
// authorization path. This query selects routing metadata only; ordinary and
// exact document authorization still run in their respective search pipelines.
func (p *PersonalRecovery) liveExactSearchReceipts(
	ctx context.Context, kbIDs, knowledgeIDs []string,
) *gorm.DB {
	query := p.service.db.WithContext(ctx).Table("task_pending_ops AS receipts").
		Joins(`JOIN knowledges live ON live.id=receipts.scope_id
AND live.tenant_id=receipts.tenant_id AND live.deleted_at IS NULL AND live.parse_status<>?`,
			types.ParseStatusDeleting).
		Where(`receipts.scope=? AND receipts.payload->>'knowledge_base_id'=live.knowledge_base_id
AND live.knowledge_base_id IN ?`, types.PersonalExactReceiptScope, kbIDs)
	if len(knowledgeIDs) > 0 {
		query = query.Where("live.id IN ?", knowledgeIDs)
	}
	return query
}
