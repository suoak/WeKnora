package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPortalM6DynamicScopeAndWithdrawal(t *testing.T) {
	f := fixture.New(t)
	r := f.Portal(t)
	ctx := fixture.Key("A", 1, 1, true)
	ts, err := r.Resolve(ctx, nil)
	require.NoError(t, err)
	require.Len(t, ts, 3)
	ids := map[string]bool{}
	for _, target := range ts {
		ids[target.Descriptor().ID] = target.Public()
	}
	require.True(t, ids["public-kb"])
	require.True(t, ids["public-two"])
	require.False(t, ids["private-home"])
	_, err = r.Resolve(ctx, []string{"private-kb"})
	require.Error(t, err)
	_, err = r.Resolve(ctx, []string{"unknown-kb"})
	require.Error(t, err)
	old := fixture.Key("A", 1, 3, false)
	ts, err = r.Resolve(old, nil)
	require.NoError(t, err)
	require.Len(t, ts, 2, "old Key retains own private KBs, without cross-space public expansion")
	for _, target := range ts {
		require.False(t, target.Public())
		require.Equal(t, uint64(1), target.Descriptor().TenantID)
	}
	target, err := r.Resolve(ctx, []string{"public-kb"})
	require.NoError(t, err)
	execution, err := target[0].SearchContext(ctx)
	require.NoError(t, err)
	_, err = access.GuardDocumentRetrieval(execution, types.SQLiteRetrieverEngineType, types.RetrieveParams{TopK: 5})
	require.Error(t, err, "unsupported legacy driver cannot consume Portal grant")
	for _, swapped := range []context.Context{types.WithPrincipal(execution, types.Principal{Type: types.PrincipalWebUser, ID: "C"}), types.WithCaller(execution, types.Caller{TenantID: 99, UserID: "A", Role: types.TenantRoleViewer}), types.WithTenantAPIKeyScope(execution, types.TenantAPIKeyScope{KeyID: 3, ScopeType: types.APIKeyScopeUserMCP, Capabilities: types.StringArray{"retrieve"}})} {
		_, err = access.GuardPortalRetrieval(swapped, types.PostgresRetrieverEngineType, types.RetrieveParams{TopK: 5})
		require.Error(t, err, "an opaque scope cannot be reused by another principal or credential context")
	}
	_, err = access.GuardPortalRetrieval(execution, types.PostgresRetrieverEngineType, types.RetrieveParams{TopK: 5, KnowledgeBaseIDs: []string{"unknown-kb"}})
	require.Error(t, err)
	_, err = access.GuardPortalRetrieval(execution, types.QdrantRetrieverEngineType, types.RetrieveParams{TopK: 5})
	require.Error(t, err)
	_, err = access.GuardPortalRetrieval(execution, types.PostgresRetrieverEngineType, types.RetrieveParams{TopK: 5, KnowledgeIDs: []string{"sensitive"}})
	require.Error(t, err)
	require.NoError(t, f.DB.Exec("UPDATE tenant_portal_configs SET status='draft' WHERE tenant_id=99").Error)
	require.Error(t, target[0].Revalidate(ctx))
	ts, err = r.Resolve(ctx, nil)
	require.NoError(t, err)
	require.Len(t, ts, 2)
	require.NoError(t, f.DB.Exec("UPDATE tenant_portal_configs SET status='archived' WHERE tenant_id=1").Error)
	ts, err = r.Resolve(ctx, nil)
	require.NoError(t, err)
	require.Len(t, ts, 2, "independent tenant-1 private grants survive public withdrawal")
	for _, target := range ts {
		require.False(t, target.Public())
	}
	require.NoError(t, f.DB.Exec("UPDATE tenant_members SET status='inactive' WHERE user_id='A' AND tenant_id=1").Error)
	ts, err = r.Resolve(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, ts)
	require.NoError(t, f.DB.Exec("UPDATE tenant_api_keys SET revoked_at=CURRENT_TIMESTAMP WHERE id=1").Error)
	_, err = r.Resolve(ctx, nil)
	require.Error(t, err)
	machine := types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPITenant, ID: "99"})
	_, err = r.Resolve(machine, nil)
	require.Error(t, err)
}

func TestPortalM7PublicRequiresLiveBaseEvenWhenClassified(t *testing.T) {
	f := fixture.New(t)
	r := f.Portal(t)
	r.Documents = access.NewDocumentReader(true, f.Store, nil, nil)
	_, err := r.Resolve(fixture.Key("A", 1, 1, true), []string{"public-kb"})
	require.Error(t, err, "whole KB public SQL scope cannot replace existing KB/Key base verification")
}

func TestPortalM6ClosedAndEmptyDoesNotCallEngine(t *testing.T) {
	f := fixture.New(t)
	r := f.Portal(t)
	r.Enabled = false
	s := &PortalMCPKnowledge{Resolver: r, Engine: func(context.Context, access.PortalKBDescriptor) (access.DocumentRetriever, error) {
		t.Fatal("disabled scope called driver")
		return nil, nil
	}}
	_, err := s.Search(fixture.Key("A", 1, 1, true), nil, "query", "hybrid", 10)
	require.Error(t, err)
}

func TestPortalM6IndependentPrivateWorkspaces(t *testing.T) {
	f := fixture.New(t)
	r := f.Portal(t)
	ctx := fixture.Key("A", 1, 3, false)
	ts, err := r.Resolve(ctx, nil)
	require.NoError(t, err)
	require.Len(t, ts, 2)
	// Explicit synthetic membership and explicit Key scope, never a public-side
	// effect. No production member or Key is changed.
	require.NoError(t, f.DB.Exec("INSERT INTO tenant_members(user_id,tenant_id,role,status) VALUES('A',99,'viewer','active')").Error)
	require.NoError(t, f.DB.Exec("INSERT INTO api_key_tenant_scopes(api_key_id,tenant_id,kb_scope_mode) VALUES(3,99,'all')").Error)
	ts, err = r.Resolve(ctx, nil)
	require.NoError(t, err)
	require.Len(t, ts, 3)
	var retained *access.PortalKBTarget
	for _, target := range ts {
		require.False(t, target.Public())
		if target.Descriptor().ID == "public-kb" {
			retained = target
		}
	}
	require.NotNil(t, retained)
	require.NoError(t, f.DB.Exec("UPDATE api_key_tenant_scopes SET kb_scope_mode='selected' WHERE api_key_id=3 AND tenant_id=99").Error)
	require.Error(t, retained.Revalidate(ctx), "empty selected scope is DENY_ALL")
	ts, err = r.Resolve(ctx, nil)
	require.NoError(t, err)
	require.Len(t, ts, 2)
	require.NoError(t, f.DB.Exec("UPDATE api_key_tenant_scopes SET kb_scope_mode='all' WHERE api_key_id=3 AND tenant_id=99").Error)
	require.NoError(t, f.DB.Exec("UPDATE tenant_members SET status='left' WHERE user_id='A' AND tenant_id=99").Error)
	ts, err = r.Resolve(ctx, nil)
	require.NoError(t, err)
	require.Len(t, ts, 2)
}
