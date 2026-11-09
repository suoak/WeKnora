package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Close the execution protocol before deleting any chunks/files. Lock ordering
// matches Claim/heartbeat/publication: datasource (if any), resource, attempt,
// then knowledge. Old
// workers cannot republish after the deletion marker commits. Protocol history
// is retained; a source-content proof is neither needed nor issued here.
func fencePersonalKnowledgeDeletion(tx *gorm.DB, k *types.Knowledge) error {
	var header struct{ Channel string }
	if err := tx.Table("knowledges").Select("channel").Where(
		"id=? AND tenant_id=? AND knowledge_base_id=? AND deleted_at IS NULL",
		k.ID, k.TenantID, k.KnowledgeBaseID).Take(&header).Error; err != nil {
		return err
	}
	if header.Channel != "feishu_personal" {
		return nil
	}
	var imports []struct{ ID, KBID, Status, URL string }
	if err := tx.Table("feishu_personal_imports").Select("id,kb_id,status,url").
		Where("knowledge_id=?", k.ID).Find(&imports).Error; err != nil {
		return err
	}
	if len(imports) != 1 || imports[0].KBID != k.KnowledgeBaseID {
		return fmt.Errorf("personal deletion binding unverified")
	}
	m := imports[0]
	// Source execution/publication takes this lock first. Even an import not yet
	// handed off may have a paused source prepared by EnableImportSource.
	var source types.DataSource
	sourceErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id=?", m.ID).First(&source).Error
	if sourceErr != nil && !errors.Is(sourceErr, gorm.ErrRecordNotFound) {
		return sourceErr
	}
	var resource types.FeishuPersonalResource
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("import_id=?", m.ID).
		Take(&resource).Error; err != nil {
		return err
	}
	if resource.LogicalKnowledgeID != k.ID {
		return fmt.Errorf("personal deletion identity changed")
	}
	var attempt types.FeishuPersonalAttempt
	if resource.CurrentAttemptID != "" {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", resource.CurrentAttemptID).
			Take(&attempt).Error; err != nil {
			return err
		}
	}
	// Do not decide from the pre-lock discovery snapshot: a datasource handoff
	// may have completed while we waited for the resource lock.
	if err := tx.Table("feishu_personal_imports").Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id,kb_id,status,url").Where("id=? AND knowledge_id=? AND kb_id=?", m.ID, k.ID, k.KnowledgeBaseID).
		Take(&m).Error; err != nil {
		return err
	}
	if m.Status == "deleted" && resource.CurrentAttemptID == "" &&
		resource.ActiveVersionID == "" && resource.CandidateVersionID == "" {
		return nil // A previous cleanup attempt fenced this exact document.
	}
	if attempt.ImportID != m.ID || attempt.LogicalKnowledgeID != k.ID ||
		attempt.CallerTenantID != k.TenantID {
		return fmt.Errorf("personal deletion attempt unverified")
	}
	if sourceErr == nil || m.Status == "source_managed" {
		if sourceErr != nil || source.TenantID != k.TenantID || source.KnowledgeBaseID != k.KnowledgeBaseID ||
			!legacySourceKind(source.Type) {
			return fmt.Errorf("personal deletion source binding unverified")
		}
		// This path retires only the dedicated, single-link handoff source before
		// legacy enrollment. Enrolled/shared sources need their own coordinated
		// deletion protocol and must never be silently unprotected here.
		var config struct {
			ResourceIDs []string `json:"resource_ids"`
			Settings    struct {
				Links []string `json:"links"`
			} `json:"settings"`
		}
		if json.Unmarshal(source.Config, &config) != nil || len(config.ResourceIDs) != 1 ||
			config.ResourceIDs[0] != "links" || len(config.Settings.Links) != 1 ||
			m.URL == "" || config.Settings.Links[0] != m.URL {
			return fmt.Errorf("personal deletion requires dedicated link source")
		}
		var unsafe int64
		for _, query := range []*gorm.DB{
			tx.Table("sync_logs").Where("data_source_id=? AND (status IN ? OR execution_claimed=?)",
				m.ID, []string{"running", "pending"}, true),
			tx.Table("feishu_legacy_resources").Where("data_source_id=? OR knowledge_id=?", m.ID, k.ID),
			tx.Table("feishu_personal_imports").Where("id<>? AND knowledge_id=?", m.ID, k.ID),
		} {
			if err := query.Count(&unsafe).Error; err != nil {
				return err
			}
			if unsafe != 0 {
				return fmt.Errorf("personal deletion source is busy, shared or enrolled")
			}
		}
		var bindings []struct{ ID string }
		bindingSQL := "json_extract(metadata,'$.datasource_id')=?"
		if tx.Name() == "postgres" {
			bindingSQL = "metadata::jsonb->>'datasource_id'=?"
		}
		if err := tx.Table("knowledges").Select("id").Where("deleted_at IS NULL").
			Where(bindingSQL, m.ID).Scan(&bindings).Error; err != nil {
			return err
		}
		if len(bindings) != 1 || bindings[0].ID != k.ID || attempt.Status != "obsolete" ||
			attempt.OwnerNonce != "" || attempt.LeaseUntil != nil || resource.ActiveVersionID != "" {
			return fmt.Errorf("personal deletion handoff state unverified")
		}
		// Retain source configuration, cursor and history; prevent scheduler or
		// delayed messages from re-enrolling and recreating this deleted file.
		if err := tx.Model(&types.DataSource{}).Where("id=?", source.ID).Updates(map[string]any{
			"status": types.DataSourceStatusPaused, "deleted_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
	}
	resource.Generation++
	resource.CurrentAttemptID, resource.ActiveVersionID, resource.CandidateVersionID = "", "", ""
	if err := tx.Save(&resource).Error; err != nil {
		return err
	}
	if err := tx.Model(&types.FeishuPersonalAttempt{}).Where("import_id=?", m.ID).
		Updates(map[string]any{
			"status": "obsolete", "owner_nonce": "", "lease_until": nil,
			"last_error_category": "deleted_by_user", "updated_at": time.Now(),
		}).Error; err != nil {
		return err
	}
	if err := tx.Table("feishu_personal_imports").Where("id=? AND knowledge_id=?", m.ID, k.ID).
		Updates(map[string]any{"status": "deleted", "updated_at": time.Now()}).Error; err != nil {
		return err
	}
	return tx.Table("task_pending_ops").Where("tenant_id=? AND scope_id=? AND scope IN ?",
		k.TenantID, k.ID, []string{types.KnowledgeDispatchScope, types.PersonalExactReceiptScope}).
		Update("op", "obsolete").Error
}
