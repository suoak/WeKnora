package repository

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPersonalMCPPersistenceAndLegacyRotationKeepBuiltinsAndScopes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.TenantAPIKey{}, &types.APIKeyTenantScope{},
		&types.APIKeyKnowledgeBaseScope{}))
	repo := &tenantAPIKeyRepository{db: db}
	owner := "owner"
	key := &types.TenantAPIKey{
		ScopeType: types.APIKeyScopeUserMCP, OwnerUserID: &owner, Name: "personal", KeyHash: "initial",
		Capabilities: types.StringArray{"enterprise_retrieve"},
		TenantScopes: []types.APIKeyTenantScope{{
			TenantID: 7, KBScopeMode: types.APIKeyKBScopeSelected,
			KnowledgeBases: []types.APIKeyKnowledgeBaseScope{{KnowledgeBaseID: "fixed", SourceType: "owned"}},
		}},
	}
	require.NoError(t, repo.CreateAPIKey(context.Background(), key))
	require.ElementsMatch(t, []string{"enterprise_retrieve", "retrieve", "add_content"}, []string(key.Capabilities))
	// Simulate a real historical persisted key, bypassing today's creation policy.
	require.NoError(t, db.Exec(`UPDATE tenant_api_keys SET capabilities='["enterprise_retrieve","retrieve"]'`).Error)
	_, err = repo.RotateUserMCPAPIKey(context.Background(), "other", key.ID, "initial", "stolen", "hint", nil)
	require.ErrorIs(t, err, ErrTenantAPIKeyNotFound)
	legacy, err := repo.GetUserMCPAPIKey(context.Background(), owner, key.ID)
	require.NoError(t, err)
	require.NotContains(t, legacy.Capabilities, "add_content", "unauthorized rotation must not change capabilities")
	rotated, err := repo.RotateUserMCPAPIKey(context.Background(), owner, key.ID, "initial", "rotated", "hint", nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"enterprise_retrieve", "retrieve", "add_content"}, []string(rotated.Capabilities))
	require.Equal(t, legacy.TenantScopes, rotated.TenantScopes, "rotation cannot replace fixed knowledge scopes")
	_, err = repo.RotateUserMCPAPIKey(context.Background(), owner, key.ID, "initial", "stale", "hint", nil)
	require.ErrorIs(t, err, ErrTenantAPIKeyNotFound)
	updated, err := repo.ReplaceUserMCPAPIKey(context.Background(), owner, &types.TenantAPIKey{
		ID: key.ID, Name: "updated", TenantScopes: rotated.TenantScopes,
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"retrieve", "add_content"}, []string(updated.Capabilities))
	require.Equal(t, legacy.TenantScopes[0].KnowledgeBases[0].KnowledgeBaseID,
		updated.TenantScopes[0].KnowledgeBases[0].KnowledgeBaseID)
}
