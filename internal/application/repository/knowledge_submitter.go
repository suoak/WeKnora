package repository

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// ReadKnowledgeSubmitters projects attribution for an already authorized page.
// It does not rewrite historical rows, source receipts or publication digests.
func (r *knowledgeRepository) ReadKnowledgeSubmitters(
	ctx context.Context, tenant uint64, kb string, ids []string,
) (map[string]*types.KnowledgeSubmitter, error) {
	out := map[string]*types.KnowledgeSubmitter{}
	if tenant == 0 || kb == "" || len(ids) == 0 || len(ids) > 1000 {
		return out, nil
	}
	var rows []struct{ ID, Kind, UserID, Name string }
	err := r.db.WithContext(ctx).Raw(`WITH allowed AS (
 SELECT id,tenant_id,knowledge_base_id,channel,
 metadata->'submitter'->>'user_id' AS stored_submitter_user_id,
 metadata->>'datasource_id' AS datasource_id FROM knowledges
 WHERE tenant_id=? AND knowledge_base_id=? AND id IN ? AND deleted_at IS NULL
), people AS (
 SELECT k.id,k.stored_submitter_user_id AS user_id,0 AS priority FROM allowed k
 WHERE coalesce(k.stored_submitter_user_id,'')<>''
 UNION ALL
 SELECT k.id,m.user_id,1 AS priority FROM allowed k
 JOIN feishu_personal_imports m ON m.knowledge_id=k.id AND m.kb_id=k.knowledge_base_id
 WHERE m.user_id<>''
 UNION ALL
 SELECT k.id,a.actor_user_id,2 AS priority FROM allowed k
 JOIN audit_logs a ON a.tenant_id=k.tenant_id AND a.scope_type='knowledge_base'
 AND (a.scope_id=k.knowledge_base_id OR EXISTS(SELECT 1 FROM knowledge_bases origin
 WHERE origin.id=a.scope_id AND origin.tenant_id=k.tenant_id))
 AND a.target_type='knowledge' AND a.target_id=k.id
 AND a.action='knowledge.created' AND a.outcome IN ('accepted','success')
 WHERE a.actor_user_id<>''
), best AS (SELECT id,MIN(priority) AS priority FROM people GROUP BY id), chosen AS (
 SELECT p.id,MIN(p.user_id) AS user_id FROM people p
 JOIN best b ON b.id=p.id AND b.priority=p.priority
 GROUP BY p.id HAVING COUNT(DISTINCT p.user_id)=1
), resolved AS (
 SELECT p.id,u.id AS user_id,u.username AS name FROM chosen p
 JOIN users u ON u.id=p.user_id AND u.deleted_at IS NULL AND trim(u.username)<>''
)
SELECT k.id,coalesce(r.user_id,'') AS user_id,coalesce(r.name,'') AS name,
 CASE WHEN r.user_id IS NOT NULL THEN 'user'
 WHEN EXISTS(SELECT 1 FROM people p WHERE p.id=k.id) THEN ''
 WHEN EXISTS(SELECT 1 FROM data_sources d WHERE d.id=k.datasource_id
 AND d.tenant_id=k.tenant_id AND d.knowledge_base_id=k.knowledge_base_id
 AND d.type=k.channel) THEN 'system_sync' ELSE '' END AS kind
FROM allowed k LEFT JOIN resolved r ON r.id=k.id`, tenant, kb, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.Kind == "system_sync" {
			out[row.ID] = &types.KnowledgeSubmitter{Kind: row.Kind}
		} else if row.Kind == "user" && !types.IsSyntheticUserID(row.UserID) && strings.TrimSpace(row.Name) != "" {
			out[row.ID] = &types.KnowledgeSubmitter{Kind: row.Kind, UserID: row.UserID, Name: row.Name}
		}
	}
	return out, nil
}
