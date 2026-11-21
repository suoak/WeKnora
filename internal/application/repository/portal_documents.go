package repository

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm/clause"
	"strings"
)

// SearchPortalDocumentMetadata reads only metadata from an explicit authorized
// ID set. The coordinator verifies each selected document again before output.
func (r *knowledgeRepository) SearchPortalDocumentMetadata(ctx context.Context, tenant uint64, kb string, ids []string, query string, limit int) ([]*types.Knowledge, error) {
	rows := []*types.Knowledge{}
	if len(ids) == 0 {
		return rows, nil
	}
	literal := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(query))
	err := r.db.WithContext(ctx).Model(&types.Knowledge{}).
		Select("id,tenant_id,knowledge_base_id,title,file_name,parse_status,updated_at").
		Where("tenant_id=? AND knowledge_base_id=? AND id IN ?", tenant, kb, ids).
		Where("(LOWER(title) LIKE ? ESCAPE '!' OR LOWER(file_name) LIKE ? ESCAPE '!')", "%"+literal+"%", "%"+literal+"%").
		Clauses(clause.OrderBy{Expression: clause.Expr{SQL: "CASE WHEN LOWER(title)=? OR LOWER(file_name)=? THEN 0 WHEN LOWER(title) LIKE ? ESCAPE '!' OR LOWER(file_name) LIKE ? ESCAPE '!' THEN 1 ELSE 2 END,updated_at DESC,id ASC", Vars: []any{strings.ToLower(query), strings.ToLower(query), literal + "%", literal + "%"}}}).
		Order("updated_at DESC,id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}
