package repository

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm/clause"
)

// SearchPortalDocumentMetadata reads only metadata from an explicit authorized
// ID set. The coordinator verifies each selected document again before output.
func (r *knowledgeRepository) SearchPortalDocumentMetadata(
	ctx context.Context,
	tenant uint64,
	kb string,
	ids []string,
	query string,
	limit int,
) ([]*types.Knowledge, error) {
	rows := []*types.Knowledge{}
	if len(ids) == 0 {
		return rows, nil
	}
	literal := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(query))
	match := "(LOWER(title) LIKE ? ESCAPE '!' OR LOWER(file_name) LIKE ? ESCAPE '!')"
	ranking := "CASE WHEN LOWER(title)=? OR LOWER(file_name)=? THEN 0 " +
		"WHEN LOWER(title) LIKE ? ESCAPE '!' OR LOWER(file_name) LIKE ? ESCAPE '!' " +
		"THEN 1 ELSE 2 END,updated_at DESC,id ASC"
	order := clause.OrderBy{Expression: clause.Expr{
		SQL:  ranking,
		Vars: []any{strings.ToLower(query), strings.ToLower(query), literal + "%", literal + "%"},
	}}
	err := r.db.WithContext(ctx).Model(&types.Knowledge{}).
		Select("id,tenant_id,knowledge_base_id,title,file_name,parse_status,updated_at").
		Where("tenant_id=? AND knowledge_base_id=? AND id IN ?", tenant, kb, ids).
		Where(match, "%"+literal+"%", "%"+literal+"%").
		Clauses(order).Limit(limit).Find(&rows).Error
	return rows, err
}
