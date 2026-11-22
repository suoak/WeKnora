package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

type portalVectorIndexError struct{ ids []string }

func (e *portalVectorIndexError) Error() string {
	return "personal MCP vector index does not match bound model"
}

func (e *portalVectorIndexError) InvalidPortalKnowledgeBases() []string {
	return append([]string(nil), e.ids...)
}

func (g *pgRepository) portalGroupedVectorDimensions(
	ctx context.Context, params types.RetrieveParams, dimension int, relation func() *gorm.DB,
) error {
	marks := make([]string, len(params.KnowledgeBaseIDs))
	args := make([]any, 0, len(marks)+2)
	for i, kb := range params.KnowledgeBaseIDs {
		marks[i] = "(?::text)"
		args = append(args, kb)
	}
	args = append(args,
		relation().Where("knowledge_base_id=scopes.kb AND dimension IS DISTINCT FROM ?", dimension),
		relation().Where("knowledge_base_id=scopes.kb AND dimension = ?", dimension))
	var rows []struct {
		KnowledgeBaseID        string
		Incompatible, Matching bool
	}
	err := portalDimensionQuerySession(g.db.WithContext(ctx), func(tx *gorm.DB) error {
		return tx.Raw("WITH scopes(kb) AS (VALUES "+strings.Join(marks, ",")+") "+
			"SELECT kb AS knowledge_base_id,EXISTS (?) AS incompatible,EXISTS (?) AS matching FROM scopes",
			args...).Scan(&rows).Error
	})
	if err != nil {
		return err
	}
	if len(rows) != len(marks) {
		return access.ErrForbidden
	}
	var invalid []string
	for _, row := range rows {
		if row.Incompatible || !row.Matching && access.PortalRetrievalKBHasDocuments(ctx, row.KnowledgeBaseID) {
			invalid = append(invalid, row.KnowledgeBaseID)
		}
	}
	if len(invalid) > 0 {
		return &portalVectorIndexError{invalid}
	}
	return nil
}

// portalGroupedVectorSQL keeps each KB's exact ordered scan and TopK, while
// sharing one SQL round trip. Dimension/tenant/store groups are never mixed.
func portalGroupedVectorSQL(sql string, ids []string, vars []any) (string, []any) {
	marks := make([]string, 0, len(ids))
	for _, kb := range ids {
		vars = append(vars, kb)
		marks = append(marks, fmt.Sprintf("($%d::text)", len(vars)))
	}
	return "SELECT hits.* FROM (VALUES " + strings.Join(marks, ",") + ") scopes(kb) " +
		"CROSS JOIN LATERAL (" + sql + ") hits ORDER BY hits.knowledge_base_id,hits.score DESC,hits.id", vars
}
