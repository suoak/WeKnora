package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// A Feishu import is a KB write, just like a local upload. Reconstruct live
// user/membership authority; inherited task grants cannot confer permissions.
// This authorizes the destination only, never source reading or export.
func (s *FeishuPersonalService) requireImportWriter(tx *gorm.DB, userID, kbID string, tenantID uint64) error {
	if userID == "" || tenantID == 0 || types.IsSyntheticUserID(userID) {
		return access.ErrUnauthorized
	}
	lock := clause.Locking{Strength: "SHARE"}
	var user types.User
	var tenant types.Tenant
	var member types.TenantMember
	var kb types.KnowledgeBase
	if err := tx.Clauses(lock).Where("id = ? AND is_active = ?", userID, true).First(&user).Error; err != nil {
		return access.ErrForbidden
	}
	if err := tx.Clauses(lock).Where("id = ? AND status = ?", tenantID, "active").First(&tenant).Error; err != nil {
		return access.ErrForbidden
	}
	if err := tx.Clauses(lock).Where("user_id = ? AND tenant_id = ? AND status = ?", userID, tenantID, types.TenantMemberStatusActive).First(&member).Error; err != nil {
		return access.ErrForbidden
	}
	if !member.Role.HasPermission(types.TenantRoleContributor) {
		return access.ErrForbidden
	}
	if err := tx.Clauses(lock).Where("id = ?", kbID).First(&kb).Error; err != nil {
		return access.ErrForbidden
	}
	if types.PrivateKBGuardsEnabled(tx) {
		owner, err := types.PrivateKBOwner(tx, kbID)
		if err != nil {
			return err
		}
		kb.PrivateOwnerID = owner
	}
	fresh := context.WithValue(tx.Statement.Context, types.KBGrantsContextKey, nil)
	fresh = context.WithValue(fresh, types.UserIDContextKey, userID)
	caller := types.Caller{TenantID: tenantID, UserID: userID, Role: member.Role}
	fresh = types.WithCaller(fresh, caller)
	fresh = types.WithPrincipal(fresh, types.Principal{Type: types.PrincipalWebUser, ID: userID})
	var shares access.KBShareLookup
	if s.recovery != nil {
		shares = s.recovery.shares
	}
	grant, err := access.ResolveKB(fresh, access.KBRequest{Caller: caller}, &kb, types.OrgRoleEditor, shares, nil)
	if err != nil {
		return err
	}
	return access.RequireKBWrite(grant.Context(fresh), &kb)
}
