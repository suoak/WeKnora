package access

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// SourceWriteTarget contains identifiers, never an assertion of authority.
// SourceID may be empty only for an HTTP request creating a source.
type SourceWriteTarget struct {
	TenantID        uint64
	KnowledgeBaseID string
	SourceID        string
}

type SourceWritePrincipal string

const (
	SourceWriteUser       SourceWritePrincipal = "user"
	SourceWriteAPIKey     SourceWritePrincipal = "source_management_key"
	SourceWriteBackground SourceWritePrincipal = "background_delegation"
)

// SourceWriteMember is a current database projection, not a token/payload claim.
type SourceWriteMember struct {
	UserID, Status       string
	TenantID             uint64
	Role                 types.TenantRole
	UserActive           bool
	UserDeleted, Deleted bool
}

type SourceWriteSource struct {
	ID, KnowledgeBaseID, Revision string
	TenantID                      uint64
	Deleted                       bool
}

// SourceWriteDelegation is loaded from the persisted source/connection policy.
// Revision must match the current source configuration. It is not a new approval.
type SourceWriteDelegation struct {
	SourceID, SourceRevision, ActorUserID string
	Mode, Status, PersonalOwnerID         string
}

// SourceWriteKey contains no secret. The loader must reload revocation, expiry,
// current capabilities and KB scope for the authenticated context's KeyID.
type SourceWriteKey struct {
	// TenantID is the current authorized workspace binding, not a platform
	// key's nullable raw owner tenant. User-MCP Scope includes its tenant grant.
	TenantID         uint64
	OwnerUserID      string
	Scope            types.TenantAPIKeyScope
	Revoked, Deleted bool
	ExpiresAt        *time.Time
}

type SourceWriteFacts struct {
	TenantID      uint64
	TenantStatus  string
	TenantDeleted bool
	KnowledgeBase *types.KnowledgeBase
	Source        *SourceWriteSource
	Member        SourceWriteMember
	Key           *SourceWriteKey
	Delegation    *SourceWriteDelegation
	// Execution is the freshly reloaded, still-owned SQL claim, if ctx has one.
	Execution *types.SyncExecution
}

// SourceWriteFactsLoader is a server-owned DB adapter, never a request DTO or
// deserialized task payload. Every call must reload current tenant/KB/member,
// source binding and delegation/key facts; execution/publication callers use
// their existing transaction and locks. KB metadata must include the current
// authoritative private-owner lookup, including a confirmed public/empty owner.
// Source eligibility, remote ACL, OAuth,
// claim/trigger fences and idempotency remain the caller's responsibility.
type SourceWriteFactsLoader interface {
	LoadSourceWriteFacts(context.Context, SourceWriteTarget, SourceWritePrincipal) (SourceWriteFacts, error)
}

// AuthorizeSourceWrite checks HTTP source-management authority. It preserves
// both the route policy and SourceWriteAccess's effective ingest constraint.
// It returns no generic KB Editor/ingest grant.
func AuthorizeSourceWrite(ctx context.Context, target SourceWriteTarget, loader SourceWriteFactsLoader) error {
	if ctx == nil {
		return ErrUnauthorized
	}
	caller := types.CallerFromContext(ctx)
	kind := SourceWriteUser
	if _, ok := types.TenantAPIKeyScopeFromContext(ctx); ok {
		kind = SourceWriteAPIKey
	} else if caller.UserID == "" || types.IsSyntheticUserID(caller.UserID) {
		return ErrUnauthorized
	}
	if caller.TenantID == 0 || caller.TenantID != target.TenantID {
		return ErrForbidden
	}
	return authorizeSourceWrite(ctx, target, kind, loader)
}

// AuthorizeBackgroundSourceWrite is only for a server-owned scheduling/worker
// path. An absent caller or execution tenant is insufficient: the mandatory
// loader must supply current persisted source delegation and active membership.
// Pre-claim scheduling is permitted; any present claim is checked exactly.
func AuthorizeBackgroundSourceWrite(ctx context.Context, target SourceWriteTarget, loader SourceWriteFactsLoader) error {
	if ctx == nil || target.SourceID == "" {
		return ErrForbidden
	}
	caller := types.CallerFromContext(ctx)
	if _, ok := types.TenantAPIKeyScopeFromContext(ctx); ok || caller.UserID != "" || caller.TenantID != 0 {
		return ErrForbidden // An HTTP principal cannot switch itself to background.
	}
	if _, ok := types.PrincipalFromContext(ctx); ok {
		return ErrForbidden
	}
	return authorizeSourceWrite(ctx, target, SourceWriteBackground, loader)
}

func authorizeSourceWrite(ctx context.Context, target SourceWriteTarget, kind SourceWritePrincipal, loader SourceWriteFactsLoader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if loader == nil || target.TenantID == 0 || target.KnowledgeBaseID == "" {
		return ErrForbidden
	}
	facts, err := loader.LoadSourceWriteFacts(ctx, target, kind)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	kb := facts.KnowledgeBase
	if facts.TenantID != target.TenantID || facts.TenantStatus != "active" || facts.TenantDeleted ||
		kb == nil || kb.ID != target.KnowledgeBaseID || kb.TenantID != target.TenantID || kb.DeletedAt.Valid {
		return ErrForbidden
	}
	if target.SourceID != "" && (facts.Source == nil || facts.Source.Deleted ||
		facts.Source.ID != target.SourceID || facts.Source.TenantID != target.TenantID ||
		facts.Source.KnowledgeBaseID != target.KnowledgeBaseID) {
		return ErrForbidden
	}
	if claim, ok := types.SyncExecutionFromContext(ctx); ok &&
		(claim.DataSourceID != target.SourceID || claim.TenantID != target.TenantID ||
			claim.SyncLogID == "" || claim.Generation < 1 || facts.Execution == nil || *facts.Execution != claim) {
		return ErrForbidden
	}
	caller := types.CallerFromContext(ctx)
	if kind == SourceWriteAPIKey {
		scope, ok := types.TenantAPIKeyScopeFromContext(ctx)
		key := facts.Key
		if !ok || scope.KeyID == 0 || key == nil || key.TenantID != target.TenantID ||
			key.Scope.KeyID != scope.KeyID || key.Revoked || key.Deleted ||
			key.Scope.Normalize().ScopeType != scope.Normalize().ScopeType ||
			(key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now())) ||
			!sourceManagementKey(scope, kb.ID) || !sourceManagementKey(key.Scope, kb.ID) ||
			(kb.PrivateOwnerID != "" && kb.PrivateOwnerID != caller.UserID) {
			return ErrForbidden
		}
		if scope.Normalize().ScopeType == types.APIKeyScopeUserMCP &&
			(key.OwnerUserID != caller.UserID || !sourceWriteMemberActive(facts.Member, caller.UserID, target.TenantID)) {
			return ErrForbidden
		}
		return nil
	}
	actor := caller.UserID
	if kind == SourceWriteBackground {
		d := facts.Delegation
		if d == nil || d.Status != "active" || d.SourceID != target.SourceID ||
			d.SourceRevision == "" || d.SourceRevision != facts.Source.Revision || d.ActorUserID == "" {
			return ErrForbidden
		}
		actor = d.ActorUserID
		if d.Mode == "user_oauth" || d.Mode == "personal_ref" {
			if d.PersonalOwnerID != actor {
				return ErrForbidden
			}
		} else if d.Mode != "application" || (kb.PrivateOwnerID != actor &&
			facts.Member.Role != types.TenantRoleOwner && facts.Member.Role != types.TenantRoleAdmin) {
			return ErrForbidden
		}
	}
	if !sourceWriteMemberActive(facts.Member, actor, target.TenantID) ||
		(kb.PrivateOwnerID != "" && kb.PrivateOwnerID != actor) {
		return ErrForbidden
	}
	// Own-space ordinary KB Viewer access is the existing datasource contract.
	return nil
}

func sourceWriteMemberActive(m SourceWriteMember, actor string, tenant uint64) bool {
	return actor != "" && !types.IsSyntheticUserID(actor) && m.UserID == actor && m.TenantID == tenant &&
		m.Status == "active" && m.Role.IsValid() && m.UserActive && !m.UserDeleted && !m.Deleted
}

func sourceManagementKey(scope types.TenantAPIKeyScope, kb string) bool {
	// APIKeyGate admits FullAccess OR manage_datasources. Real auth sessions
	// normalize the caller role, so SourceWriteAccess also calls RequireKBWrite,
	// which requires ingest for scoped keys. Preserve the effective intersection;
	// interpreting the route gate alone as sufficient would expand authority.
	return scope.AllowsKnowledgeBase(kb) &&
		(scope.FullAccess || (scope.HasCapability(types.APIKeyCapabilityManageDataSources) &&
			scope.HasCapability(types.APIKeyCapabilityIngest)))
}
