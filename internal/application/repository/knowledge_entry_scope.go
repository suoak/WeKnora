package repository

import (
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// liveKnowledgeEntries is shared by list totals, KB cards and asset counts.
// Failed and cancelled rows remain entries until explicitly deleted.
func liveKnowledgeEntries(query *gorm.DB, alias string) *gorm.DB {
	return query.Where(alias+"deleted_at IS NULL AND "+alias+"parse_status <> ?", types.ParseStatusDeleting)
}

var documentEntryTypes = []string{"file", "file_url", "manual", "url"}
