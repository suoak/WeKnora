package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PersonalExactResumeMaxAttempts retains a cumulative bounded budget, including
// compatibility retries for original retained-output and embedded-image resumes.
// The cumulative count is retained; no task or attempt counter is reset.
const PersonalExactResumeMaxAttempts = 5

// PersonalExactResumeLimit grants one extra cumulative attempt only to IDs in
// the server-owned incident review list. HTTP payloads cannot change this list,
// counters are never reset, and all source/ownership checks still run.
func PersonalExactResumeLimit(knowledge string) int {
	if knowledge != "" {
		for _, id := range strings.Split(os.Getenv("FEISHU_PERSONAL_EXACT_REVIEWED_RESUME_KNOWLEDGE_IDS"), ",") {
			if strings.TrimSpace(id) == knowledge {
				return PersonalExactResumeMaxAttempts + 1
			}
		}
	}
	return PersonalExactResumeMaxAttempts
}

// PersonalExactResumeBudget admits one reviewed retry at an exact historical
// counter value. Old preflight deferrals used the execution counter, so some
// documents already exceed the normal budget without having executed once.
// A stale baseline cannot authorize another retry or reset that history.
func PersonalExactResumeBudget(knowledge string, count int) int {
	limit := PersonalExactResumeLimit(knowledge)
	if knowledge == "" || count < PersonalExactResumeMaxAttempts || count > 10000 {
		return limit
	}
	raw := os.Getenv("FEISHU_PERSONAL_EXACT_REVIEWED_RESUME_BASELINES")
	if len(raw) > 16<<10 {
		return limit
	}
	var baselines map[string]int
	if json.Unmarshal([]byte(raw), &baselines) != nil || len(baselines) > 32 {
		return limit
	}
	if baseline, ok := baselines[knowledge]; ok && baseline == count {
		return count + 1
	}
	return limit
}

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
		observed.LeaseUntil != nil || observed.RetryCount >= PersonalExactResumeBudget(
		observed.LogicalKnowledgeID, observed.RetryCount) ||
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
		receiptErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"scope=? AND scope_id=? AND tenant_id=?", types.PersonalExactReceiptScope,
			source.KnowledgeID, source.TenantID).Take(&receipt).Error
		missingReceipt := errors.Is(receiptErr, gorm.ErrRecordNotFound)
		if missingReceipt {
			// Claim can stop before staging is established. Only a freshly
			// verified source with absolutely no output may create its first
			// receipt here. Never infer ownership of retained partial output.
			var count int64
			for _, table := range []string{"chunks", "embeddings"} {
				if err := tx.Table(table).Where("knowledge_id=?", source.KnowledgeID).Count(&count).Error; err != nil {
					return err
				}
				if count != 0 {
					return ErrPersonalIdentity
				}
			}
			if err := exactResumeEmptyDerived(tx, source); err != nil {
				return err
			}
			if err := tx.Model(&types.TaskPendingOp{}).Where("scope=? AND scope_id=?",
				types.PersonalExactReceiptScope, source.KnowledgeID).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return ErrPersonalIdentity
			}
			body, err := json.Marshal(source)
			if err != nil {
				return err
			}
			receipt = types.TaskPendingOp{
				TenantID: source.TenantID, Scope: types.PersonalExactReceiptScope,
				ScopeID: source.KnowledgeID, TaskType: "knowledge:exact_receipt", Op: "staging",
				DedupKey: fmt.Sprintf("exact-%s-%d", source.AttemptID, source.Generation),
				Payload:  body, EnqueuedAt: now,
			}
			if err := tx.Create(&receipt).Error; err != nil {
				return err
			}
		} else if receiptErr != nil {
			return receiptErr
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
			"receipt_missing_before_resume": missingReceipt,
			"reviewed_extra_attempt":        attempt.RetryCount >= PersonalExactResumeMaxAttempts,
			"reviewed_counter_baseline":     attempt.RetryCount,
			"reviewed_cumulative_limit":     PersonalExactResumeBudget(attempt.LogicalKnowledgeID, attempt.RetryCount),
		})
		if err != nil {
			return err
		}
		auditReason := "original_staging_retained_before_reviewed_resume"
		if missingReceipt {
			auditReason = "source_reproved_before_first_staging_resume"
		}
		if err := tx.Create(&types.TaskDeadLetter{
			TenantID: source.TenantID, TaskType: "knowledge:exact_resume_audit",
			Scope: types.TaskScopeKnowledge, ScopeID: source.KnowledgeID,
			RelatedID: attempt.ID, Payload: audit, FailedAt: now,
			LastError: auditReason,
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
