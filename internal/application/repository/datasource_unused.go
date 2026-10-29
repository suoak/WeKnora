package repository

import (
	"bytes"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// The caller holds the datasource lock through soft deletion. Only a source
// with no execution or content history can leave an inert tombstone. Historical
// (including deleted) documents, attempted publications and derived Wiki pages
// keep the existing revocation policy; no read guard is bypassed.
func deletedLegacySourceState(tx *gorm.DB, source *types.DataSource) (string, error) {
	if source.LastSyncAt != nil || hasSyncJSON(source.LastSyncCursor) || hasSyncJSON(source.LastSyncResult) {
		return "revoked", nil
	}
	var used bool
	err := tx.Raw(`SELECT
EXISTS(SELECT 1 FROM sync_logs WHERE data_source_id=?)
OR EXISTS(SELECT 1 FROM knowledges WHERE metadata->>'datasource_id'=?)
OR EXISTS(SELECT 1 FROM feishu_legacy_resources WHERE data_source_id=?)
OR EXISTS(SELECT 1 FROM wiki_pages WHERE knowledge_base_id=?)`,
		source.ID, source.ID, source.ID, source.KnowledgeBaseID).Scan(&used).Error
	if err != nil {
		return "", err
	}
	if used {
		return "revoked", nil
	}
	return "unmanaged", nil
}

func hasSyncJSON(value types.JSON) bool {
	value = bytes.TrimSpace(value)
	return len(value) != 0 && !bytes.Equal(value, []byte("null"))
}
