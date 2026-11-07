package postgres

import (
	"context"
	"fmt"
	"strings"

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
