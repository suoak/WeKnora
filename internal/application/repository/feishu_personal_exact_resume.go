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

// PersonalExactResumeMaxAttempts retains a cumulative bounded budget, including
// one compatibility retry after the original three retained-output attempts.
const PersonalExactResumeMaxAttempts = 4

// QueueExactResume preserves the original dispatch, file and receipt IDs.
// It grants no body/model authority. A new owner must rederive and compare all
// old parser output before reindexing or publishing any candidate content.
func (r *PersonalRecoveryRepository) QueueExactResume(
	ctx context.Context, observed *types.FeishuPersonalAttempt,
	source types.PersonalExactReceipt,
	validate func(*gorm.DB, *types.FeishuPersonalAttempt) error,
) error {
	if !r.FirstCreateOnly || observed == nil || validate == nil ||
		observed.Status != "needs_attention" || observed.OwnerNonce != "" ||
		observed.LeaseUntil != nil || observed.RetryCount >= PersonalExactResumeMaxAttempts ||
		observed.LastErrorCategory != "exact_document_execution_requires_review" ||
		source.AttemptID != observed.ID || source.Generation != observed.Fence {
		return ErrPersonalFence
	}
	return r.locked(ctx, observed.ID, func(tx *gorm.DB, resource *types.FeishuPersonalResource,
		attempt *types.FeishuPersonalAttempt, now time.Time,
	) error {
		if !reflect.DeepEqual(attempt, observed) ||
			resource.CurrentAttemptID != observed.ID ||
			resource.Generation != observed.Fence+1 ||
			resource.ActiveVersionID != "" || observed.ExpectedActiveVersion != "" ||
			resource.CandidateVersionID != observed.CandidateVersionID ||
			resource.LogicalKnowledgeID != observed.LogicalKnowledgeID {
			return ErrPersonalFence
		}
		if err := CheckExactResumeCapacity(tx, attempt.ID, true); err != nil {
			return err
		}
		var imported types.FeishuPersonalImport
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id=?", attempt.ImportID).Take(&imported).Error; err != nil {
			return err
		}
		if imported.Status != "needs_attention" || imported.KnowledgeID != source.KnowledgeID ||
			imported.KBID != source.KnowledgeBaseID || attempt.CallerTenantID != source.TenantID {
			return ErrPersonalIdentity
		}
		if err := r.lockPrincipal(tx, &imported, attempt); err != nil {
			return err
		}
		if err := validate(tx, attempt); err != nil {
			return err
		}
		var receipt, dispatch types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"scope=? AND scope_id=? AND tenant_id=?", types.PersonalExactReceiptScope,
			source.KnowledgeID, source.TenantID).Take(&receipt).Error; err != nil {
			return err
		}
		var previous types.PersonalExactReceipt
		if (receipt.Op != "staging" && receipt.Op != "resuming") ||
			json.Unmarshal(receipt.Payload, &previous) != nil {
			return ErrPersonalIdentity
		}
		if receipt.Op == "resuming" && previous.Generation+1 == source.Generation {
			// A worker lost before activation wrote no new derived output.
			previous.Generation = source.Generation
		}
		if previous != source {
			return ErrPersonalIdentity
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"scope=? AND scope_id=? AND tenant_id=?", types.KnowledgeDispatchScope,
			source.KnowledgeID, source.TenantID).Take(&dispatch).Error; err != nil {
			return err
		}
		var intent types.KnowledgeDispatch
		if dispatch.Op != "attention" || json.Unmarshal(dispatch.Payload, &intent) != nil ||
			intent.PayloadVersion != 3 || intent.Task.PersonalAttemptID != attempt.ID ||
			intent.Task.KnowledgeID != source.KnowledgeID ||
			intent.Task.CandidateVersionID != source.CandidateVersionID || !intent.Task.FirstCreate {
			return ErrPersonalIdentity
		}
		if err := exactResumeEmptyDerived(tx, source); err != nil {
			return err
		}
		audit, err := json.Marshal(map[string]any{
			"attempt": attempt, "resource": resource, "receipt": receipt, "dispatch": dispatch,
		})
		if err != nil {
			return err
		}
		if err := tx.Create(&types.TaskDeadLetter{
			TenantID: source.TenantID, TaskType: "knowledge:exact_resume_audit",
			Scope: types.TaskScopeKnowledge, ScopeID: source.KnowledgeID,
			RelatedID: attempt.ID, Payload: audit, FailedAt: now,
			LastError: "original_staging_retained_before_reviewed_resume",
			FailCount: attempt.RetryCount,
		}).Error; err != nil {
			return err
		}
		attempt.Fence = resource.Generation
		attempt.Status, attempt.LastErrorCategory = "index_pending", "exact_document_resume_prepared"
		attempt.RetryCount++
		attempt.RetryAfter, attempt.UpdatedAt = nil, now
		if err := tx.Save(attempt).Error; err != nil {
			return err
		}
		source.Generation = resource.Generation
		payload, err := json.Marshal(source)
		if err != nil {
			return err
		}
		if err := tx.Model(&receipt).Updates(map[string]any{
			"op": "resuming", "payload": json.RawMessage(payload),
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&dispatch).Update("op", "pending").Error; err != nil {
			return err
		}
		return tx.Model(&imported).Update("status", "index_pending").Error
	})
}

// CheckExactResumeCapacity reserves two recovery slots independently of ordinary imports.
// Ignore old queued intents for admission; the same fence is checked at Claim.
func CheckExactResumeCapacity(tx *gorm.DB, attempt string, includeQueued bool) error {
	if tx.Name() != "postgres" {
		return ErrPersonalFence
	}
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(
hashtextextended('personal-exact-resume-admission',0))`).Error; err != nil {
		return err
	}
	var count int64
	query := tx.Model(&types.FeishuPersonalAttempt{}).Where(
		"id<>? AND last_error_category=?", attempt, "exact_document_resume_prepared")
	if includeQueued {
		query = query.Where(`(status='processing' AND lease_until>clock_timestamp()) OR
(status='index_pending' AND updated_at>clock_timestamp()-interval '15 minutes')`)
	} else {
		query = query.Where("status='processing' AND lease_until>clock_timestamp()")
	}
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count >= 2 {
		return ErrPersonalBusy
	}
	return nil
}

func exactResumeEmptyDerived(tx *gorm.DB, source types.PersonalExactReceipt) error {
	var safe bool
	err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM knowledges k
WHERE k.id=? AND k.tenant_id=? AND k.knowledge_base_id=? AND k.deleted_at IS NULL
AND k.channel='feishu_personal' AND k.parse_status IN ('pending','processing')
AND k.enable_status='disabled' AND COALESCE(k.description,'')=''
AND (k.profile IS NULL OR k.profile::text='null')
AND (k.custom_metadata IS NULL OR k.custom_metadata::text IN ('{}','null'))
AND NOT EXISTS(SELECT 1 FROM chunks c WHERE c.knowledge_id=k.id AND
 (c.chunk_type NOT IN ('text','parent_text') OR c.is_enabled OR c.deleted_at IS NOT NULL
 OR c.content_revision<>0))
AND NOT EXISTS(SELECT 1 FROM embeddings e WHERE e.knowledge_id=k.id AND e.is_enabled))`,
		source.KnowledgeID, source.TenantID, source.KnowledgeBaseID).Scan(&safe).Error
	if err != nil {
		return err
	}
	if !safe {
		return ErrPersonalIdentity
	}
	return nil
}

// ActivateExactResume consumes only the preceding queued resume generation.
// Current principal/Grant Epoch and the actual Claim are checked by Mutate.
func (r *PersonalRecoveryRepository) ActivateExactResume(
	ctx context.Context, owner *types.FeishuPersonalAttempt, source types.PersonalExactReceipt,
) error {
	return r.Mutate(ctx, owner, 0, func(tx *gorm.DB, current *types.FeishuPersonalAttempt) error {
		var receipt types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"scope=? AND scope_id=? AND tenant_id=?", types.PersonalExactReceiptScope,
			source.KnowledgeID, source.TenantID).Take(&receipt).Error; err != nil {
			return err
		}
		var queued types.PersonalExactReceipt
		if receipt.Op != "resuming" || json.Unmarshal(receipt.Payload, &queued) != nil ||
			queued.Generation+1 != current.Fence || source.Generation != current.Fence {
			return ErrPersonalFence
		}
		queued.Generation = current.Fence
		if queued != source {
			return ErrPersonalIdentity
		}
		if err := exactResumeEmptyDerived(tx, source); err != nil {
			return err
		}
		payload, err := json.Marshal(source)
		if err != nil {
			return err
		}
		return tx.Model(&receipt).Updates(map[string]any{
			"op": "staging", "payload": json.RawMessage(payload),
		}).Error
	})
}
