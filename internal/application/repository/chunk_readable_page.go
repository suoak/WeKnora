package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// ListReadableTextPage preserves canonical text semantics, including enabled
// chunks of any indexing status. Body selection is bounded before SQL runs.
func (r *chunkRepository) ListReadableTextPage(
	ctx context.Context, tenant uint64, knowledge, kb string, offset, limit int,
) ([]*types.Chunk, int64, error) {
	if tenant == 0 || knowledge == "" || kb == "" || offset < 0 || limit < 1 || limit > 100 {
		return nil, 0, types.ErrExactDocumentScope
	}
	var rows []*types.Chunk
	var total int64
	read := func(current context.Context) error {
		db := r.db.WithContext(current)
		if err := types.CheckPrivateKBRead(db, knowledge, kb); err != nil {
			return err
		}
		if err := types.CheckLegacySourceRead(db, knowledge, kb); err != nil {
			return err
		}
		query := db.Model(&types.Chunk{}).Where(
			"tenant_id=? AND knowledge_id=? AND knowledge_base_id=? AND chunk_type=? AND is_enabled=?",
			tenant, knowledge, kb, types.ChunkTypeText, true,
		)
		if err := query.Count(&total).Error; err != nil {
			return err
		}
		if int64(offset) >= total {
			return nil
		}
		return query.Order("chunk_index ASC, id ASC").Offset(offset).Limit(limit).Find(&rows).Error
	}
	var err error
	if _, boundTenant, boundKnowledge, boundKB, exact := types.ExactPublishedDocumentIdentity(ctx); exact {
		if tenant != boundTenant || knowledge != boundKnowledge || kb != boundKB {
			return nil, 0, types.ErrExactDocumentScope
		}
		err = types.ReadExactPublishedPage(ctx, knowledge, read)
	} else if boundTenant, boundKnowledge, boundKB, exact := types.ExactDocumentIdentity(ctx); exact {
		if tenant != boundTenant || knowledge != boundKnowledge || kb != boundKB {
			return nil, 0, types.ErrExactDocumentScope
		}
		err = types.ReadExactWorkerPage(ctx, tenant, knowledge, kb, read)
	} else {
		err = read(ctx)
	}
	if err != nil {
		return nil, 0, err
	}
	return rows, total, err
}
