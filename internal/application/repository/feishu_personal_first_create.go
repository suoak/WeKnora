package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Separate from Claim's private-fetch terminal boundary. It never changes a
// candidate into public Knowledge and never allocates a new logical ID.
func (r *PersonalRecoveryRepository) ClaimFirstCreate(ctx context.Context, id string, lease time.Duration) (*types.FeishuPersonalAttempt, error) {
	var owner types.FeishuPersonalAttempt
	err := r.locked(ctx, id, func(tx *gorm.DB, res *types.FeishuPersonalResource, a *types.FeishuPersonalAttempt, now time.Time) error {
		if !r.FirstCreateOnly || res.CurrentAttemptID != a.ID || a.Status != "awaiting_index_isolation" || res.ActiveVersionID != a.ExpectedActiveVersion || a.ManifestHash == "" {
			return ErrPersonalFence
		}
		if a.RetryAfter != nil && a.RetryAfter.After(now) {
			return ErrPersonalBusy
		}
		res.Generation++
		a.Fence = res.Generation
		a.OwnerNonce = uuid.NewString()
		until := now.Add(lease)
		a.LeaseUntil = &until
		a.Status = "processing"
		if err := tx.Save(res).Error; err != nil {
			return err
		}
		if err := tx.Save(a).Error; err != nil {
			return err
		}
		owner = *a
		return nil
	})
	return &owner, err
}
