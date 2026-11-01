package repository

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReviewExactRetry serializes admission with all normal ownership transitions.
// The callback must recheck source/KB authority and the exact staged snapshot;
// it may persist a durable intent, but cannot create an execution owner here.
func (r *PersonalRecoveryRepository) ReviewExactRetry(
	ctx context.Context, observed *types.FeishuPersonalAttempt, sealed int64,
	validate func(*gorm.DB, *types.FeishuPersonalAttempt) error,
) error {
	if !r.FirstCreateOnly || observed == nil || validate == nil || sealed != observed.Fence+1 {
		return ErrPersonalFence
	}
	return r.locked(ctx, observed.ID, func(tx *gorm.DB, resource *types.FeishuPersonalResource,
		attempt *types.FeishuPersonalAttempt, _ time.Time,
	) error {
		if !reflect.DeepEqual(attempt, observed) || !reviewedExactRetryState(resource, attempt, sealed) {
			return ErrPersonalFence
		}
		var imported types.FeishuPersonalImport
		if err := tx.Where("id=?", attempt.ImportID).Take(&imported).Error; err != nil {
			return err
		}
		if imported.KnowledgeID != resource.LogicalKnowledgeID || imported.Status == "source_managed" {
			return ErrPersonalIdentity
		}
		if err := r.lockPrincipal(tx, &imported, attempt); err != nil {
			return err
		}
		return validate(tx, attempt)
	})
}

func reviewedExactRetryState(
	resource *types.FeishuPersonalResource, attempt *types.FeishuPersonalAttempt, sealed int64,
) bool {
	return resource.CurrentAttemptID == attempt.ID && resource.ImportID == attempt.ImportID &&
		resource.LogicalKnowledgeID == attempt.LogicalKnowledgeID && resource.Generation == sealed &&
		resource.CandidateVersionID == attempt.CandidateVersionID && resource.ActiveVersionID == "" &&
		attempt.ExpectedActiveVersion == "" && attempt.AuthMode == types.FeishuUserOAuth &&
		attempt.Fence+1 == sealed && attempt.Status == "needs_attention" &&
		attempt.OwnerNonce == "" && attempt.LeaseUntil == nil
}

// ClaimReviewedExactRetry mints a new generation/nonce. Quarantine and all
// validation run in this same transaction: any refusal rolls back ownership,
// the original outbox, staged rows and the archive together.
func (r *PersonalRecoveryRepository) ClaimReviewedExactRetry(
	ctx context.Context, observed *types.FeishuPersonalAttempt, ticket *types.TaskPendingOp,
	lease time.Duration, validate func(*gorm.DB, *types.FeishuPersonalAttempt) error,
	activate func(*gorm.DB, *types.FeishuPersonalAttempt) error,
) (*types.FeishuPersonalAttempt, error) {
	if observed == nil || ticket == nil || validate == nil || activate == nil || lease <= 0 || lease > 5*time.Minute {
		return nil, ErrPersonalFence
	}
	var owner types.FeishuPersonalAttempt
	err := r.ReviewExactRetry(ctx, observed, observed.Fence+1, func(
		tx *gorm.DB, attempt *types.FeishuPersonalAttempt,
	) error {
		var locked types.TaskPendingOp
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id=?", ticket.ID).Take(&locked).Error; err != nil {
			return err
		}
		var intent types.PersonalExactRetry
		if !reflect.DeepEqual(locked, *ticket) ||
			(locked.Op != "pending" && locked.Op != "dispatched") ||
			locked.Scope != types.PersonalExactRetryScope || locked.TenantID != attempt.CallerTenantID ||
			json.Unmarshal(locked.Payload, &intent) != nil || intent.Version != 1 ||
			intent.SealedGeneration != attempt.Fence+1 || intent.Source.AttemptID != attempt.ID ||
			intent.Source.KnowledgeID != attempt.LogicalKnowledgeID ||
			intent.Task.ExactRetryTicketID != locked.ID {
			return ErrPersonalFence
		}
		var now time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
			return err
		}
		if !intent.NotAfter.After(now) {
			return ErrPersonalFence
		}
		// Source preflight describes the sealed snapshot. Check it before
		// minting the replacement owner, while all authority rows are locked.
		if err := validate(tx, attempt); err != nil {
			return err
		}
		until := now.Add(lease)
		attempt.Fence = intent.SealedGeneration + 1
		attempt.OwnerNonce, attempt.LeaseUntil = uuid.NewString(), &until
		attempt.Status, attempt.LastErrorCategory, attempt.UpdatedAt = "processing", "", now
		changed := tx.Model(&types.FeishuPersonalResource{}).Where(
			"import_id=? AND generation=? AND current_attempt_id=?",
			attempt.ImportID, intent.SealedGeneration, attempt.ID,
		).Update("generation", attempt.Fence)
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrPersonalFence
		}
		if err := tx.Save(attempt).Error; err != nil {
			return err
		}
		if err := activate(tx, attempt); err != nil {
			return err
		}
		if err := tx.Model(&locked).Update("op", "claimed").Error; err != nil {
			return err
		}
		owner = *attempt
		return nil
	})
	return &owner, err
}
