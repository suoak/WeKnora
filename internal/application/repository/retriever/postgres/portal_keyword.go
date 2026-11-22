package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// portalOrderedKeywordSQL reuses the vector path's ordered-ID/lateral-proof
// shape. No candidate LIMIT precedes authorization. The lexical relation never
// copies vector payloads or document bodies, and every returned row passes the
// existing full canonical/source predicates before its per-KB final LIMIT.
func (g *pgRepository) portalOrderedKeywordSQL(
	ctx context.Context, params types.RetrieveParams, conditions []clause.Expression, columns []string,
) (string, []any, error) {
	projection := append([]string(nil), columns...)
	projection[0] = "lexical.score AS score"
	var rows []pgVectorWithScore
	proof := g.db.WithContext(ctx).Session(&gorm.Session{DryRun: true}).Table("embeddings").
		Clauses(conditions...).Where("embeddings.id=lexical.id").Select(projection).Find(&rows).Statement
	vars := append([]any(nil), proof.Vars...)
	vars = append(vars, params.Query)
	where := fmt.Sprintf("content ||| $%d", len(vars))
	where, vars, err := g.lexicalSearchScope(ctx, where, vars, params)
	if err != nil {
		return "", nil, err
	}
	// The canonical proof already binds both source_id and chunk_id to the
	// same current chunk. Exclude synthetic identities before materialization
	// and per-KB proof probes too; no authoritative predicate or LIMIT moves.
	cte := "WITH lexical AS MATERIALIZED (SELECT id,knowledge_base_id,paradedb.score(id) AS score " +
		"FROM embeddings WHERE " + where + " AND source_id=chunk_id ORDER BY score DESC,id) "
	selection := "SELECT proof.* FROM lexical JOIN LATERAL (" + proof.SQL.String() + " OFFSET 0) proof ON true "
	vars = append(vars, params.TopK)
	limit := fmt.Sprintf("$%d", len(vars))
	order := " ORDER BY lexical.score DESC,lexical.id LIMIT " + limit
	if !access.PortalRetrievalGrouped(ctx) {
		return cte + selection + order, vars, nil
	}
	marks := make([]string, 0, len(params.KnowledgeBaseIDs))
	for _, kb := range params.KnowledgeBaseIDs {
		vars = append(vars, kb)
		marks = append(marks, fmt.Sprintf("($%d::text)", len(vars)))
	}
	return cte + "SELECT hits.* FROM (VALUES " + strings.Join(marks, ",") + ") scopes(kb) " +
		"CROSS JOIN LATERAL (" + selection + "WHERE lexical.knowledge_base_id=scopes.kb" + order +
		") hits ORDER BY hits.knowledge_base_id,hits.score DESC,hits.id", vars, nil
}
