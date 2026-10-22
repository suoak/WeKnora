package access

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

type DocumentMembershipLookup interface {
	GetMembership(context.Context, string, uint64) (*types.TenantMember, error)
}
type DocumentKeyScopeLookup interface {
	GetUserMCPTenantScope(context.Context, uint64, uint64) (*types.APIKeyTenantScope, error)
}
type DocumentKBLookup interface {
	GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error)
}

// LiveDocumentBaseVerifier reuses existing membership, Key scope, shares and
// ResolveKB. It reloads facts at every authorization boundary instead of
// accepting a cached KBAccess grant. EnterpriseScope and Files remain nil until
// their business mapping/legacy-link containment has been explicitly approved.
type LiveDocumentBaseVerifier struct {
	Members         DocumentMembershipLookup
	Keys            DocumentKeyScopeLookup
	KBs             DocumentKBLookup
	Shares          KBShareLookup
	Agents          AgentShareLookup
	EnterpriseScope func(context.Context, uint64, string) error
	Files           func(context.Context, uint64, string) error
}

// PrivateWorkspaceContext is a server-side selection of a persisted user Key
// scope and active membership; no client claim or public grant creates it.
func (v *LiveDocumentBaseVerifier) PrivateWorkspaceContext(ctx context.Context, tenant uint64) (context.Context, error) {
	caller := types.CallerFromContext(ctx)
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	key, keyed := types.TenantAPIKeyScopeFromContext(ctx)
	if v == nil || v.Members == nil || v.Keys == nil || !ok || p.Type != types.PrincipalWebUser || p.ID != caller.UserID || !keyed || key.ScopeType != types.APIKeyScopeUserMCP {
		return nil, ErrForbidden
	}
	member, err := v.Members.GetMembership(ctx, p.ID, tenant)
	if err != nil || member == nil || member.Status != types.TenantMemberStatusActive || member.DeletedAt.Valid || !member.Role.IsValid() {
		return nil, ErrForbidden
	}
	live, err := v.Keys.GetUserMCPTenantScope(ctx, key.KeyID, tenant)
	if err != nil || live == nil || live.APIKeyID != key.KeyID || live.TenantID != tenant {
		return nil, ErrForbidden
	}
	if live.KBScopeMode != types.APIKeyKBScopeAll && live.KBScopeMode != types.APIKeyKBScopeSelected {
		return nil, ErrForbidden
	}
	key.KnowledgeBaseRestricted = live.KBScopeMode == types.APIKeyKBScopeSelected
	key.KnowledgeBaseIDs = types.StringArray{}
	for _, kb := range live.KnowledgeBases {
		if kb.APIKeyID != key.KeyID || kb.TenantID != tenant {
			return nil, ErrForbidden
		}
		key.KnowledgeBaseIDs = append(key.KnowledgeBaseIDs, kb.KnowledgeBaseID)
	}
	caller.TenantID = tenant
	caller.Role = member.Role
	return types.WithTenantAPIKeyScope(types.WithExecutionTenant(types.WithCaller(ctx, caller), tenant), key), nil
}

func (v *LiveDocumentBaseVerifier) CheckDocumentReadBase(ctx context.Context, tenant uint64, kbID string, operation types.DocumentOperation, enterprise bool) error {
	if v == nil || v.Members == nil || v.KBs == nil || !operation.Valid() {
		return ErrForbidden
	}
	caller := types.CallerFromContext(ctx)
	if caller.UserID == "" || caller.TenantID == 0 {
		return ErrUnauthorized
	}
	member, err := v.Members.GetMembership(ctx, caller.UserID, caller.TenantID)
	if err != nil {
		return err
	}
	if member == nil || member.UserID != caller.UserID || member.TenantID != caller.TenantID || member.Status != types.TenantMemberStatusActive || member.DeletedAt.Valid || !member.Role.IsValid() {
		return ErrForbidden
	}
	caller.Role = member.Role
	// A new context contains current membership and scope, not old KB grants.
	fresh := types.WithCaller(ctx, caller)
	if scope, keyed := types.TenantAPIKeyScopeFromContext(ctx); keyed {
		if v.Keys == nil || scope.ScopeType != types.APIKeyScopeUserMCP || scope.FullAccess {
			return ErrForbidden
		}
		live, err := v.Keys.GetUserMCPTenantScope(ctx, scope.KeyID, caller.TenantID)
		if err != nil {
			return err
		}
		if live == nil || live.APIKeyID != scope.KeyID || live.TenantID != caller.TenantID || (live.KBScopeMode != types.APIKeyKBScopeAll && live.KBScopeMode != types.APIKeyKBScopeSelected) {
			return ErrForbidden
		}
		ids := types.StringArray{}
		for _, entry := range live.KnowledgeBases {
			if entry.APIKeyID != scope.KeyID || entry.TenantID != caller.TenantID {
				return ErrForbidden
			}
			if entry.SourceType != types.APIKeyKBSourceOwned && entry.SourceType != types.APIKeyKBSourceShared {
				return ErrForbidden
			}
			if entry.SourceType == types.APIKeyKBSourceShared && (entry.KBShareID == nil || *entry.KBShareID == "") {
				return ErrForbidden
			}
			if entry.KnowledgeBaseID == kbID && entry.SourceType == types.APIKeyKBSourceOwned && tenant != caller.TenantID {
				return ErrForbidden
			}
			ids = append(ids, entry.KnowledgeBaseID)
		}
		scope.KnowledgeBaseRestricted = live.KBScopeMode == types.APIKeyKBScopeSelected
		scope.KnowledgeBaseIDs = ids
		fresh = types.WithTenantAPIKeyScope(fresh, scope)
		if !enterprise && !scope.AllowsKnowledgeBase(kbID) {
			return ErrForbidden
		}
	}
	kb, err := v.KBs.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return err
	}
	if kb == nil || kb.ID != kbID || kb.TenantID != tenant || kb.DeletedAt.Valid || kb.IsTemporary {
		return ErrForbidden
	}
	if enterprise {
		if operation == types.DocumentFile || v.EnterpriseScope == nil {
			return ErrForbidden
		}
		return v.EnterpriseScope(fresh, tenant, kbID)
	}
	// ResolveKB checks current shares independently and does not consult a
	// context KBGrant fast path. The new read grant never becomes membership.
	if _, err := ResolveKB(fresh, KBRequest{Caller: caller}, kb, types.OrgRoleViewer, v.Shares, v.Agents); err != nil {
		return err
	}
	if operation == types.DocumentFile {
		if v.Files == nil {
			return ErrForbidden
		}
		return v.Files(fresh, tenant, kbID)
	}
	return nil
}
