package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// PersonalReparseMetadata contains identity fields, never document content.
type PersonalReparseMetadata struct {
	ID, KnowledgeBaseID string
	TenantID            uint64
}

// Protocol metadata is deliberately projected without loading Knowledge or
// invoking a body read hook. It remains available when recovery is disabled.
func (r *knowledgeRepository) LoadPersonalReparseMetadata(
	ctx context.Context, id string,
) (*PersonalReparseMetadata, error) {
	var row PersonalReparseMetadata
	err := r.db.WithContext(ctx).Table("knowledges AS k").Select("k.id,k.tenant_id,k.knowledge_base_id").
		Where("k.id=? AND k.deleted_at IS NULL AND (EXISTS("+
			"SELECT 1 FROM task_pending_ops op WHERE op.scope=? AND op.scope_id=k.id "+
			"AND op.tenant_id=k.tenant_id AND op.payload->>'payload_version' IN ('3','5')) "+
			"OR EXISTS(SELECT 1 FROM task_pending_ops receipt WHERE receipt.scope=? AND receipt.scope_id=k.id "+
			"AND receipt.tenant_id=k.tenant_id))", id,
			types.KnowledgeDispatchScope, types.PersonalExactReceiptScope).
		Scan(&row).Error
	return &row, err
}
