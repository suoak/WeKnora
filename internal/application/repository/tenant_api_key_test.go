package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTenantAPIKeyRepositoryPersistsUTCExpiry(t *testing.T) {
	t.Setenv("TZ", "Asia/Shanghai")

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.TenantAPIKey{}))

	repo := NewTenantAPIKeyRepository(db)
	ctx := context.Background()

	expiresAt := time.Unix(time.Now().UTC().Add(5*time.Second).Unix(), 0).UTC()
	tenantID := uint64(42)
	key := &types.TenantAPIKey{
		TenantID:   &tenantID,
		ScopeType:  types.APIKeyScopeTenant,
		Name:       "integration",
		KeyHash:    "hash-expiry",
		APIKey:     "sk-test",
		FullAccess: true,
		ExpiresAt:  &expiresAt,
	}
	require.NoError(t, repo.CreateAPIKey(ctx, key))

	loaded, err := repo.GetAPIKeyByHash(ctx, key.KeyHash)
	require.NoError(t, err)
	require.NotNil(t, loaded.ExpiresAt)
	require.Equal(t, time.UTC, loaded.ExpiresAt.Location())
	require.True(t, loaded.ExpiresAt.Equal(expiresAt))
}

// TestTenantAPIKeyRepositoryUpdateIsTenantScoped 验证通用更新不会越过租户边界。
// 输入同租户和其他租户的 Key；前者更新全部可配置字段，后者必须返回未找到。
func TestTenantAPIKeyRepositoryUpdateIsTenantScoped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.TenantAPIKey{}))
	repo := NewTenantAPIKeyRepository(db)
	ctx := context.Background()
	tenant42, tenant43 := uint64(42), uint64(43)
	keys := []*types.TenantAPIKey{
		{TenantID: &tenant42, ScopeType: types.APIKeyScopeTenant, Name: "scoped", KeyHash: "hash-scoped", APIKey: "sk-scoped"},
		{TenantID: &tenant43, ScopeType: types.APIKeyScopeTenant, Name: "other", KeyHash: "hash-other", APIKey: "sk-other"},
		{TenantID: &tenant42, ScopeType: types.APIKeyScopeTenant, Name: "full", KeyHash: "hash-full", APIKey: "sk-full", FullAccess: true},
	}
	for _, key := range keys {
		require.NoError(t, repo.CreateAPIKey(ctx, key))
	}

	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	updated, err := repo.UpdateAPIKey(ctx, tenant42, keys[0].ID, &types.TenantAPIKey{
		Name: "updated", FullAccess: false,
		KnowledgeBaseIDs: types.StringArray{"kb-1", "kb-2"},
		Capabilities:     types.StringArray{"retrieve", "chat"},
		ExpiresAt:        &expiresAt,
	})
	require.NoError(t, err)
	require.Equal(t, "updated", updated.Name)
	require.Equal(t, types.StringArray{"kb-1", "kb-2"}, updated.KnowledgeBaseIDs)
	require.Equal(t, types.StringArray{"retrieve", "chat"}, updated.Capabilities)
	require.NotNil(t, updated.ExpiresAt)
	require.True(t, updated.ExpiresAt.Equal(expiresAt))

	_, err = repo.UpdateAPIKey(ctx, tenant42, keys[1].ID, &types.TenantAPIKey{Name: "blocked"})
	require.ErrorIs(t, err, ErrTenantAPIKeyNotFound)

	full, err := repo.UpdateAPIKey(ctx, tenant42, keys[2].ID, &types.TenantAPIKey{
		Name: "full updated", FullAccess: false, Capabilities: types.StringArray{"retrieve"},
	})
	require.NoError(t, err)
	require.False(t, full.FullAccess)
}

func TestGetUserMCPTenantScopeDropsRevokedSharedGrant(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	statements := []string{
		`CREATE TABLE api_key_tenant_scopes (id INTEGER PRIMARY KEY, api_key_id INTEGER, tenant_id INTEGER, kb_scope_mode TEXT)`,
		`CREATE TABLE api_key_kb_scopes (id INTEGER PRIMARY KEY, api_key_tenant_scope_id INTEGER, api_key_id INTEGER, tenant_id INTEGER, knowledge_base_id TEXT, source_type TEXT, kb_share_id TEXT)`,
		`CREATE TABLE kb_shares (id TEXT PRIMARY KEY, knowledge_base_id TEXT, organization_id TEXT, deleted_at DATETIME)`,
		`CREATE TABLE organization_tenant_members (organization_id TEXT, tenant_id INTEGER)`,
		`CREATE TABLE organizations (id TEXT PRIMARY KEY, deleted_at DATETIME)`,
		`CREATE TABLE knowledge_bases (id TEXT PRIMARY KEY, deleted_at DATETIME)`,
		`INSERT INTO api_key_tenant_scopes VALUES (1, 9, 42, 'selected')`,
		`INSERT INTO api_key_kb_scopes VALUES (1, 1, 9, 42, 'kb-shared', 'shared', 'share-1')`,
		`INSERT INTO kb_shares VALUES ('share-1', 'kb-shared', 'org-1', NULL)`,
		`INSERT INTO organization_tenant_members VALUES ('org-1', 42)`,
		`INSERT INTO organizations VALUES ('org-1', NULL)`,
		`INSERT INTO knowledge_bases VALUES ('kb-shared', NULL)`,
	}
	for _, statement := range statements {
		require.NoError(t, db.Exec(statement).Error)
	}
	repo := &tenantAPIKeyRepository{db: db}

	scope, err := repo.GetUserMCPTenantScope(context.Background(), 9, 42)
	require.NoError(t, err)
	require.Len(t, scope.KnowledgeBases, 1)

	require.NoError(t, db.Exec(`UPDATE kb_shares SET deleted_at = CURRENT_TIMESTAMP WHERE id = 'share-1'`).Error)
	scope, err = repo.GetUserMCPTenantScope(context.Background(), 9, 42)
	require.NoError(t, err)
	require.Empty(t, scope.KnowledgeBases, "revoked exact share must leave an explicitly empty restriction")
}
