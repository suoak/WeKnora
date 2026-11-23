package service

import (
	"errors"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Handoff needs only protocol identifiers. Loading a Knowledge invokes its body
// guard before this first import has a source grant, and must remain forbidden.
// The caller holds the import/resource/attempt locks and verifies its receipt.
func bindFirstImportSourceMetadata(
	tx *gorm.DB, tenant uint64, kb, user string, current *types.FeishuPersonalImport,
) error {
	var row struct {
		ID, FileHash, Importer, ExternalID, DataSourceID string
	}
	if err := tx.Table("knowledges").Clauses(clause.Locking{Strength: "UPDATE"}).
		Select(`id,file_hash,metadata->>'feishu_imported_by' AS importer,
metadata->>'external_id' AS external_id,metadata->>'datasource_id' AS data_source_id`).
		Where("id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL",
			current.KnowledgeID, tenant, kb).Take(&row).Error; err != nil {
		return err
	}
	if row.Importer != user || row.ExternalID != current.ResourceType+":"+current.DocumentToken {
		return access.ErrForbidden
	}
	if row.DataSourceID != "personal:"+current.ID && row.DataSourceID != current.ID {
		return errors.New("source_binding_conflict")
	}
	// Change only the binding key under the row lock; preserve every other
	// metadata field, including concurrent edits committed before this lock.
	value := gorm.Expr("jsonb_set(CAST(metadata AS jsonb),'{datasource_id}',to_jsonb(CAST(? AS text)),true)",
		current.ID)
	if tx.Name() == "sqlite" {
		value = gorm.Expr("json_set(metadata,'$.datasource_id',?)", current.ID)
	}
	change := tx.Model(&types.Knowledge{}).
		Where("id=? AND tenant_id=? AND knowledge_base_id=? AND file_hash=?", row.ID, tenant, kb, row.FileHash).
		Where("metadata->>'datasource_id'=? AND metadata->>'external_id'=? AND metadata->>'feishu_imported_by'=?",
			row.DataSourceID, row.ExternalID, user).Update("metadata", value)
	if change.Error != nil {
		return change.Error
	}
	if change.RowsAffected != 1 {
		return errors.New("source_binding_changed")
	}
	return nil
}
