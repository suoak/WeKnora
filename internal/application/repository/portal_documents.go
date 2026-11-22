package repository

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
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

// SearchPortalDocumentMetadataBatch uses one bound JSON relation of exact
// authorized tenant/KB/document tuples. The scope join precedes matching,
// ranking and LIMIT; SQLite retains the existing portable per-KB path.
func (r *knowledgeRepository) SearchPortalDocumentMetadataBatch(
	ctx context.Context, targets []*access.PortalKBTarget, query string, limit int,
) ([]*types.Knowledge, bool, error) {
	if r.db.Name() != "postgres" {
		return nil, false, nil
	}
	type scope struct {
		Tenant uint64   `json:"tenant"`
		KB     string   `json:"kb"`
		IDs    []string `json:"ids"`
	}
	scopes := make([]scope, 0, len(targets))
	for _, target := range targets {
		if err := target.Revalidate(ctx); err != nil {
			return nil, true, err
		}
		d := target.Descriptor()
		ids := target.DocumentIDs()
		if ids == nil {
			ids = []string{}
		}
		scopes = append(scopes, scope{d.TenantID, d.ID, ids})
	}
	encoded, err := json.Marshal(scopes)
	if err != nil {
		return nil, true, err
	}
	lower := strings.ToLower(query)
	literal := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(lower)
	sql := `WITH scopes AS MATERIALIZED (
 SELECT * FROM jsonb_to_recordset(?::jsonb) AS s(tenant bigint,kb text,ids jsonb)
 ) SELECT k.id,k.tenant_id,k.knowledge_base_id,k.title,k.file_name,k.parse_status,k.updated_at
 FROM scopes s JOIN knowledges k ON k.tenant_id=s.tenant AND k.knowledge_base_id=s.kb
 AND k.id IN (SELECT jsonb_array_elements_text(s.ids))
 WHERE k.deleted_at IS NULL AND (LOWER(k.title) LIKE ? ESCAPE '!' OR LOWER(k.file_name) LIKE ? ESCAPE '!')`
	args := []any{string(encoded), "%" + literal + "%", "%" + literal + "%"}
	if types.PrivateKBGuardsEnabled(r.db) {
		sql += ` AND NOT EXISTS(SELECT 1 FROM feishu_private_kbs p WHERE p.kb_id=k.knowledge_base_id AND p.owner_id<>?)`
		args = append(args, types.PrivateKBReadUser(ctx))
	}
	sql += ` ORDER BY CASE WHEN LOWER(k.title)=? OR LOWER(k.file_name)=? THEN 0
 WHEN LOWER(k.title) LIKE ? ESCAPE '!' OR LOWER(k.file_name) LIKE ? ESCAPE '!' THEN 1 ELSE 2 END,
 k.updated_at DESC,('knowhub:tenant:'||k.tenant_id::text||':kb:'||k.knowledge_base_id||':document:'||k.id) ASC LIMIT ?`
	args = append(args, lower, lower, literal+"%", literal+"%", limit)
	var rows []*types.Knowledge
	err = r.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	return rows, true, err
}
