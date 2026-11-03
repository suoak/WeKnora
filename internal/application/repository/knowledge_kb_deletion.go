package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
)

// ListKnowledgeForKBDeletion supplies only cleanup metadata to the existing
// deletion worker, after proving the parent was deleted in this exact tenant.
// It does not return body, profile, description or any Chunk content.
func (r *knowledgeRepository) ListKnowledgeForKBDeletion(
	ctx context.Context, tenant uint64, kb string,
) ([]*types.Knowledge, error) {
	var deleted int64
	if err := r.db.WithContext(ctx).Table("knowledge_bases").Where(
		"id=? AND tenant_id=? AND deleted_at IS NOT NULL", kb, tenant).Count(&deleted).Error; err != nil {
		return nil, err
	}
	if tenant == 0 || kb == "" || deleted != 1 {
		return nil, errors.New("knowledge base deletion identity not verified")
	}
	var rows []*types.Knowledge
	err := r.db.WithContext(ctx).Table("knowledges").
		Select("id, tenant_id, knowledge_base_id, embedding_model_id, type, file_path, storage_size").
		Where("tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL", tenant, kb).Scan(&rows).Error
	return rows, err
}
