package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AbortExactExecution closes only this claimed execution's protocol/outbox.
// It grants no read/write access to Knowledge, chunks, vectors or source state.
// An expired owner may close its own abandoned ledger, but can never cancel a
// newer generation or a published version. No lease is extended or reused.
func (r *PersonalRecoveryRepository) AbortExactExecution(
	ctx context.Context, owner *types.FeishuPersonalAttempt, operationID int64,
) error {
	if !r.FirstCreateOnly || owner == nil || owner.ID == "" || owner.OwnerNonce == "" || operationID <= 0 {
		return ErrPersonalFence
	}
	return r.locked(ctx, owner.ID, func(tx *gorm.DB, resource *types.FeishuPersonalResource,
		attempt *types.FeishuPersonalAttempt, now time.Time,
	) error {
		if resource.CurrentAttemptID != owner.ID || resource.ImportID != owner.ImportID ||
			resource.LogicalKnowledgeID != owner.LogicalKnowledgeID ||
			resource.CandidateVersionID != owner.CandidateVersionID ||
			attempt.ExpectedActiveVersion != owner.ExpectedActiveVersion || attempt.AuthMode != types.FeishuUserOAuth ||
			attempt.Fence != owner.Fence ||
			attempt.LogicalKnowledgeID != owner.LogicalKnowledgeID || attempt.ImportID != owner.ImportID ||
			attempt.CallerTenantID != owner.CallerTenantID || attempt.CandidateVersionID != owner.CandidateVersionID ||
			resource.ActiveVersionID != owner.ExpectedActiveVersion {
			return ErrPersonalFence
		}
		var imported types.FeishuPersonalImport
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id,knowledge_id,status").
			Where("id=?", owner.ImportID).Take(&imported).Error; err != nil {
			return err
		}
		if imported.KnowledgeID != owner.LogicalKnowledgeID || imported.Status == "source_managed" {
			return ErrPersonalFence
		}
		var op types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id=? AND tenant_id=? AND scope=? AND scope_id=?", operationID, owner.CallerTenantID,
			types.KnowledgeDispatchScope, owner.LogicalKnowledgeID,
		).Take(&op).Error; err != nil {
			return err
		}
		var dispatch types.KnowledgeDispatch
		if json.Unmarshal(op.Payload, &dispatch) != nil || dispatch.PayloadVersion != 3 ||
			dispatch.Task.PersonalAttemptID != owner.ID || dispatch.Task.KnowledgeID != owner.LogicalKnowledgeID ||
			dispatch.Task.CandidateVersionID != owner.CandidateVersionID || !dispatch.Task.FirstCreate {
			return ErrPersonalIdentity
		}
		if attempt.Status == "needs_attention" && attempt.OwnerNonce == "" &&
			resource.Generation == owner.Fence+1 && op.Op == "attention" {
			return nil // Idempotent acknowledgement of this generation's closure.
		}
		if resource.Generation != owner.Fence || attempt.OwnerNonce != owner.OwnerNonce ||
			attempt.Status != "processing" || op.Op != "processing" {
			return ErrPersonalFence
		}
		resource.Generation++
		attempt.Status, attempt.OwnerNonce, attempt.LeaseUntil = "needs_attention", "", nil
		attempt.LastErrorCategory, attempt.UpdatedAt = "exact_document_execution_requires_review", now
		if err := tx.Save(resource).Error; err != nil {
			return err
		}
		if err := tx.Save(attempt).Error; err != nil {
			return err
		}
		if err := tx.Model(&types.FeishuPersonalImport{}).Where("id=?", attempt.ImportID).
			Update("status", "needs_attention").Error; err != nil {
			return err
		}
		return tx.Model(&op).Update("op", "attention").Error
	})
}
