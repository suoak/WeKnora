package repository

import (
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Runs under Prepare's canonical resource lock and in its admission transaction.
// History principals remain intact; existing worker/publication fences still
// require the current import principal AND current resource generation.
func (r *PersonalRecoveryRepository) handoffUnpublished(tx *gorm.DB, previous, incoming *types.FeishuPersonalImport, next *types.FeishuPersonalAttempt) error {
	if r.UnpublishedHandoffValidator == nil || previous.UserID != incoming.UserID ||
		previous.AuthMode != types.FeishuApplicationGrant || incoming.AuthMode != types.FeishuUserOAuth ||
		incoming.OpenID == "" || incoming.ApplicationID != "" || next.AuthMode != types.FeishuUserOAuth ||
		next.GrantEpoch == "" || next.ApplicationID != "" || !tx.Migrator().HasTable("feishu_import_principal_history") {
		return ErrPersonalAuthModeConflict
	}
	var resource types.FeishuPersonalResource
	var old types.FeishuPersonalAttempt
	var current types.FeishuPersonalImport
	lock := clause.Locking{Strength: "UPDATE"}
	if err := tx.Clauses(lock).Where("import_id=?", previous.ID).First(&resource).Error; err != nil {
		return err
	}
	if err := tx.Clauses(lock).Where("id=?", resource.CurrentAttemptID).First(&old).Error; err != nil {
		return err
	}
	if err := tx.Clauses(lock).Where("id=?", previous.ID).First(&current).Error; err != nil {
		return err
	}
	if current.UserID != incoming.UserID || current.AppID != incoming.AppID || current.KBID != incoming.KBID ||
		current.DocumentToken != incoming.DocumentToken || current.ResourceType != incoming.ResourceType ||
		current.AuthMode != types.FeishuApplicationGrant || current.ApplicationID != incoming.AppID ||
		current.Status != "needs_attention" || current.KnowledgeID != "" || current.SyncLease != "" || resource.ActiveVersionID != "" ||
		old.AuthMode != types.FeishuApplicationGrant || old.ImportID != current.ID || old.Status != "needs_attention" ||
		old.ExpectedActiveVersion != "" || old.OwnerNonce != "" || old.LeaseUntil != nil ||
		old.LogicalKnowledgeID != resource.LogicalKnowledgeID || old.CandidateVersionID != resource.CandidateVersionID ||
		next.CallerTenantID != old.CallerTenantID {
		return ErrPersonalAuthModeConflict
	}
	// No partial publication or other live historical execution may be hidden.
	var count int64
	if err := tx.Model(&types.FeishuPersonalAttempt{}).Where("import_id=? AND (status NOT IN ? OR owner_nonce<>'' OR lease_until IS NOT NULL)", current.ID,
		[]string{"needs_attention", "obsolete"}).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrPersonalReviewRequired
	}
	for _, table := range []string{"knowledges", "chunks", "embeddings"} {
		field := "knowledge_id"
		if table == "knowledges" {
			field = "id"
		}
		if err := tx.Table(table).Where(field+"=? OR "+field+" IN (SELECT candidate_version_id FROM feishu_personal_attempts WHERE import_id=?)",
			resource.LogicalKnowledgeID, current.ID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrPersonalReviewRequired
		}
	}
	if err := tx.Model(&types.TaskPendingOp{}).Where("scope=? AND scope_id IN (SELECT candidate_version_id FROM feishu_personal_attempts WHERE import_id=?) AND (claimed_at IS NOT NULL OR op NOT IN ?)",
		types.KnowledgeDispatchScope, current.ID, []string{"attention", "obsolete", "completed"}).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrPersonalBusy
	}
	if err := r.UnpublishedHandoffValidator(tx, &current, incoming, next); err != nil {
		return err
	}
	if err := r.lockPrincipal(tx, incoming, next); err != nil {
		return err
	}
	// The history trigger records the new tuple; the old attempts' FK continues
	// referencing the old tuple. No attempt identity is rewritten or removed.
	if err := tx.Model(&current).Updates(map[string]any{"auth_mode": incoming.AuthMode,
		"application_id": "", "open_id": incoming.OpenID, "updated_at": time.Now()}).Error; err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{"old_attempt_id": old.ID, "logical_knowledge_id": resource.LogicalKnowledgeID,
		"generation": resource.Generation, "from": "application_grant", "to": "user_oauth", "revision": next.Revision,
		"app_config_version": next.AppConfigVersion, "grant_epoch": next.GrantEpoch})
	if err != nil {
		return err
	}
	return tx.Create(&types.AuditLog{TenantID: next.CallerTenantID, ActorUserID: incoming.UserID,
		Action: "feishu.unpublished_principal_handoff", TargetType: "feishu_import", TargetID: current.ID,
		ScopeType: "knowledge_base", ScopeID: current.KBID, Outcome: "success", Details: types.JSON(details), CreatedAt: time.Now()}).Error
}
