package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetPortalKBPublicBlock persists an exception in the existing policy fact.
// Block acts on the latest revision; unblock requires CAS and cannot resurrect
// a newer restriction. No approved record or personal grant is manufactured.
func (r *EnterpriseAccessRepository) SetPortalKBPublicBlock(ctx context.Context, tenant uint64, kb, actor, reason string, expected int64, blocked bool) error {
	if tenant == 0 || kb == "" || actor == "" || strings.TrimSpace(reason) == "" || len(reason) > 512 {
		return types.ErrEnterprisePolicyConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target struct{ TenantID uint64 }
		if err := tx.Table("knowledge_bases").Select("tenant_id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=? AND deleted_at IS NULL", kb, tenant).Take(&target).Error; err != nil {
			return err
		}
		var active int64
		if err := tx.Table("users").Where("id=? AND is_active=true AND deleted_at IS NULL", actor).Count(&active).Error; err != nil || active != 1 {
			return types.ErrEnterprisePolicyConflict
		}
		t := types.EnterprisePolicyTarget{Kind: "kb", ID: kb}
		old, err := loadEnterprisePolicy(tx, t, true)
		if err != nil {
			return err
		}
		if !blocked && old.Revision != expected {
			return types.ErrEnterprisePolicyConflict
		}
		version, owner, err := resourceVersion(tx, t)
		if err != nil {
			return err
		}
		if owner != tenant {
			return types.ErrEnterprisePolicyConflict
		}
		now := time.Now().UTC()
		next := types.EnterprisePolicy{State: types.EnterprisePolicyPending, Policy: types.EnterprisePolicyInherit, Revision: old.Revision + 1, UpdatedBy: actor, UpdatedAt: now, Reason: reason, ResourceVersion: version, ContentClass: "unknown", SourceACLState: "unknown"}
		if blocked {
			next.State = types.EnterprisePolicyRevoked
			next.Policy = types.EnterprisePolicyMCPBlocked
		}
		values := enterprisePolicyValues(next)
		if old.Revision == 0 {
			values["knowledge_base_id"] = kb
			q := tx.Table("kb_publication_policies").Clauses(clause.OnConflict{DoNothing: true}).Create(values)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return types.ErrEnterprisePolicyConflict
			}
		} else {
			q := tx.Table("kb_publication_policies").Where("knowledge_base_id=? AND revision=?", kb, old.Revision).Updates(values)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected != 1 {
				return types.ErrEnterprisePolicyConflict
			}
		}
		details, err := json.Marshal(map[string]any{"rule": "portal_published_default", "public_blocked": blocked, "old_revision": old.Revision, "revision": next.Revision, "resource_version": version, "reason": reason})
		if err != nil {
			return err
		}
		action := "enterprise.portal_public_allow"
		if blocked {
			action = "enterprise.portal_public_block"
		}
		return NewAuditLogRepository(tx).Create(ctx, &types.AuditLog{TenantID: tenant, ActorUserID: actor, Action: types.AuditAction(action), ScopeType: "tenant", ScopeID: strconv.FormatUint(tenant, 10), TargetType: "enterprise_kb", TargetID: kb, Outcome: types.AuditOutcomeSuccess, Details: types.JSON(details), CreatedAt: now})
	})
}
