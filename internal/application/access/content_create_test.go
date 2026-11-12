package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestContentCreateDoesNotPromoteWorkspaceReadAccess(t *testing.T) {
	for _, tc := range []struct {
		name, owner, private string
		role                 types.TenantRole
		tenant               uint64
		share                types.OrgMemberRole
		allowed              bool
	}{
		{"other same-space KB", "B", "", types.TenantRoleContributor, 1, "", false},
		{"owned KB", "A", "", types.TenantRoleContributor, 1, "", true},
		{"private owned KB", "A", "A", types.TenantRoleContributor, 1, "", true},
		{"foreign private KB even admin", "B", "B", types.TenantRoleAdmin, 1, "", false},
		{"admin", "B", "", types.TenantRoleAdmin, 1, "", true},
		{"public read only", "B", "", types.TenantRoleViewer, 2, "", false},
		{"shared viewer", "B", "", types.TenantRoleContributor, 2, types.OrgRoleViewer, false},
		{"shared editor", "B", "", types.TenantRoleContributor, 2, types.OrgRoleEditor, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "A", Role: tc.role})
			kb := &types.KnowledgeBase{ID: "kb", TenantID: tc.tenant, Type: "document", CreatorID: tc.owner, PrivateOwnerID: tc.private}
			require.Equal(t, tc.allowed, CheckKBContentCreate(ctx, kb, &shareLookup{permission: tc.share}) == nil)
		})
	}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "A", Role: types.TenantRoleContributor})
	kb := &types.KnowledgeBase{ID: "kb", TenantID: 1, Type: "document", CreatorID: "B"}
	grant, err := ResolveKB(ctx, KBRequest{Caller: types.CallerFromContext(ctx)}, kb, types.OrgRoleEditor, nil, nil)
	require.NoError(t, err)
	require.NoError(t, RequireKBWrite(grant.Context(ctx), kb))
	require.ErrorIs(t, RequireKBContentCreate(grant.Context(ctx), kb), ErrForbidden)
	kb.CreatorID = "A"
	for _, tc := range []struct {
		capabilities types.StringArray
		allowed      bool
	}{
		{types.StringArray{"retrieve"}, false},
		{types.StringArray{"retrieve", "add_content"}, true},
		{types.StringArray{"retrieve", "ingest"}, false},
	} {
		scoped := types.WithTenantAPIKeyScope(grant.Context(ctx), types.TenantAPIKeyScope{ScopeType: types.APIKeyScopeUserMCP, Capabilities: tc.capabilities})
		require.Equal(t, tc.allowed, RequireKBContentCreate(scoped, kb) == nil)
		if !scopedCapabilityIsIngest(tc.capabilities) {
			require.Error(t, RequireKBWrite(scoped, kb), "Personal add_content never enables generic mutation")
		}
	}
}

func scopedCapabilityIsIngest(caps types.StringArray) bool {
	for _, c := range caps {
		if c == "ingest" {
			return true
		}
	}
	return false
}
