package service

import (
	"context"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errBatchPublicationExpired = errors.New("publication_confirmation_expired")

// Compatibility for old batch callers: historic approval IDs confer no write
// authority and are not propagated into the current authorization context.
func batchPublicationContext(ctx context.Context, _ *types.FeishuBatchItem) context.Context {
	return ctx
}

// Lock live authority until the intent/grant is committed. Ingestion still
// revalidates its own source permission, revision, protection and publication fences.
func lockBatchPublicationAuthority(tx *gorm.DB, b *types.FeishuBatch) error {
	lock := clause.Locking{Strength: "SHARE"}
	var user types.User
	var tenant types.Tenant
	var member types.TenantMember
	var kb types.KnowledgeBase
	if err := tx.Clauses(lock).Where("id = ? AND is_active = ?", b.OwnerID, true).First(&user).Error; err != nil {
		return access.ErrForbidden
	}
	if err := tx.Clauses(lock).Where("id = ? AND status = ?", b.TenantID, "active").First(&tenant).Error; err != nil {
		return access.ErrForbidden
	}
	if err := tx.Clauses(lock).Where("user_id = ? AND tenant_id = ? AND status = ?",
		b.OwnerID, b.TenantID, types.TenantMemberStatusActive).First(&member).Error; err != nil {
		return access.ErrForbidden
	}
	if !member.Role.HasPermission(types.TenantRoleContributor) {
		return access.ErrForbidden
	}
	if err := tx.Clauses(lock).Where("id = ? AND tenant_id = ?", b.KBID, b.TenantID).First(&kb).Error; err != nil {
		return access.ErrForbidden
	}
	if kb.CreatorID != b.OwnerID && !member.Role.HasPermission(types.TenantRoleAdmin) {
		return access.ErrForbidden
	}
	return nil
}

// The click grants only the submitted resources' first verified versions. Grant
// creation and version binding are atomic, so crashes/retries can never silently
// approve another revision or replace a revoked grant. No content is read here.
func (s *FeishuPersonalService) bindBatchPublication(
	ctx context.Context, b *types.FeishuBatch, item *types.FeishuBatchItem,
) error {
	// Do not manufacture a sharing approval from an import click. Destination
	// authority is checked live; source permission/revision checks follow normally.
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.FeishuBatchItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND batch_id = ? AND status = ? AND lease_owner = ? AND lease_until > ?",
				item.ID, b.ID, "processing", item.LeaseOwner, time.Now()).First(&current).Error; err != nil {
			return err
		}
		return s.requireImportWriter(tx, b.OwnerID, b.KBID, b.TenantID)
	})
}

// ContinueBatch combines explicit publication intent and requeue for an old
// blocked batch. It never changes a bound revision, existing attempt, source,
// cursor or retry budget, and performs no remote work before returning.
func (s *FeishuPersonalService) ContinueBatch(ctx context.Context, id string, confirmed bool) error {
	// Compatibility endpoint: confirmation is no longer a product requirement.
	// RetryBatch reloads owner/KB/connection authority and preserves all IDs.
	return s.RetryBatch(ctx, id)
}
