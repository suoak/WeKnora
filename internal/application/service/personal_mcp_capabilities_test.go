package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func TestPersonalMCPCreateAndReplaceCannotRemoveBuiltinCapabilities(t *testing.T) {
	for _, caps := range [][]string{nil, {}, {"retrieve"}, {"add_content"}, {"chat"}, {"enterprise_retrieve"}} {
		repo := newFakeTenantAPIKeyRepo()
		svc := NewTenantAPIKeyService(repo)
		created, err := svc.CreateAPIKey(context.Background(), interfaces.TenantAPIKeyCreateRequest{
			ScopeType: types.APIKeyScopeUserMCP, OwnerUserID: "owner", Name: "personal", Capabilities: caps,
			FullAccess: true, KnowledgeBaseIDs: []string{"must-not-expand"},
			TenantScopes: []types.APIKeyTenantScope{{TenantID: 7, KBScopeMode: types.APIKeyKBScopeSelected,
				KnowledgeBases: []types.APIKeyKnowledgeBaseScope{{KnowledgeBaseID: "owned"}}}},
		})
		require.NoError(t, err)
		require.Contains(t, created.APIKey.Capabilities, "retrieve")
		require.Contains(t, created.APIKey.Capabilities, "add_content")
		require.False(t, created.APIKey.FullAccess)
		require.Empty(t, created.APIKey.KnowledgeBaseIDs)
		updated, err := svc.(interfaces.UserMCPAPIKeyService).ReplaceUserMCPAPIKey(
			context.Background(), "owner", &types.TenantAPIKey{
				ID: created.APIKey.ID, Name: "renamed", Capabilities: types.StringArray{},
				TenantScopes: created.APIKey.TenantScopes,
			})
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"retrieve", "add_content"}, []string(updated.Capabilities))
		require.Equal(t, types.APIKeyKBScopeSelected, updated.TenantScopes[0].KBScopeMode)
		require.Equal(t, "owned", updated.TenantScopes[0].KnowledgeBases[0].KnowledgeBaseID)
	}
	// Ordinary machine Keys retain their existing explicit capability semantics.
	svc := NewTenantAPIKeyService(newFakeTenantAPIKeyRepo())
	machine, err := svc.CreateAPIKey(context.Background(), interfaces.TenantAPIKeyCreateRequest{
		TenantID: 7, Name: "machine", Capabilities: []string{"retrieve"},
	})
	require.NoError(t, err)
	require.Equal(t, types.StringArray{"retrieve"}, machine.APIKey.Capabilities)
}
