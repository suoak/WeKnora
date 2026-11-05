package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// GetKnowledgeProcessingMetadata resolves ownership and progress without reading
// source content, filenames, locators, summaries, profiles, or custom metadata.
// The route still runs its ordinary KB/private-owner/API-key authorization.
func (r *knowledgeRepository) GetKnowledgeProcessingMetadata(
	ctx context.Context, id string,
) (*types.Knowledge, error) {
	var rows []*types.Knowledge
	err := r.db.WithContext(ctx).Table("knowledges").
		Select(`id,tenant_id,knowledge_base_id,channel,parse_status,summary_status,
pending_subtasks_count,created_at,updated_at`).
		Where("id=? AND deleted_at IS NULL", id).Limit(1).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, ErrKnowledgeNotFound
	}
	row := rows[0]
	if err := types.CheckPrivateKBRead(r.db.WithContext(ctx), row.ID, row.KnowledgeBaseID); err != nil {
		return nil, err
	}
	row.MetadataOnly = true
	return row, nil
}
