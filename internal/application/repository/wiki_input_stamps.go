package repository

import (
	"sort"

	"gorm.io/gorm"
)

func wikiSelectionIDs(selected map[string]bool) []string {
	ids := make([]string, 0, len(selected))
	for id, include := range selected {
		if include {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// Trigger coverage includes every knowledge/chunk mutation, owner changes,
// timestamp-preserving ABA and PostgreSQL TRUNCATE. The input digest remains
// the original complete content hash; these live stamps avoid reading all KB
// bodies again before every model request and every individual page write.
func wikiDocumentInputStamps(db *gorm.DB, kb string, tenant uint64) (map[string]string, error) {
	return wikiSelectedDocumentInputStamps(db, kb, tenant, nil)
}

func wikiSelectedDocumentInputStamps(
	db *gorm.DB,
	kb string,
	tenant uint64,
	selected map[string]bool,
) (map[string]string, error) {
	if err := checkDocumentEpochCoverage(db); err != nil {
		return nil, err
	}
	var rows []struct {
		ID          string
		TenantID    uint64
		KBID        string
		Generation  int64
		Incarnation string
	}
	query := `SELECT k.id,k.tenant_id,k.knowledge_base_id AS kb_id,e.generation,e.incarnation
 FROM knowledges k LEFT JOIN document_access_epochs e ON e.knowledge_id=k.id
 WHERE k.knowledge_base_id=? AND k.deleted_at IS NULL`
	args := []any{kb}
	if selected != nil {
		query += " AND k.id IN ?"
		args = append(args, wikiSelectionIDs(selected))
	}
	query += " ORDER BY k.id LIMIT 2049"
	if err := db.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 || len(rows) > 2048 {
		return nil, ErrWikiMCPUnverified
	}
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		if selected != nil && !selected[row.ID] {
			continue
		}
		if row.TenantID != tenant || row.KBID != kb || row.Generation < 1 || len(row.Incarnation) != 32 {
			return nil, ErrWikiMCPUnverified
		}
		result[row.ID] = wikiDigest(row)
	}
	if selected != nil && len(result) != len(selected) {
		return nil, ErrWikiMCPInputsChanged
	}
	return result, nil
}
