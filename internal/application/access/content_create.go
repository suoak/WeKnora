package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// CheckKBContentCreate complements ResolveKB: a same-workspace access grant
// describes reach, not ownership. Callers must supply a freshly loaded KB and
// a live membership role. Cross-workspace editors use the existing share cap.
func CheckKBContentCreate(ctx context.Context, kb *types.KnowledgeBase, shares KBShareLookup) error {
	caller := types.CallerFromContext(ctx).Normalize()
	if kb == nil || kb.ID == "" || kb.TenantID == 0 || kb.DeletedAt.Valid || kb.IsTemporary ||
		kb.Type != types.KnowledgeBaseTypeDocument {
		return ErrForbidden
	}
	if caller.UserID == "" || caller.TenantID == 0 {
		return ErrUnauthorized
	}
	if kb.PrivateOwnerID != "" && kb.PrivateOwnerID != caller.UserID {
		return ErrForbidden
	}
	if kb.TenantID == caller.TenantID {
		if caller.Role.HasPermission(types.TenantRoleAdmin) || (kb.CreatorID != "" && kb.CreatorID == caller.UserID) ||
			kb.PrivateOwnerID == caller.UserID {
			return nil
		}
		return ErrForbidden
	}
	if kb.PrivateOwnerID != "" || shares == nil {
		return ErrForbidden
	}
	permission, shared, err := shares.CheckTenantKBPermission(ctx, kb.ID, caller.TenantID, caller.Role)
	if err != nil {
		return err
	}
	if !shared || !permission.HasPermission(types.OrgRoleEditor) {
		return ErrForbidden
	}
	return nil
}

// RequireKBContentCreate consumes an ordinary write grant plus the human
// ownership rule. Task grants continue an already admitted operation.
func RequireKBContentCreate(ctx context.Context, kb *types.KnowledgeBase) error {
	if kb == nil {
		return ErrNotFound
	}
	scope, keyed := types.TenantAPIKeyScopeFromContext(ctx)
	// Internal import services historically run without an authenticated human
	// or machine credential. HTTP adapters must establish a caller before entry.
	if !keyed && types.CallerFromContext(ctx).UserID == "" {
		return nil
	}
	if keyed && scope.ScopeType == types.APIKeyScopeUserMCP {
		if scope.FullAccess || !scope.HasCapability(types.APIKeyCapabilityAddContent) ||
			!HasKBGrant(ctx, kb.ID, kb.TenantID, types.OrgRoleEditor) {
			return ErrForbidden
		}
	} else if err := RequireKBWrite(ctx, kb); err != nil {
		return err
	}
	// Machine tenant keys retain their existing independent authority.
	if keyed && scope.ScopeType != types.APIKeyScopeUserMCP {
		return nil
	}
	grants, _ := ctx.Value(types.KBGrantsContextKey).([]kbGrant)
	for _, g := range grants {
		if g.task && g.kbID == kb.ID && g.tenantID == kb.TenantID && g.caller == types.CallerFromContext(ctx) {
			return nil
		}
	}
	if types.CallerFromContext(ctx).TenantID == kb.TenantID {
		return CheckKBContentCreate(ctx, kb, nil)
	}
	// The existing explicit Editor grant has already checked the share.
	return nil
}
