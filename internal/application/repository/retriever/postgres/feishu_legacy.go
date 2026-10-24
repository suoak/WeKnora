package postgres

import (
	"context"
	"strconv"

	"github.com/Tencent/WeKnora/internal/types"
)

// Applied inside the actual BM25/vector candidate query. A later output filter
// cannot protect model input, scores, titles or snippets from a held KB.
const legacyStrictSourceCandidatePredicate = `NOT EXISTS (
 SELECT 1 FROM feishu_legacy_resources legacy_r WHERE legacy_r.knowledge_base_id=embeddings.knowledge_base_id
 AND legacy_r.state IN ('hold','revoked')) AND NOT EXISTS (
 SELECT 1 FROM data_sources legacy_d WHERE legacy_d.knowledge_base_id=embeddings.knowledge_base_id
 AND legacy_d.legacy_access_state IN ('hold','revoked'))`

func legacySourceCandidatePredicate(ctx context.Context) string {
	tenant := types.WorkspaceWebReadTenant(ctx)
	if tenant == 0 {
		return legacyStrictSourceCandidatePredicate
	}
	// Match CheckLegacySourceRead: a sync hold does not revoke an existing
	// workspace member's stored knowledge. Document quarantine and revocation
	// remain authoritative. The tenant is a server-validated uint64 literal.
	return `NOT EXISTS (
 SELECT 1 FROM feishu_legacy_resources legacy_r WHERE legacy_r.knowledge_base_id=embeddings.knowledge_base_id
 AND legacy_r.state IN ('hold','revoked') AND EXISTS (
 SELECT 1 FROM knowledges existing_doc WHERE existing_doc.id=legacy_r.knowledge_id
 AND existing_doc.deleted_at IS NULL)) AND NOT EXISTS (
 SELECT 1 FROM data_sources legacy_d WHERE legacy_d.knowledge_base_id=embeddings.knowledge_base_id
 AND (legacy_d.legacy_access_state='revoked' OR (legacy_d.legacy_access_state='hold' AND NOT EXISTS (
 SELECT 1 FROM knowledge_bases owner_kb WHERE owner_kb.id=legacy_d.knowledge_base_id
 AND owner_kb.tenant_id=` + strconv.FormatUint(tenant, 10) + `))))`
}
