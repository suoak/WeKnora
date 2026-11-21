package repository

import (
	"context"
	"math"

	"github.com/Tencent/WeKnora/internal/application/access"
)

// PortalSearchWorkHints estimates scheduling work from documents already
// authorized by the resolver. Hints never select resources or grant access,
// never leave the coordinator, and fall back to directory order on errors.
func (r *EnterpriseAccessRepository) PortalSearchWorkHints(
	ctx context.Context, targets []*access.PortalKBTarget,
) []int64 {
	type owner struct {
		index  int
		tenant uint64
		kb     string
	}
	allowed := make(map[string]owner)
	var ids []string
	for i, target := range targets {
		if target == nil {
			return nil
		}
		d := target.Descriptor()
		for _, id := range target.DocumentIDs() {
			if _, exists := allowed[id]; exists {
				return nil
			}
			allowed[id] = owner{i, d.TenantID, d.ID}
			ids = append(ids, id)
		}
	}
	hints := make([]int64, len(targets))
	for start := 0; start < len(ids); start += 1024 {
		end := min(start+1024, len(ids))
		var rows []struct {
			ID, KnowledgeBaseID string
			TenantID            uint64
			StorageSize         int64
		}
		if err := r.db.WithContext(ctx).Table("knowledges").
			Select("id,tenant_id,knowledge_base_id,storage_size").
			Where("id IN ? AND deleted_at IS NULL", ids[start:end]).Scan(&rows).Error; err != nil {
			return nil
		}
		for _, row := range rows {
			o, ok := allowed[row.ID]
			if !ok || row.TenantID != o.tenant || row.KnowledgeBaseID != o.kb || row.StorageSize <= 0 {
				continue
			}
			if hints[o.index] > math.MaxInt64-row.StorageSize {
				hints[o.index] = math.MaxInt64
			} else {
				hints[o.index] += row.StorageSize
			}
		}
	}
	return hints
}
