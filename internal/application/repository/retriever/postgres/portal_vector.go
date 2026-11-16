package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

func bulkPortalVector(ctx context.Context) bool {
	_, protected := access.PortalRetrievalTenant(ctx)
	_, _, _, _, exact := types.ExactPublishedDocumentIdentity(ctx)
	// Exact serving receipts have their own row-bound manifest predicates.
	// Workspace reads retain their existing source-hold exception as well.
	return protected && !exact && types.WorkspaceWebReadTenant(ctx) == 0
}

// A statement-local set, never a process authorization cache. Explicit NULL
// handling preserves NOT EXISTS semantics for both NULL candidates and rows.
func portalExcludedSet(column, selection string) string {
	return "NOT COALESCE(" + column + " = ANY(ARRAY(" + selection + ")),false)"
}

func portalOrdinarySourcePredicate() string {
	return strings.Join([]string{
		portalExcludedSet("embeddings.knowledge_id", `SELECT scope_id FROM task_pending_ops
 WHERE scope='personal_exact_receipt_v1' AND scope_id IS NOT NULL`),
		portalExcludedSet("embeddings.knowledge_id", `SELECT id FROM knowledges
 WHERE channel='feishu_personal' AND id IS NOT NULL`),
		portalExcludedSet("embeddings.knowledge_base_id", `SELECT knowledge_base_id FROM feishu_legacy_resources
 WHERE state IN ('hold','revoked') AND knowledge_base_id IS NOT NULL`),
		portalExcludedSet("embeddings.knowledge_base_id", `SELECT knowledge_base_id FROM data_sources
 WHERE legacy_access_state IN ('hold','revoked') AND knowledge_base_id IS NOT NULL`),
	}, " AND ")
}

func portalVectorSQL(dimension int, tenantPlaceholder, chunkScope, whereClause string,
	public, defaults bool, poolParam, thresholdParam, limitParam int,
) string {
	if chunkScope != "" {
		chunkScope = " AND " + chunkScope
	}
	canonicalText := strings.ReplaceAll(portalCanonicalIndexText(), "acl_k.title", "acl_c.title")
	tenantIndex, _ := strconv.Atoi(strings.TrimPrefix(tenantPlaceholder, "$"))
	sourcePlaceholder := fmt.Sprintf("$%d", tenantIndex+1)
	// All eligible chunks are authorized before the embedding join and LIMIT.
	// Store one scalar distance instead of spilling every full vector and
	// rereading it for both the threshold and sort. The exact rank is unchanged.
	return fmt.Sprintf(`WITH portal_chunks AS MATERIALIZED (
 SELECT acl_c.id,acl_c.knowledge_id,acl_c.knowledge_base_id,acl_c.content,acl_c.context_header,acl_k.title
 %s%s
), authorized AS MATERIALIZED (
 SELECT embeddings.id,acl_c.content,embeddings.source_id,embeddings.source_type,embeddings.chunk_id,
 embeddings.knowledge_id,embeddings.knowledge_base_id,embeddings.tag_id,
 (embeddings.embedding::halfvec(%d) <=> $1::halfvec(%d)) AS distance
 FROM embeddings JOIN portal_chunks acl_c ON acl_c.id=embeddings.chunk_id
 AND acl_c.id=embeddings.source_id AND acl_c.knowledge_id=embeddings.knowledge_id
 AND acl_c.knowledge_base_id=embeddings.knowledge_base_id
 %s AND embeddings.source_type=%s AND %s
)
 SELECT id,content,source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,tag_id,1-distance AS score
 FROM authorized WHERE $%d::int>0 AND distance<=$%d ORDER BY distance,id LIMIT $%d`,
		portalCanonicalChunkRelation(tenantPlaceholder, public, defaults), chunkScope,
		dimension, dimension, whereClause, sourcePlaceholder, canonicalText, poolParam, thresholdParam, limitParam)
}
