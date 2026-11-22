package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
)

// The exact publication manifest is bounded to 1 MiB (well below this many
// UUID/hash pairs). This also keeps the scalar SQL bindings below PostgreSQL's
// parameter limit; an invalid/concurrently enlarged document stays closed.
const maxPublishedLexicalCandidateIDs = 16384

// Published files have already been independently authorized. Select only
// numeric embedding identities, never bodies, and use their indexed key field
// for the coarse lexical scan. UUID text selectors use heap_filter in the live
// ParadeDB index and otherwise scan the global postings for every file.
// Every authoritative KB/document/state/receipt/hash predicate remains in the
// outer query, BEFORE its only LIMIT. This is not an authorization grant.
func (g *pgRepository) lexicalSearchScope(
	ctx context.Context, where string, vars []interface{}, params types.RetrieveParams,
) (string, []interface{}, error) {
	_, _, knowledge, kb, exact := types.ExactPublishedDocumentIdentity(ctx)
	if !exact {
		if tenant, protected := access.PortalRetrievalTenant(ctx); protected {
			return g.portalLexicalSearchScope(ctx, tenant, where, vars, params)
		}
		where, vars = lexicalCandidateScope(where, vars, params)
		return where, vars, nil
	}
	if (len(params.KnowledgeBaseIDs) > 0 && (len(params.KnowledgeBaseIDs) != 1 || params.KnowledgeBaseIDs[0] != kb)) ||
		(len(params.KnowledgeIDs) > 0 && (len(params.KnowledgeIDs) != 1 || params.KnowledgeIDs[0] != knowledge)) {
		return "", nil, types.ErrExactDocumentScope
	}
	if _, err := types.CheckExactPublishedDocumentRead(ctx); err != nil {
		return "", nil, err
	}
	var ids []int64
	if err := g.db.WithContext(ctx).Table("embeddings").Where(
		"knowledge_id=? AND knowledge_base_id=?", knowledge, kb,
	).Order("id").Limit(maxPublishedLexicalCandidateIDs+1).Pluck("id", &ids).Error; err != nil {
		return "", nil, err
	}
	if len(ids) > maxPublishedLexicalCandidateIDs {
		return "", nil, types.ErrExactDocumentScope
	}
	if _, err := types.CheckExactPublishedDocumentRead(ctx); err != nil {
		return "", nil, err
	}
	if len(ids) == 0 {
		return where + " AND FALSE", vars, nil
	}
	marks := make([]string, 0, len(ids))
	for _, id := range ids {
		vars = append(vars, id)
		marks = append(marks, fmt.Sprintf("$%d::bigint", len(vars)))
	}
	// The key set is a filter, not textual evidence: it must add no score.
	return where + " AND id @@@ paradedb.boost(0::real,paradedb.term_set('id',ARRAY[" +
		strings.Join(marks, ",") + "]::bigint[]))", vars, nil
}

// UUID columns in the deployed ParadeDB index use heap_filter. Constrain the
// native postings by their numeric indexed identities instead. This is only a
// coarse accelerator for the full issued KB group: the caller retains every
// live tenant/document/canonical predicate outside MATERIALIZED and before LIMIT.
const maxPortalLexicalCandidateIDs = 131072

func (g *pgRepository) portalLexicalSearchScope(ctx context.Context, tenant uint64,
	where string, vars []interface{}, params types.RetrieveParams,
) (string, []interface{}, error) {
	if tenant == 0 || len(params.KnowledgeBaseIDs) == 0 {
		where, vars = lexicalCandidateScope(where, vars, params)
		return where, vars, nil
	}
	var ids []int64
	q := g.db.WithContext(ctx).Table("embeddings AS e").
		Where("e.knowledge_base_id IN ?", params.KnowledgeBaseIDs).
		Where("EXISTS(SELECT 1 FROM knowledge_bases kb WHERE kb.id=e.knowledge_base_id "+
			"AND kb.tenant_id=? AND kb.deleted_at IS NULL)", tenant)
	if len(params.KnowledgeIDs) > 0 {
		q = q.Where("e.knowledge_id IN ?", params.KnowledgeIDs)
	}
	if err := q.Order("e.id").Limit(maxPortalLexicalCandidateIDs+1).Pluck("e.id", &ids).Error; err != nil {
		return "", nil, err
	}
	if len(ids) > maxPortalLexicalCandidateIDs {
		// Never truncate a search corpus before ranking or authorization.
		where, vars = lexicalCandidateScope(where, vars, params)
		return where, vars, nil
	}
	if len(ids) == 0 {
		return where + " AND FALSE", vars, nil
	}
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = strconv.FormatInt(id, 10)
	}
	// One scalar array binding avoids PostgreSQL's parameter-count limit. Values
	// are server-loaded integers; no query/document text is interpolated in SQL.
	vars = append(vars, "{"+strings.Join(values, ",")+"}")
	return where + fmt.Sprintf(
		" AND id @@@ paradedb.boost(0::real,paradedb.term_set('id',$%d::bigint[]))",
		len(vars),
	), vars, nil
}

// Coarse, scalar restrictions only. The outer query retains every canonical
// KB/document/source predicate and applies authorization BEFORE its only LIMIT.
// Do not push correlated authorization ORs into ParadeDB's custom scan.
func lexicalCandidateScope(where string, vars []interface{}, params types.RetrieveParams) (string, []interface{}) {
	for _, restriction := range []struct {
		column string
		ids    []string
	}{
		{"knowledge_base_id", params.KnowledgeBaseIDs},
		{"knowledge_id", params.KnowledgeIDs},
	} {
		if len(restriction.ids) == 0 {
			continue
		}
		marks := make([]string, 0, len(restriction.ids))
		for _, id := range restriction.ids {
			vars = append(vars, id)
			marks = append(marks, fmt.Sprintf("$%d", len(vars)))
		}
		where += " AND " + restriction.column + " IN (" + strings.Join(marks, ",") + ")"
	}
	return where, vars
}
