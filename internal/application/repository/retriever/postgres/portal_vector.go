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
	return protected && !exact && types.WorkspaceWebReadTenant(ctx) == 0 && !types.LocalSourceContentRead(ctx)
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
	// Compute exact distances for every scoped row, without a candidate LIMIT.
	// The materialized set contains only IDs/distances. Every candidate passes
	// the original canonical/policy relation before the single final LIMIT, so
	// arbitrarily many stale nearest neighbours cannot hide a valid result.
	// Detoast an unconstrained halfvec once for dimension and distance checks.
	// Invalid dimensions sort first but raise an error only after authorization;
	// an untrusted malformed index row must not break another document's search.
	if chunkScope != "" {
		chunkScope = " AND " + chunkScope
	}
	tenantIndex, _ := strconv.Atoi(strings.TrimPrefix(tenantPlaceholder, "$"))
	sourcePlaceholder := fmt.Sprintf("$%d", tenantIndex+1)
	return fmt.Sprintf(`WITH portal_vector_candidates AS MATERIALIZED (
 SELECT embeddings.id,CASE WHEN vector_payload.embedding IS NULL THEN NULL
 WHEN vector_dims(vector_payload.embedding)=%d THEN
 (vector_payload.embedding::halfvec(%d) <=> $1::halfvec(%d)) ELSE '-Infinity'::float8 END AS distance
 FROM embeddings JOIN LATERAL (
 SELECT halfvec(embeddings.embedding,-1,false) AS embedding OFFSET 0
 ) vector_payload ON true %s AND embeddings.source_type=%s
)
 SELECT proof.id,proof.content,proof.source_id,proof.source_type,proof.chunk_id,
 proof.knowledge_id,proof.knowledge_base_id,proof.tag_id,
 1-(CASE WHEN ranked.distance='-Infinity'::float8 THEN
 (proof.embedding::halfvec(%d) <=> $1::halfvec(%d)) ELSE ranked.distance END) AS score
 FROM (SELECT id,distance FROM portal_vector_candidates
 WHERE $%d::int>0 AND distance<=$%d ORDER BY distance,id OFFSET 0) ranked
 JOIN LATERAL (
 SELECT embeddings.id,canonical.content,embeddings.source_id,embeddings.source_type,embeddings.chunk_id,
 embeddings.knowledge_id,embeddings.knowledge_base_id,embeddings.tag_id,embeddings.embedding
 FROM embeddings JOIN LATERAL (
 SELECT acl_c.content %s%s
 AND acl_c.id=embeddings.chunk_id AND acl_c.id=embeddings.source_id
 AND acl_c.knowledge_id=embeddings.knowledge_id AND acl_c.knowledge_base_id=embeddings.knowledge_base_id
 AND %s AND embeddings.source_type=%s OFFSET 0
 ) canonical ON true WHERE embeddings.id=ranked.id OFFSET 0
 ) proof ON true ORDER BY ranked.distance,ranked.id LIMIT $%d`,
		dimension, dimension, dimension, whereClause, sourcePlaceholder, dimension, dimension, poolParam, thresholdParam,
		portalCanonicalChunkRelation(tenantPlaceholder, public, defaults), chunkScope,
		portalCanonicalIndexText(), sourcePlaceholder, limitParam)
}
