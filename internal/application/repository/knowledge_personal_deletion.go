package repository

import (
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Close the execution protocol before deleting any chunks/files. Lock ordering
// matches Claim/heartbeat/publication: resource, attempt, then knowledge. Old
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
	var imports []struct{ ID, KBID, Status string }
	if err := tx.Table("feishu_personal_imports").Select("id,kb_id,status").
		Where("knowledge_id=?", k.ID).Find(&imports).Error; err != nil {
		return err
	}
	if len(imports) != 1 || imports[0].KBID != k.KnowledgeBaseID {
		return fmt.Errorf("personal deletion binding unverified")
	}
	m := imports[0]
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
		Select("id,kb_id,status").Where("id=? AND knowledge_id=? AND kb_id=?", m.ID, k.ID, k.KnowledgeBaseID).
		Take(&m).Error; err != nil {
		return err
	}
	if m.Status == "deleted" && resource.CurrentAttemptID == "" &&
		resource.ActiveVersionID == "" && resource.CandidateVersionID == "" {
		return nil // A previous cleanup attempt fenced this exact document.
	}
	if attempt.ImportID != m.ID || attempt.LogicalKnowledgeID != k.ID ||
		attempt.CallerTenantID != k.TenantID || m.Status == "source_managed" {
		return fmt.Errorf("personal deletion attempt unverified")
	}
	resource.Generation++
	resource.CurrentAttemptID, resource.ActiveVersionID, resource.CandidateVersionID = "", "", ""
	if err := tx.Save(&resource).Error; err != nil {
		return err
	}
	if err := tx.Model(&types.FeishuPersonalAttempt{}).Where("import_id=?", m.ID).
		Updates(map[string]any{"status": "obsolete", "owner_nonce": "", "lease_until": nil,
			"last_error_category": "deleted_by_user", "updated_at": time.Now()}).Error; err != nil {
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
