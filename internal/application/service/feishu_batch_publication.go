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

type batchPublicationApprovalKey struct{}

func batchPublicationContext(ctx context.Context, item *types.FeishuBatchItem) context.Context {
	if item.SharingApprovalID == nil {
		return ctx
	}
	return context.WithValue(ctx, batchPublicationApprovalKey{}, *item.SharingApprovalID)
}

// Write access alone never constitutes source publication authority.
func (s *FeishuPersonalService) requireSharingAuthority(ctx context.Context, kbID string) error {
	user, ok := types.UserIDFromContext(ctx)
	if !ok || types.IsSyntheticUserID(user) {
		return access.ErrUnauthorized
	}
	kb, err := s.kbs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return err
	}
	if err = access.RequireKBWrite(ctx, kb); err != nil {
		return err
	}
	caller := types.CallerFromContext(ctx)
	if caller.TenantID != kb.TenantID ||
		kb.CreatorID != user && !caller.Role.HasPermission(types.TenantRoleAdmin) {
		return access.ErrForbidden
	}
	return nil
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
	if b.PublicationConsentAt == nil || item.SharingApprovalID != nil {
		return nil
	}
	if b.AuthMode != types.FeishuUserOAuth {
		return access.ErrForbidden
	}
	expires := b.PublicationConsentAt.Add(24 * time.Hour)
	if !expires.After(time.Now()) {
		return errBatchPublicationExpired
	}
	approval, err := s.sharingApprovalCandidate(ctx, b.KBID, b.ConnectionID, item.URL,
		"一键导入：明确发布本批所选文档的当前版本给目标知识库读者", true)
	if err != nil {
		return err
	}
	if approval.ConfigVersion != b.ConfigVersion || approval.OwnerID != b.OwnerID {
		return ErrFeishuReconnect
	}
	approval.ExpiresAt = expires
	bound := ""
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current types.FeishuBatchItem
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND batch_id = ? AND status = ? AND lease_owner = ? AND lease_until > ?",
				item.ID, b.ID, "processing", item.LeaseOwner, time.Now()).First(&current).Error; e != nil {
			return e
		}
		if current.SharingApprovalID != nil {
			bound = *current.SharingApprovalID
			return nil
		}
		if !expires.After(time.Now()) {
			return errBatchPublicationExpired
		}
		if e := lockBatchPublicationAuthority(tx, b); e != nil {
			return e
		}
		if e := tx.Create(approval).Error; e != nil {
			return e
		}
		if e := tx.Model(&current).Update("sharing_approval_id", approval.ID).Error; e != nil {
			return e
		}
		bound = approval.ID
		return nil
	})
	if err == nil && bound != "" {
		item.SharingApprovalID = &bound
	}
	return err
}

// ContinueBatch combines explicit publication intent and requeue for an old
// blocked batch. It never changes a bound revision, existing attempt, source,
// cursor or retry budget, and performs no remote work before returning.
func (s *FeishuPersonalService) ContinueBatch(ctx context.Context, id string, confirmed bool) error {
	if !confirmed {
		return ErrFeishuSharingApproval
	}
	view, err := s.Batch(ctx, id)
	if err != nil {
		return err
	}
	b := &view.Batch
	if b.AuthMode != types.FeishuUserOAuth {
		return access.ErrForbidden
	}
	fresh, err := s.batchContext(ctx, b)
	if err != nil {
		return err
	}
	if err = s.requireSharingAuthority(fresh, b.KBID); err != nil {
		return err
	}
	_, _, ref, err := s.ResolveCredentialReference(fresh, b.AuthMode, b.ConnectionID)
	if err != nil {
		return err
	}
	if ref.ConfigVersion != b.ConfigVersion {
		return ErrFeishuReconnect
	}
	return s.db.WithContext(fresh).Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", b.ID).First(b).Error; e != nil {
			return e
		}
		if e := lockBatchPublicationAuthority(tx, b); e != nil {
			return e
		}
		now := time.Now()
		if b.PublicationConsentAt != nil && !b.PublicationConsentAt.Add(24*time.Hour).After(now) {
			return errBatchPublicationExpired
		}
		if b.PublicationConsentAt == nil {
			if e := tx.Model(b).Update("publication_consent_at", now).Error; e != nil {
				return e
			}
		}
		return tx.Model(&types.FeishuBatchItem{}).
			Where("batch_id = ? AND status = ? AND error_category = ? AND import_id = ?"+
				" AND sharing_approval_id IS NULL AND retry_count < 3",
				b.ID, "blocked", "sharing_approval_required", "").
			Updates(map[string]any{"status": "pending", "error_category": "", "updated_at": now}).Error
	})
}
