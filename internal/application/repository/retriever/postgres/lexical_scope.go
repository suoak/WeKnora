package postgres

import (
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

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
