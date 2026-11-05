package repository

import (
	"context"
	"reflect"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// RecordPreflightDeferral records an observation, never an execution grant.
// Resource/attempt CAS prevents an old probe from changing a new generation,
// a claimed owner, a published receipt, or its durable dispatch operation.
func (r *PersonalRecoveryRepository) RecordPreflightDeferral(
	ctx context.Context, observed *types.FeishuPersonalAttempt, category string,
) error {
	if observed == nil {
		return ErrPersonalFence
	}
	switch category {
	case "embedded_dependency_unsupported", "source_verification_required",
		"authorization_revalidation_failed", "preflight_transient":
	default:
		return ErrPersonalIdentity
	}
	return r.locked(ctx, observed.ID, func(
		tx *gorm.DB, resource *types.FeishuPersonalResource, a *types.FeishuPersonalAttempt, now time.Time,
	) error {
		if !reflect.DeepEqual(a, observed) || a.Status != "index_pending" ||
			a.OwnerNonce != "" || a.LeaseUntil != nil || resource.CurrentAttemptID != a.ID ||
			resource.Generation != a.Fence || resource.CandidateVersionID != a.CandidateVersionID ||
			resource.ActiveVersionID != a.ExpectedActiveVersion {
			return ErrPersonalFence
		}
		count := min(max(a.RetryCount, 0)+1, 1000)
		delay := min(15*time.Minute, time.Minute*time.Duration(1<<min(count-1, 4)))
		next := now.Add(delay)
		return tx.Model(a).Updates(map[string]any{
			"retry_count": count, "retry_after": next, "last_error_category": category,
		}).Error
	})
}
