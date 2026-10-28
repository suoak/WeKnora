package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// GetKnowledgeDeletionMetadata is for cleanup of already-admitted document IDs.
// Scan deliberately avoids content-read AfterFind hooks. The projection excludes
// descriptions, profiles and custom metadata; no chunk body is read. Callers must
// validate every persisted KB binding before mutating anything. Ordinary read
// methods keep their source restrictions, including for the same context.
func (r *knowledgeRepository) GetKnowledgeDeletionMetadata(
	ctx context.Context, tenantID uint64, ids []string,
) ([]*types.Knowledge, error) {
	if tenantID == 0 {
		return nil, fmt.Errorf("cleanup tenant required")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []*types.Knowledge
	err := r.db.WithContext(ctx).Model(&types.Knowledge{}).
		Select("id, tenant_id, knowledge_base_id, type, title, file_name, file_type, file_path, "+
			"file_hash, storage_size, embedding_model_id, parse_status, updated_at, metadata").
		Where("tenant_id = ? AND id IN ?", tenantID, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := types.CheckPrivateKBRead(r.db.WithContext(ctx), row.ID, row.KnowledgeBaseID); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// MarkKnowledgeDeleting preserves all content and accounting columns. A moved,
// concurrently modified or deleted row must not be resurrected by a retry.
func (r *knowledgeRepository) MarkKnowledgeDeleting(ctx context.Context, knowledge *types.Knowledge) error {
	if knowledge == nil || knowledge.ID == "" || knowledge.TenantID == 0 || knowledge.KnowledgeBaseID == "" {
		return fmt.Errorf("cleanup binding required")
	}
	if err := types.CheckPrivateKBRead(r.db.WithContext(ctx), knowledge.ID, knowledge.KnowledgeBaseID); err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	result := r.db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("id = ? AND tenant_id = ? AND knowledge_base_id = ? AND parse_status = ? AND updated_at = ?",
			knowledge.ID, knowledge.TenantID, knowledge.KnowledgeBaseID, knowledge.ParseStatus, knowledge.UpdatedAt).
		Updates(map[string]any{"parse_status": types.ParseStatusDeleting, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("knowledge changed during deletion")
	}
	knowledge.ParseStatus = types.ParseStatusDeleting
	knowledge.UpdatedAt = now
	return nil
}
