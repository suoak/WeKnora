package access

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type sourceWriteLoaderFunc func(context.Context, SourceWriteTarget, SourceWritePrincipal) (SourceWriteFacts, error)

func (f sourceWriteLoaderFunc) LoadSourceWriteFacts(ctx context.Context, target SourceWriteTarget, kind SourceWritePrincipal) (SourceWriteFacts, error) {
	return f(ctx, target, kind)
}

func sourceWriteFixture() (context.Context, SourceWriteTarget, SourceWriteFacts) {
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleViewer})
	target := SourceWriteTarget{TenantID: 1, KnowledgeBaseID: "kb", SourceID: "source"}
	facts := SourceWriteFacts{
		TenantID: 1, TenantStatus: "active", KnowledgeBase: &types.KnowledgeBase{ID: "kb", TenantID: 1},
		Source: &SourceWriteSource{ID: "source", KnowledgeBaseID: "kb", TenantID: 1, Revision: "current-config"},
		Member: SourceWriteMember{UserID: "owner", TenantID: 1, Role: types.TenantRoleViewer, Status: "active", UserActive: true},
	}
	return ctx, target, facts
}

func TestSourceWriteCurrentOwnSpaceContract(t *testing.T) {
	for _, name := range []string{"viewer", "private-owner", "new-source", "foreign-space", "private-other", "deleted-source", "wrong-source-kb", "inactive-member", "deleted-user", "inactive-tenant"} {
		t.Run(name, func(t *testing.T) {
			ctx, target, facts := sourceWriteFixture()
			allowed := true
			switch name {
			case "private-owner":
				facts.KnowledgeBase.PrivateOwnerID = "owner"
			case "new-source":
				target.SourceID, facts.Source = "", nil
			case "foreign-space":
				ctx = types.WithCaller(ctx, types.Caller{TenantID: 2, UserID: "owner", Role: types.TenantRoleAdmin})
				grant, err := ResolveKB(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, facts.KnowledgeBase,
					types.OrgRoleEditor, &shareLookup{permission: types.OrgRoleEditor}, nil)
				require.NoError(t, err)
				ctx = grant.Context(ctx)
				allowed = false
			case "private-other":
				facts.KnowledgeBase.PrivateOwnerID, allowed = "another", false
			case "deleted-source":
				facts.Source.Deleted, allowed = true, false
			case "wrong-source-kb":
				facts.Source.KnowledgeBaseID, allowed = "other", false
			case "inactive-member":
				facts.Member.Status, allowed = "suspended", false
			case "deleted-user":
				facts.Member.UserDeleted, allowed = true, false
			case "inactive-tenant":
				facts.TenantStatus, allowed = "suspended", false
			}
			loader := sourceWriteLoaderFunc(func(_ context.Context, got SourceWriteTarget, kind SourceWritePrincipal) (SourceWriteFacts, error) {
				require.Equal(t, target, got)
				require.Equal(t, SourceWriteUser, kind)
				return facts, nil
			})
			err := AuthorizeSourceWrite(ctx, target, loader)
			require.Equal(t, allowed, err == nil)
			if allowed {
				require.ErrorIs(t, RequireKBWrite(ctx, facts.KnowledgeBase), ErrForbidden, "source permission creates no generic ingest grant")
			}
		})
	}
}

func TestSourceWriteKeyCapabilitiesAndLiveRevocation(t *testing.T) {
	for _, name := range []string{"manage-only", "manage-and-ingest", "full", "ingest-only", "retrieve-only", "wrong-kb", "live-kb-revoked", "live-capability-revoked", "live-ingest-revoked", "revoked", "expired", "wrong-key", "private-other", "personal-owner", "personal-revoked-member", "personal-wrong-owner", "changed-scope-type"} {
		t.Run(name, func(t *testing.T) {
			ctx, target, facts := sourceWriteFixture()
			ctx = types.WithCaller(ctx, types.Caller{TenantID: 1})
			scope := types.TenantAPIKeyScope{KeyID: 7, Capabilities: types.StringArray{"manage_datasources", "ingest"}, KnowledgeBaseIDs: types.StringArray{"kb"}}
			allowed := name == "manage-and-ingest" || name == "full" || name == "personal-owner"
			if name == "manage-only" {
				scope.Capabilities = types.StringArray{"manage_datasources"}
			}
			if name == "personal-owner" || name == "personal-revoked-member" || name == "personal-wrong-owner" {
				ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "owner", Role: types.TenantRoleViewer})
				scope.ScopeType = types.APIKeyScopeUserMCP
			}
			if name == "full" {
				scope.FullAccess, scope.Capabilities = true, nil
			}
			if name == "ingest-only" || name == "retrieve-only" {
				scope.Capabilities = types.StringArray{"ingest"}
				if name == "retrieve-only" {
					scope.Capabilities = types.StringArray{"retrieve"}
				}
			}
			if name == "wrong-kb" {
				scope.KnowledgeBaseIDs = types.StringArray{"other"}
			}
			facts.Key = &SourceWriteKey{TenantID: 1, Scope: scope}
			if scope.ScopeType == types.APIKeyScopeUserMCP {
				facts.Key.OwnerUserID = "owner"
			}
			switch name {
			case "live-kb-revoked":
				facts.Key.Scope.KnowledgeBaseIDs = types.StringArray{"other"}
			case "live-capability-revoked":
				facts.Key.Scope.Capabilities = nil
			case "live-ingest-revoked":
				facts.Key.Scope.Capabilities = types.StringArray{"manage_datasources"}
			case "revoked":
				facts.Key.Revoked = true
			case "expired":
				expired := time.Now().Add(-time.Minute)
				facts.Key.ExpiresAt = &expired
			case "wrong-key":
				facts.Key.Scope.KeyID = 8
			case "private-other":
				facts.KnowledgeBase.PrivateOwnerID = "owner"
			case "personal-revoked-member":
				facts.Member.Status = "suspended"
			case "personal-wrong-owner":
				facts.Key.OwnerUserID = "another"
			case "changed-scope-type":
				facts.Key.Scope.ScopeType = types.APIKeyScopePlatform
			}
			ctx = types.WithTenantAPIKeyScope(ctx, scope)
			loader := sourceWriteLoaderFunc(func(_ context.Context, _ SourceWriteTarget, kind SourceWritePrincipal) (SourceWriteFacts, error) {
				require.Equal(t, SourceWriteAPIKey, kind)
				return facts, nil
			})
			require.Equal(t, allowed, AuthorizeSourceWrite(ctx, target, loader) == nil)
			require.False(t, HasKBGrant(ctx, "kb", 1, types.OrgRoleEditor))
		})
	}
}

func TestSourceWriteRequiresCurrentFactsEveryTime(t *testing.T) {
	ctx, target, facts := sourceWriteFixture()
	grant, err := ResolveKB(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, facts.KnowledgeBase,
		types.OrgRoleEditor, nil, nil)
	require.NoError(t, err)
	ctx = grant.Context(ctx)
	calls := 0
	loader := sourceWriteLoaderFunc(func(context.Context, SourceWriteTarget, SourceWritePrincipal) (SourceWriteFacts, error) {
		calls++
		return facts, nil
	})
	require.NoError(t, AuthorizeSourceWrite(ctx, target, loader))
	facts.Member.Status = "suspended"
	require.ErrorIs(t, AuthorizeSourceWrite(ctx, target, loader), ErrForbidden)
	require.Equal(t, 2, calls, "an existing resource grant must not cache source authority")
	want := errors.New("current policy database unavailable")
	loader = func(context.Context, SourceWriteTarget, SourceWritePrincipal) (SourceWriteFacts, error) {
		return facts, want
	}
	require.ErrorIs(t, AuthorizeSourceWrite(ctx, target, loader), want)
	require.ErrorIs(t, AuthorizeSourceWrite(ctx, target, nil), ErrForbidden)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, AuthorizeSourceWrite(canceled, target, loader), context.Canceled)
}

func TestSourceWriteBackgroundDelegationIsSeparateAndBound(t *testing.T) {
	for _, name := range []string{"management", "oauth-owner-viewer", "private-owner-viewer", "no-delegation", "stale-delegation", "ordinary-viewer-management", "wrong-oauth-owner", "unknown-mode", "http-user", "api-key", "terminal-principal-without-caller", "no-source"} {
		t.Run(name, func(t *testing.T) {
			_, target, facts := sourceWriteFixture()
			ctx := types.WithExecutionTenant(context.Background(), 1)
			facts.Member.Role = types.TenantRoleOwner
			facts.Delegation = &SourceWriteDelegation{SourceID: "source", SourceRevision: "current-config", ActorUserID: "owner", Mode: "application", Status: "active"}
			allowed := name == "management" || name == "oauth-owner-viewer" || name == "private-owner-viewer"
			switch name {
			case "oauth-owner-viewer":
				facts.Member.Role = types.TenantRoleViewer
				facts.Delegation.Mode, facts.Delegation.PersonalOwnerID = "user_oauth", "owner"
			case "private-owner-viewer":
				facts.Member.Role, facts.KnowledgeBase.PrivateOwnerID = types.TenantRoleViewer, "owner"
			case "no-delegation":
				facts.Delegation = nil
			case "stale-delegation":
				facts.Delegation.SourceRevision = "previous-config"
			case "ordinary-viewer-management":
				facts.Member.Role = types.TenantRoleViewer
			case "wrong-oauth-owner":
				facts.Delegation.Mode, facts.Delegation.PersonalOwnerID = "user_oauth", "another"
			case "unknown-mode":
				facts.Delegation.Mode = "payload-says-trusted"
			case "http-user":
				ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: "owner"})
			case "api-key":
				ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 7, FullAccess: true})
			case "terminal-principal-without-caller":
				ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalMCPEndpoint, ID: "endpoint"})
			case "no-source":
				target.SourceID = ""
			}
			loader := sourceWriteLoaderFunc(func(_ context.Context, _ SourceWriteTarget, kind SourceWritePrincipal) (SourceWriteFacts, error) {
				require.Equal(t, SourceWriteBackground, kind)
				return facts, nil
			})
			require.Equal(t, allowed, AuthorizeBackgroundSourceWrite(ctx, target, loader) == nil)
			if allowed {
				require.Error(t, AuthorizeSourceWrite(ctx, target, loader), "background context cannot authorize an HTTP request")
				require.Zero(t, types.CallerFromContext(ctx).TenantID, "successful background authorization never impersonates management")
			}
		})
	}
}

func TestSourceWriteCurrentClaimAndUntrustedContext(t *testing.T) {
	ctx, target, facts := sourceWriteFixture()
	claim := types.SyncExecution{DataSourceID: "source", TenantID: 1, SyncLogID: "run", Generation: 9}
	facts.Execution = &claim
	ctx = types.WithSyncExecution(ctx, claim)
	loader := sourceWriteLoaderFunc(func(context.Context, SourceWriteTarget, SourceWritePrincipal) (SourceWriteFacts, error) {
		return facts, nil
	})
	require.NoError(t, AuthorizeSourceWrite(ctx, target, loader))
	current := claim
	current.Generation++
	facts.Execution = &current
	require.ErrorIs(t, AuthorizeSourceWrite(ctx, target, loader), ErrForbidden)
	facts.Execution = nil
	require.ErrorIs(t, AuthorizeSourceWrite(ctx, target, loader), ErrForbidden)
	withoutCaller := types.WithExecutionTenant(context.Background(), 1)
	require.ErrorIs(t, AuthorizeSourceWrite(withoutCaller, target, loader), ErrUnauthorized)
	// The identifier tuple and an execution tenant never substitute for DB facts.
	require.ErrorIs(t, AuthorizeBackgroundSourceWrite(withoutCaller, target, nil), ErrForbidden)
}
