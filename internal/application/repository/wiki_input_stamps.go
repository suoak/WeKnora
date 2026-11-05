package repository

import "gorm.io/gorm"

// Trigger coverage includes every knowledge/chunk mutation, owner changes,
// timestamp-preserving ABA and PostgreSQL TRUNCATE. The input digest remains
// the original complete content hash; these live stamps avoid reading all KB
// bodies again before every model request and every individual page write.
func wikiDocumentInputStamps(db *gorm.DB, kb string, tenant uint64) (map[string]string, error) {
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
	if err := db.Raw(`SELECT k.id,k.tenant_id,k.knowledge_base_id AS kb_id,e.generation,e.incarnation
 FROM knowledges k LEFT JOIN document_access_epochs e ON e.knowledge_id=k.id
 WHERE k.knowledge_base_id=? AND k.deleted_at IS NULL ORDER BY k.id LIMIT 2049`, kb).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 || len(rows) > 2048 {
		return nil, ErrWikiMCPUnverified
	}
	result := make(map[string]string, len(rows))
	for _, row := range rows {
		if row.TenantID != tenant || row.KBID != kb || row.Generation < 1 || len(row.Incarnation) != 32 {
			return nil, ErrWikiMCPUnverified
		}
		result[row.ID] = wikiDigest(row)
	}
	return result, nil
}
