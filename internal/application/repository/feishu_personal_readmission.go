package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// QueuePersonalReadmission requeues only the original batch admission. It never
// grants execution, resets an attempt or adopts an existing document/output.
// The canonical import preflight/Prepare must subsequently authorize and Claim.
func (r *PersonalRecoveryRepository) QueuePersonalReadmission(
	ctx context.Context, observed *types.FeishuPersonalAttempt, observedResource *types.FeishuPersonalResource,
	batchID, itemID string,
	validate func(*gorm.DB, *types.FeishuPersonalImport, *types.FeishuPersonalAttempt) error,
) error {
	user, ok := types.UserIDFromContext(ctx)
	principal, hasPrincipal := types.PrincipalFromContext(ctx)
	if !r.FirstCreateOnly || observed == nil || observedResource == nil || validate == nil || !ok || !hasPrincipal ||
		principal.Type != types.PrincipalWebUser || principal.ID != user ||
		observed.Status != "needs_attention" || observed.OwnerNonce != "" || observed.LeaseUntil != nil {
		return ErrPersonalIdentity
	}
	return r.locked(ctx, observed.ID, func(tx *gorm.DB, resource *types.FeishuPersonalResource,
		attempt *types.FeishuPersonalAttempt, now time.Time,
	) error {
		if !reflect.DeepEqual(attempt, observed) || !reflect.DeepEqual(resource, observedResource) ||
			resource.CurrentAttemptID != attempt.ID || resource.Generation < attempt.Fence || resource.Generation < 1 ||
			resource.CandidateVersionID != attempt.CandidateVersionID ||
			resource.LogicalKnowledgeID != attempt.LogicalKnowledgeID || resource.ActiveVersionID != "" ||
			attempt.ExpectedActiveVersion != "" ||
			types.CallerFromContext(ctx).TenantID != attempt.CallerTenantID {
			return ErrPersonalFence
		}
		// A source can fail before its first Claim or retire a claimed fetch in
		// the same generation. The entire observed resource and closed ownerless
		// attempt are the admission CAS; this does not issue execution authority.
		var imported types.FeishuPersonalImport
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", attempt.ImportID).
			Take(&imported).Error; err != nil {
			return err
		}
		if imported.UserID != user || imported.KnowledgeID != "" || imported.Status != "needs_attention" ||
			imported.AuthMode != types.FeishuUserOAuth || attempt.AuthMode != types.FeishuUserOAuth {
			return ErrPersonalIdentity
		}
		var batch types.FeishuBatch
		if err := tx.Where("id=? AND owner_id=? AND tenant_id=? AND kb_id=? AND auth_mode=?",
			batchID, user, attempt.CallerTenantID, imported.KBID, types.FeishuUserOAuth).Take(&batch).Error; err != nil {
			return ErrPersonalIdentity
		}
		var item types.FeishuBatchItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id=? AND batch_id=? AND import_id=?", itemID, batchID, imported.ID).
			Take(&item).Error; err != nil {
			return err
		}
		if item.RetryCount >= 3 ||
			(item.Status != "failed" && item.Status != "blocked" && item.Status != "dispatched" &&
				item.Status != "duplicate" && item.Status != "existing") {
			return ErrPersonalFence
		}
		currentIDs := []string{attempt.LogicalKnowledgeID, attempt.CandidateVersionID}
		var history []types.FeishuPersonalAttempt
		if err := tx.Where("import_id=?", imported.ID).Limit(65).Find(&history).Error; err != nil {
			return err
		}
		if len(history) > 64 {
			return ErrPersonalReviewRequired
		}
		ids := append([]string{}, currentIDs...)
		for _, prior := range history {
			if prior.LogicalKnowledgeID != "" {
				ids = append(ids, prior.LogicalKnowledgeID)
			}
			if prior.CandidateVersionID != "" {
				ids = append(ids, prior.CandidateVersionID)
			}
		}
		var count int64
		for _, query := range []*gorm.DB{
			tx.Table("knowledges").Where("id IN ?", ids),
			tx.Table("chunks").Where("knowledge_id IN ?", ids),
			tx.Table("embeddings").Where("knowledge_id IN ?", ids),
			tx.Table("resource_bindings").Where("owner_type='knowledge' AND owner_id IN ?", ids),
			tx.Table("data_sources").Where("id=?", imported.ID),
			tx.Table("feishu_legacy_resources").Where("knowledge_id IN ? OR data_source_id=?", ids, imported.ID),
			tx.Table("task_pending_ops").Where("scope IN ? AND scope_id IN ?",
				[]string{types.PersonalExactReceiptScope, "personal_index_receipt"}, ids),
		} {
			if err := query.Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return ErrPersonalReviewRequired
			}
		}
		var operations []types.TaskPendingOp
		if err := tx.Where("scope=? AND scope_id IN ?", types.KnowledgeDispatchScope, currentIDs).
			Find(&operations).Error; err != nil {
			return err
		}
		if len(operations) != 1 {
			return ErrPersonalReviewRequired
		}
		var intent types.KnowledgeDispatch
		op := operations[0]
		if json.Unmarshal(op.Payload, &intent) != nil || intent.PayloadVersion != 2 ||
			op.TaskType != types.TypeDocumentProcess || op.TenantID != attempt.CallerTenantID ||
			intent.AttemptID != attempt.ID || intent.SourceID != imported.ID ||
			intent.Revision != attempt.Revision || intent.ExternalID != imported.ResourceType+":"+imported.DocumentToken ||
			intent.Task.PersonalAttemptID != attempt.ID || intent.Task.KnowledgeBaseID != imported.KBID ||
			intent.Task.KnowledgeID != attempt.CandidateVersionID ||
			intent.Task.CandidateVersionID != attempt.CandidateVersionID ||
			intent.Task.TenantID != attempt.CallerTenantID || op.ScopeID != attempt.CandidateVersionID ||
			intent.Task.DispatchID != op.DedupKey || op.DedupKey != "personal-"+attempt.ID ||
			intent.Task.URL != "" || intent.Task.FileURL != "" || intent.Task.FilePath != "" ||
			len(intent.Task.Passages) != 0 {
			return ErrPersonalReviewRequired
		}
		if err := validate(tx, &imported, attempt); err != nil {
			return err
		}
		audit, err := json.Marshal(map[string]any{
			"import": imported, "attempt": attempt, "resource": resource, "item": item,
		})
		if err != nil {
			return err
		}
		if err := tx.Create(&types.TaskDeadLetter{
			TenantID: attempt.CallerTenantID, Scope: "feishu_batch", ScopeID: batchID, RelatedID: attempt.ID,
			TaskType: "feishu:readmission_audit", Payload: audit, FailedAt: now,
			LastError: "explicit_owner_retry_before_document_creation", FailCount: item.RetryCount,
		}).Error; err != nil {
			return err
		}
		// These are batch reconciliation leases, not source execution leases.
		// Clearing them invalidates a stale reconciler's CAS after readmission.
		return tx.Model(&item).Updates(map[string]any{
			"status": "pending", "error_category": "", "retry_count": item.RetryCount + 1, "updated_at": now,
			"lease_owner": "", "lease_until": nil,
		}).Error
	})
}
