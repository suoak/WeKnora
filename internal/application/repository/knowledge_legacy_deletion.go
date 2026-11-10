package repository

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// User deletion is a local write operation, never a source publication. Fence
// old executions first and retain a per-document tombstone so scheduled sync
// cannot recreate a file the member explicitly deleted.
func fenceLegacyKnowledgeDeletion(tx *gorm.DB, k *types.Knowledge) error {
	if tx.Name() != "postgres" || !types.LegacyReadGuardsEnabled(tx) || !legacySourceKind(k.Channel) {
		return nil
	}
	var discovered []types.FeishuLegacyResource
	if err := tx.Where("knowledge_id=? AND tenant_id=? AND knowledge_base_id=?",
		k.ID, k.TenantID, k.KnowledgeBaseID).Find(&discovered).Error; err != nil {
		return err
	}
	if len(discovered) == 0 {
		return nil // Historical content that has never entered the source protocol.
	}
	if len(discovered) != 1 {
		return fmt.Errorf("ambiguous source binding during deletion")
	}
	var source types.DataSource
	if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"id=? AND tenant_id=? AND knowledge_base_id=?", discovered[0].DataSourceID,
		k.TenantID, k.KnowledgeBaseID).Take(&source).Error; err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := tx.Model(&types.SyncLog{}).Where(
		"data_source_id=? AND (status IN ? OR execution_claimed=?)", source.ID,
		[]string{"running", "pending"}, true).Updates(map[string]any{
		"status": types.SyncLogStatusCanceled, "execution_claimed": false,
		"execution_generation": gorm.Expr("execution_generation + 1"),
		"finished_at":          now, "error_message": "Document deleted by workspace member",
	}).Error; err != nil {
		return err
	}
	var resource types.FeishuLegacyResource
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
		"id=? AND knowledge_id=? AND data_source_id=? AND tenant_id=? AND knowledge_base_id=?",
		discovered[0].ID, k.ID, source.ID, k.TenantID, k.KnowledgeBaseID).Take(&resource).Error; err != nil {
		return err
	}
	var tombstone map[string]any
	if len(resource.QuarantineSnapshot) > 0 {
		if err := json.Unmarshal(resource.QuarantineSnapshot, &tombstone); err != nil {
			return err
		}
	}
	if tombstone == nil {
		tombstone = make(map[string]any)
	}
	tombstone["deleted_by_user"] = true
	marker, err := json.Marshal(tombstone)
	if err != nil {
		return err
	}
	if err := tx.Model(&types.FeishuLegacyAttempt{}).Where("resource_id=?", resource.ID).
		Updates(map[string]any{"status": "obsolete", "owner_nonce": "", "lease_until": now}).Error; err != nil {
		return err
	}
	return tx.Model(&types.FeishuLegacyResource{}).Where("id=?", resource.ID).Updates(map[string]any{
		"generation": gorm.Expr("generation + 1"), "state": "deleted",
		"current_attempt": "", "active_attempt": "", "quarantine_snapshot": string(marker),
		"updated_at": now,
	}).Error
}
