package postgres

import (
	"context"
	"encoding/hex"
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
	if receipt, tenant, knowledge, kb, exact := types.ExactPublishedDocumentIdentity(ctx); exact {
		// Values are encoded as hexadecimal, never interpolated as SQL text.
		literal := func(value string) string {
			return "convert_from(decode('" + hex.EncodeToString([]byte(value)) + "','hex'),'UTF8')"
		}
		return `((` + ordinaryLegacySourceCandidatePredicate(ctx) + `) OR (
embeddings.knowledge_id=` + literal(knowledge) + ` AND embeddings.knowledge_base_id=` + literal(kb) + `
AND EXISTS(SELECT 1 FROM task_pending_ops exact_r
JOIN feishu_personal_resources exact_v ON exact_v.logical_knowledge_id=embeddings.knowledge_id
JOIN feishu_personal_attempts exact_a ON exact_a.id=exact_v.current_attempt_id
JOIN feishu_personal_imports exact_i ON exact_i.id=exact_a.import_id
JOIN feishu_personal_grant_epochs exact_g ON exact_g.user_id=exact_i.user_id AND exact_g.epoch=exact_a.grant_epoch
JOIN feishu_connections exact_conn ON exact_conn.user_id=exact_i.user_id
 AND exact_conn.app_id=exact_i.app_id AND exact_conn.open_id=exact_i.open_id
JOIN resources exact_file ON exact_file.id=exact_r.payload->>'resource_id'
 AND exact_file.tenant_id=exact_r.tenant_id AND exact_file.handle=exact_r.payload->>'resource_handle'
JOIN resource_bindings exact_binding ON exact_binding.id=exact_r.payload->>'binding_id'
 AND exact_binding.resource_id=exact_file.id AND exact_binding.owner_type='knowledge'
 AND exact_binding.owner_id=embeddings.knowledge_id AND exact_binding.relation='source_file'
 AND exact_binding.tenant_id=exact_r.tenant_id
WHERE exact_r.id=` + strconv.FormatInt(receipt, 10) + ` AND exact_r.tenant_id=` + strconv.FormatUint(tenant, 10) + `
AND exact_r.scope='personal_exact_receipt_v1' AND exact_r.scope_id=embeddings.knowledge_id AND exact_r.op='published'
AND exact_v.generation=exact_a.fence AND exact_v.active_version_id=exact_a.candidate_version_id
AND exact_v.candidate_version_id=exact_v.active_version_id AND exact_a.status='indexed'
AND exact_a.owner_nonce='' AND exact_a.lease_until IS NULL
AND exact_i.status IN ('indexed','synced') AND exact_conn.expires_at>clock_timestamp()
AND exact_r.payload->>'connection_identity'=exact_conn.app_id||':'||exact_conn.open_id
AND exact_file.provider='local' AND exact_file.state='active' AND exact_file.lifecycle='persistent'
AND exact_file.deleted_at IS NULL AND exact_file.expires_at IS NULL
AND (COALESCE(exact_file.content_hash,'')='' OR exact_file.content_hash=exact_r.payload->>'file_sha256')
AND (SELECT count(*) FROM resource_bindings exact_all_bindings WHERE exact_all_bindings.resource_id=exact_file.id)=1
AND EXISTS(SELECT 1 FROM tenant_members exact_viewer JOIN users exact_user ON exact_user.id=exact_viewer.user_id
 WHERE exact_viewer.user_id=` + literal(types.CallerFromContext(ctx).UserID) + `
 AND exact_viewer.tenant_id=` + strconv.FormatUint(types.CallerFromContext(ctx).TenantID, 10) + `
 AND exact_viewer.role=` + literal(string(types.CallerFromContext(ctx).Role)) + `
 AND exact_viewer.status='active' AND exact_viewer.deleted_at IS NULL
 AND exact_user.is_active AND exact_user.deleted_at IS NULL)
AND exact_r.payload->>'attempt_id'=exact_a.id
AND exact_r.payload->>'generation'=exact_v.generation::text
AND exact_r.payload->>'candidate_version_id'=exact_v.active_version_id
AND ((exact_r.payload->>'vectors_db_manifest')::jsonb)->>embeddings.source_id=
encode(sha256(convert_to((to_jsonb(embeddings)-'created_at'-'updated_at'-'id'-'score')::text,'UTF8')),'hex')
)))`
	}
	return ordinaryLegacySourceCandidatePredicate(ctx)
}

func ordinaryLegacySourceCandidatePredicate(ctx context.Context) string {
	// Staged/recovered output requires an exact serving receipt, independent
	// of the older workspace exception for a held datasource. Apply this at
	// candidate selection, before returning content, snippets or scores.
	const receiptGate = `NOT EXISTS (SELECT 1 FROM task_pending_ops exact_r
 WHERE exact_r.scope='personal_exact_receipt_v1' AND exact_r.scope_id=embeddings.knowledge_id) AND `
	tenant := types.WorkspaceWebReadTenant(ctx)
	if tenant == 0 {
		return receiptGate + legacyStrictSourceCandidatePredicate
	}
	// Match CheckLegacySourceRead: a sync hold does not revoke an existing
	// workspace member's stored knowledge. Document quarantine and revocation
	// remain authoritative. The tenant is a server-validated uint64 literal.
	return receiptGate + `NOT EXISTS (
 SELECT 1 FROM feishu_legacy_resources legacy_r WHERE legacy_r.knowledge_base_id=embeddings.knowledge_base_id
 AND legacy_r.state IN ('hold','revoked') AND EXISTS (
 SELECT 1 FROM knowledges existing_doc WHERE existing_doc.id=legacy_r.knowledge_id
 AND existing_doc.deleted_at IS NULL)) AND NOT EXISTS (
 SELECT 1 FROM data_sources legacy_d WHERE legacy_d.knowledge_base_id=embeddings.knowledge_base_id
 AND (legacy_d.legacy_access_state='revoked' OR (legacy_d.legacy_access_state='hold' AND NOT EXISTS (
 SELECT 1 FROM knowledge_bases owner_kb WHERE owner_kb.id=legacy_d.knowledge_base_id
 AND owner_kb.tenant_id=` + strconv.FormatUint(tenant, 10) + `))))`
}
