package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// SQL excludes foreign bodies before hooks run. Every requested unique ID
// must be returned from the exact worker document, and a failed live boundary
// discards all prior pages. This is not a published serving capability.
func (r *chunkRepository) listExactWorkerChunks(
	ctx context.Context, tenant uint64, knowledge, kb string, ids []string,
) ([]*types.Chunk, error) {
	seen := make(map[string]bool, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			return nil, types.ErrExactDocumentScope
		}
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	var rows []*types.Chunk
	for start := 0; start < len(unique); start += 100 {
		batch := unique[start:min(start+100, len(unique))]
		var page []*types.Chunk
		err := types.ReadExactWorkerPage(ctx, tenant, knowledge, kb, func(current context.Context) error {
			if err := r.db.WithContext(current).
				Where("tenant_id=? AND knowledge_id=? AND knowledge_base_id=? AND id IN ?",
					tenant, knowledge, kb, batch).Find(&page).Error; err != nil {
				return err
			}
			if len(page) != len(batch) {
				return types.ErrExactDocumentScope
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		rows = append(rows, page...)
	}
	return rows, nil
}
