package postgres

import (
	"fmt"
	"strconv"
	"strings"
)

// Runs inside the candidate SQL WHERE, before BM25/vector LIMIT. The canonical
// Current canonical forms reject stale index bodies and synthetic question rows.
func portalCanonicalPredicate(tenantPlaceholder string, public bool, publishedDefaults ...bool) string {
	sourcePlaceholder := "?"
	if strings.HasPrefix(tenantPlaceholder, "$") {
		n, _ := strconv.Atoi(tenantPlaceholder[1:])
		sourcePlaceholder = fmt.Sprintf("$%d", n+1)
	}
	policy := ""
	defaults := len(publishedDefaults) == 1 && publishedDefaults[0]
	block := func(p string) string {
		return "(" + p + ".state='revoked' OR " + p + ".policy IN ('restricted','mcp_blocked','deny') OR " + p + ".content_class='mixed' OR " + p + ".source_acl_state='revoked')"
	}
	if defaults {
		// Preserve explicit document restrictions as a whole-KB hold, including
		// a concurrent restriction installed after grant resolution. No missing
		// approval record or import channel excludes a Published ordinary KB.
		policy = ` AND NOT EXISTS (SELECT 1 FROM knowledge_access_policies acl_dp
 JOIN knowledges acl_dk ON acl_dk.id=acl_dp.knowledge_id
 WHERE acl_dk.knowledge_base_id=acl_k.knowledge_base_id AND acl_dk.tenant_id=acl_k.tenant_id
 AND acl_dk.deleted_at IS NULL AND ` + block("acl_dp") + `)`
		if public {
			policy += ` AND NOT EXISTS (SELECT 1 FROM kb_publication_policies acl_bp WHERE acl_bp.knowledge_base_id=acl_b.id AND ` + block("acl_bp") + `)
 AND NOT EXISTS (SELECT 1 FROM enterprise_space_policies acl_sp WHERE acl_sp.tenant_id=acl_k.tenant_id AND ` + block("acl_sp") + `)
 AND EXISTS (SELECT 1 FROM tenant_portal_configs acl_pc WHERE acl_pc.tenant_id=acl_k.tenant_id AND acl_pc.status='published')`
		}
	} else if public {
		policy = ` AND EXISTS (SELECT 1 FROM knowledge_access_policies acl_p
 WHERE acl_p.knowledge_id=acl_k.id AND acl_p.state='approved' AND acl_p.policy='inherit'
 AND acl_p.content_class='ordinary' AND acl_p.source_acl_state='local' AND acl_p.revision>0)
 AND EXISTS (SELECT 1 FROM tenant_portal_configs acl_pc JOIN tenants acl_t ON acl_t.id=acl_pc.tenant_id
 WHERE acl_pc.tenant_id=acl_k.tenant_id AND acl_pc.status='published' AND acl_t.status='active' AND acl_t.deleted_at IS NULL)`
	}
	return `EXISTS (SELECT 1 FROM chunks acl_c JOIN knowledges acl_k ON acl_k.id=acl_c.knowledge_id
 JOIN knowledge_bases acl_b ON acl_b.id=acl_k.knowledge_base_id AND acl_b.tenant_id=acl_k.tenant_id
 JOIN tenants acl_owner ON acl_owner.id=acl_b.tenant_id
 WHERE acl_c.id=embeddings.chunk_id AND acl_c.knowledge_id=embeddings.knowledge_id
 AND acl_k.knowledge_base_id=embeddings.knowledge_base_id AND acl_c.knowledge_base_id=acl_k.knowledge_base_id
 AND acl_k.tenant_id=` + tenantPlaceholder + ` AND acl_c.tenant_id=acl_k.tenant_id
 AND acl_k.deleted_at IS NULL AND acl_c.deleted_at IS NULL AND acl_k.enable_status='enabled'
	AND acl_b.deleted_at IS NULL AND acl_b.is_temporary=false AND acl_owner.deleted_at IS NULL AND acl_owner.status='active'
	AND acl_k.type IN ('file','manual')
 AND acl_k.parse_status='completed' AND acl_c.is_enabled=true AND acl_c.chunk_type='text'
 AND embeddings.source_id=acl_c.id AND ` + portalCanonicalIndexText() + ` AND embeddings.source_type=` + sourcePlaceholder + policy + `)`
}

// Match the existing writers, without trusting index-only text. Go's
// strings.TrimSpace uses exactly these Unicode White_Space code points.
// They are constants, never SQL supplied by a caller or an imported document.
func portalTrimmedSQL(column string) string {
	const whitespace = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"
	return "btrim(" + column + ", '" + whitespace + "')"
}

func portalCanonicalIndexText() string {
	body := portalTrimmedSQL("acl_c.content")
	embeddingBody := "(CASE WHEN coalesce(acl_c.context_header,'')='' THEN " + body + " ELSE acl_c.context_header || E'\\n\\n' || " + body + " END)"
	title := portalTrimmedSQL("coalesce(acl_k.title,'')")
	// Normal indexing uses a trimmed title and one newline. The isolated
	// Feishu writer uses its bound title and two newlines. Historical bare
	// Chunk rows remain valid. Every variant is reconstructed from the SAME
	// authorized current Knowledge/Chunk, not prefix/suffix pattern matching.
	return "(embeddings.content=acl_c.content OR embeddings.content=" + embeddingBody +
		" OR embeddings.content=(CASE WHEN " + title + "='' THEN " + embeddingBody + " ELSE " + title + " || E'\\n' || " + embeddingBody + " END)" +
		" OR embeddings.content=(coalesce(acl_k.title,'') || E'\\n\\n' || " + embeddingBody + "))"
}

// Project only current Chunk text after the candidate WHERE has verified its
// exact canonical index form. This keeps the existing service-side equality
// check and prevents enriched/stale index text from reaching a model/output.
func portalCanonicalTextProjection() string {
	return `(SELECT acl_projection.content FROM chunks acl_projection
 WHERE acl_projection.id=embeddings.chunk_id AND acl_projection.knowledge_id=embeddings.knowledge_id
 AND acl_projection.knowledge_base_id=embeddings.knowledge_base_id)`
}

// Preserve a correlated probe for dimension EXISTS checks, rather than letting
// PostgreSQL flatten the canonical relation into a misestimated whole-KB join.
// OFFSET 0 does not remove predicates or limit the authorized relation. The
// candidate BM25/vector query shape is deliberately unchanged.
func portalDimensionCanonicalPredicate(tenant string, public bool, defaults ...bool) string {
	predicate := portalCanonicalPredicate(tenant, public, defaults...)
	return strings.TrimSuffix(predicate, ")") + " OFFSET 0)"
}
