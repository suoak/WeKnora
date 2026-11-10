package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
)

// ListKnowledgeForKBClear returns identities for a live, authorized parent's
// cleanup. Content read permission from the remote connector is not required.
func (r *knowledgeRepository) ListKnowledgeForKBClear(ctx context.Context, tenant uint64, kb string) ([]*types.Knowledge, error) {
	if tenant == 0 || kb == "" {
		return nil, errors.New("knowledge base identity required")
	}
	if err := types.CheckPrivateKBRead(r.db.WithContext(ctx), "", kb); err != nil {
		return nil, err
	}
	var parents int64
	if err := r.db.WithContext(ctx).Table("knowledge_bases").Where(
		"id=? AND tenant_id=? AND deleted_at IS NULL", kb, tenant).Count(&parents).Error; err != nil {
		return nil, err
	}
	if parents != 1 {
		return nil, errors.New("knowledge base identity changed")
	}
	var ids []string
	if err := r.db.WithContext(ctx).Table("knowledges").Where(
		"tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL", tenant, kb).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return r.GetKnowledgeDeletionMetadata(ctx, tenant, ids)
}
