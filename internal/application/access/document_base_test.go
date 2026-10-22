package access

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type documentMembers struct{ member *types.TenantMember }

func (m *documentMembers) GetMembership(context.Context, string, uint64) (*types.TenantMember, error) {
	return m.member, nil
}

type documentKeys struct{ scope *types.APIKeyTenantScope }

func (k *documentKeys) GetUserMCPTenantScope(context.Context, uint64, uint64) (*types.APIKeyTenantScope, error) {
	return k.scope, nil
}

type documentKBs struct{ kb *types.KnowledgeBase }

func (k documentKBs) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return k.kb, nil
}

func TestDocumentM3LiveBaseRechecksMembershipSharesAndKeyScopes(t *testing.T) {
	members := &documentMembers{&types.TenantMember{UserID: "employee", TenantID: 1, Status: types.TenantMemberStatusActive, Role: types.TenantRoleViewer}}
	pinnedShare := "share-live"
	keys := &documentKeys{&types.APIKeyTenantScope{APIKeyID: 7, TenantID: 1, KBScopeMode: types.APIKeyKBScopeSelected, KnowledgeBases: []types.APIKeyKnowledgeBaseScope{{APIKeyID: 7, TenantID: 1, KnowledgeBaseID: "target", SourceType: types.APIKeyKBSourceShared, KBShareID: &pinnedShare}}}}
	shares := &shareLookup{permission: types.OrgRoleViewer}
	v := &LiveDocumentBaseVerifier{Members: members, Keys: keys, KBs: documentKBs{&types.KnowledgeBase{ID: "target", TenantID: 99}}, Shares: shares}
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1, UserID: "employee", Role: types.TenantRoleAdmin})
	ctx = types.WithTenantAPIKeyScope(ctx, types.TenantAPIKeyScope{KeyID: 7, ScopeType: types.APIKeyScopeUserMCP, KnowledgeBaseIDs: types.StringArray{"target"}})
	require.NoError(t, v.CheckDocumentReadBase(ctx, 99, "target", types.DocumentRead, false))
	require.Equal(t, types.TenantRoleViewer, shares.role, "old administrator role cannot bypass current membership role")
	keys.scope.KnowledgeBases[0].SourceType = types.APIKeyKBSourceOwned
	require.Error(t, v.CheckDocumentReadBase(ctx, 99, "target", types.DocumentRead, false), "an owned KB moved to another tenant must not silently acquire a share path")
	keys.scope.KnowledgeBases[0].SourceType = types.APIKeyKBSourceShared
	shares.permission = ""
	require.Error(t, v.CheckDocumentReadBase(ctx, 99, "target", types.DocumentRead, false))
	shares.permission = types.OrgRoleViewer
	keys.scope.KnowledgeBases = nil
	require.Error(t, v.CheckDocumentReadBase(ctx, 99, "target", types.DocumentRead, false), "empty selected set is DENY_ALL even with stale request scope")
	require.Error(t, v.CheckDocumentReadBase(ctx, 99, "target", types.DocumentRead, true), "unmapped enterprise credential scope is HOLD")
	v.EnterpriseScope = func(context.Context, uint64, string) error { return nil }
	require.NoError(t, v.CheckDocumentReadBase(ctx, 99, "target", types.DocumentRead, true))
	members.member.Status = types.TenantMemberStatus("revoked")
	require.Error(t, v.CheckDocumentReadBase(ctx, 99, "target", types.DocumentRead, true))
	members.member.Status = types.TenantMemberStatusActive
	keys.scope.KBScopeMode = types.APIKeyKBScopeAll
	require.Error(t, v.CheckDocumentReadBase(ctx, 99, "target", types.DocumentFile, false), "file containment is independently required")
}
