package repository

import (
	"context"
	"sync"
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

func TestUserMCPCredentialRotateIsOwnerScopedAndCompareAndSwap(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.TenantAPIKey{}, &types.APIKeyTenantScope{}, &types.APIKeyKnowledgeBaseScope{}))
	repo := &tenantAPIKeyRepository{db: db}
	owner := "owner-1"
	key := &types.TenantAPIKey{
		ScopeType: types.APIKeyScopeUserMCP, OwnerUserID: &owner, Name: "cursor",
		ClientType: types.MCPClientCursor, KeyHash: "old-hash", APIKey: "", TokenHint: "••••OLD1",
		Capabilities: types.StringArray{"retrieve", "chat"},
	}
	require.NoError(t, repo.CreateAPIKey(context.Background(), key))

	_, err = repo.RotateUserMCPAPIKey(context.Background(), "other-owner", key.ID, "old-hash", "blocked", "••••NOPE", nil)
	require.ErrorIs(t, err, ErrTenantAPIKeyNotFound)

	rotated, err := repo.RotateUserMCPAPIKey(context.Background(), owner, key.ID, "old-hash", "new-hash", "••••NEW1", nil)
	require.NoError(t, err)
	require.Equal(t, "new-hash", rotated.KeyHash)
	require.Equal(t, "••••NEW1", rotated.TokenHint)
	require.Empty(t, rotated.APIKey)

	_, err = repo.RotateUserMCPAPIKey(context.Background(), owner, key.ID, "old-hash", "second-hash", "••••NEW2", nil)
	require.ErrorIs(t, err, ErrTenantAPIKeyNotFound, "stale concurrent rotation must lose the CAS")
}

func TestListUserMCPCredentialsRetainsRevokedHistory(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&types.TenantAPIKey{}, &types.APIKeyTenantScope{}, &types.APIKeyKnowledgeBaseScope{}))
	repo := &tenantAPIKeyRepository{db: db}
	owner := "owner-1"
	key := &types.TenantAPIKey{ScopeType: types.APIKeyScopeUserMCP, OwnerUserID: &owner, Name: "history", KeyHash: "history-hash"}
	require.NoError(t, repo.CreateAPIKey(context.Background(), key))
	require.NoError(t, repo.RevokeUserMCPAPIKey(context.Background(), owner, key.ID))

	rows, err := repo.ListUserMCPAPIKeys(context.Background(), owner)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].RevokedAt)
	otherRows, err := repo.ListUserMCPAPIKeys(context.Background(), "other-owner")
	require.NoError(t, err)
	require.Empty(t, otherRows)

	_, err = repo.ReplaceUserMCPAPIKey(context.Background(), "other-owner", &types.TenantAPIKey{
		ID: key.ID, Name: "stolen", ClientType: types.MCPClientGeneric,
		Capabilities: types.StringArray{"retrieve"},
	})
	require.ErrorIs(t, err, ErrTenantAPIKeyNotFound)
	require.ErrorIs(t, repo.RevokeUserMCPAPIKey(context.Background(), "other-owner", key.ID), ErrTenantAPIKeyNotFound)

	_, err = repo.ReplaceUserMCPAPIKey(context.Background(), owner, &types.TenantAPIKey{
		ID: key.ID, Name: "revived", ClientType: types.MCPClientGeneric,
		Capabilities: types.StringArray{"retrieve"},
	})
	require.ErrorIs(t, err, ErrTenantAPIKeyNotFound, "revoked credentials must not be updated")
	_, err = repo.RotateUserMCPAPIKey(context.Background(), owner, key.ID, key.KeyHash, "revived", "••••NOPE", nil)
	require.ErrorIs(t, err, ErrTenantAPIKeyNotFound, "revoked credentials must not be rotated")
}

func TestConcurrentUserMCPRotateHasSingleCASWinner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_busy_timeout=5000"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&types.TenantAPIKey{}, &types.APIKeyTenantScope{}, &types.APIKeyKnowledgeBaseScope{}))
	repo := &tenantAPIKeyRepository{db: db}
	owner := "owner-1"
	key := &types.TenantAPIKey{ScopeType: types.APIKeyScopeUserMCP, OwnerUserID: &owner, Name: "race", KeyHash: "race-old"}
	require.NoError(t, repo.CreateAPIKey(context.Background(), key))

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, rotateErr := repo.RotateUserMCPAPIKey(context.Background(), owner, key.ID, "race-old", "race-new-"+string(rune('a'+i)), "••••RACE", nil)
			results <- rotateErr
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for rotateErr := range results {
		if rotateErr == nil {
			successes++
		} else {
			require.ErrorIs(t, rotateErr, ErrTenantAPIKeyNotFound)
		}
	}
	require.Equal(t, 1, successes)
}
