package repository

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// ListKnowledgePageIdentities selects only IDs. Content is fetched separately
// after each exact document obtains its own live read proof; no multi-file
// source capability or body-hook bypass is introduced.
func (r *knowledgeRepository) ListKnowledgePageIdentities(ctx context.Context,
	tenant uint64, kb string, page *types.Pagination, filter types.KnowledgeListFilter,
) ([]string, int64, error) {
	if page == nil {
		return nil, 0, access.ErrForbidden
	}
	if err := types.CheckPrivateKBRead(r.db.WithContext(ctx), "", kb); err != nil {
		return nil, 0, err
	}
	allowed, protected, err := access.GuardDocumentReadQuery(ctx, tenant, kb)
	if err != nil {
		return nil, 0, err
	}
	scope := func() *gorm.DB {
		q := r.db.WithContext(ctx).Table("knowledges").
			Where("tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL", tenant, kb)
		if protected {
			if len(allowed) == 0 {
				q = q.Where("1=0")
			} else {
				q = q.Where("id IN ?", allowed)
			}
		}
		// A metadata catalog must not become an oracle for protected title text.
		metadataFilter := filter
		metadataFilter.Keyword = ""
		q = applyKnowledgeListFilter(q, metadataFilter)
		if filter.Keyword != "" {
			q = q.Where("LOWER(file_name) LIKE LOWER(?)", "%"+filter.Keyword+"%")
		}
		return q
	}
	var total int64
	if err := scope().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var ids []string
	err = scope().Order(knowledgeListOrderClause(filter)).Order("id ASC").
		Offset(page.Offset()).Limit(page.Limit()).Pluck("id", &ids).Error
	return ids, total, err
}
